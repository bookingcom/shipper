package capacity

import (
	"github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"sort"
)

type reportBuilder struct {
	ownerName  string
	breakdowns []v1alpha1.ClusterCapacityReportBreakdown
}

func newReportBuilder(ownerName string) *reportBuilder {
	return &reportBuilder{ownerName: ownerName}
}

func (c *reportBuilder) AddBreakdown(breakdown v1alpha1.ClusterCapacityReportBreakdown) *reportBuilder {
	c.breakdowns = append(c.breakdowns, breakdown)
	return c
}

func (c *reportBreakdownBuilder) AddContainerBreakdown(container v1alpha1.ClusterCapacityReportContainerBreakdown) *reportBreakdownBuilder {
	c.containers = append(c.containers, container)
	return c
}

func (c *reportBuilder) Build() v1alpha1.ClusterCapacityReport {

	sort.Slice(c.breakdowns, func(i, j int) bool {
		return c.breakdowns[i].Type < c.breakdowns[j].Type
	})

	report := v1alpha1.ClusterCapacityReport{
		Owner: v1alpha1.ClusterCapacityReportOwner{
			Name: c.ownerName,
		},
		Breakdown: c.breakdowns,
	}
	return report
}

type reportBreakdownBuilder struct {
	podCount           uint32
	podConditionType   string
	podConditionStatus string
	name               string
	podConditionReason string
	states             []v1alpha1.ClusterCapacityReportContainerStateBreakdown
	containers         []v1alpha1.ClusterCapacityReportContainerBreakdown
}

func newBreakdownBuilder(
	podCount uint32,
	podConditionType string,
	podConditionStatus string,
	podConditionReason string,
) *reportBreakdownBuilder {
	return &reportBreakdownBuilder{
		podCount:           podCount,
		podConditionType:   podConditionType,
		podConditionStatus: podConditionStatus,
		podConditionReason: podConditionReason,
	}
}

func (c *reportBreakdownBuilder) SetName(name string) *reportBreakdownBuilder {
	c.name = name
	return c
}

func (c *reportBreakdownBuilder) AddState(
	count uint32,
	podExampleName string,
	podConditionType,
	podConditionStatus string,
) *reportBreakdownBuilder {
	c.states = append(c.states, v1alpha1.ClusterCapacityReportContainerStateBreakdown{
		Count: count,
		Type:  podConditionType,
		Example: v1alpha1.ClusterCapacityReportContainerBreakdownExample{
			Pod: podExampleName,
		},
	})
	return c
}

func (c *reportBreakdownBuilder) Build() v1alpha1.ClusterCapacityReportBreakdown {
	return v1alpha1.ClusterCapacityReportBreakdown{
		Type:       c.podConditionType,
		Status:     c.podConditionStatus,
		Count:      c.podCount,
		Reason:     c.podConditionReason,
		Containers: c.containers,
	}
}

type containerBreakdownBuilder struct {
	containerName string
	states        []v1alpha1.ClusterCapacityReportContainerStateBreakdown
}

func newContainerBreakdownBuilder(containerName string) *containerBreakdownBuilder {
	return &containerBreakdownBuilder{containerName: containerName}
}

func (c *containerBreakdownBuilder) AddState(
	containerCount uint32,
	podExampleName string,
	containerConditionType string,
	containerConditionReason string,
) *containerBreakdownBuilder {
	c.states = append(c.states, v1alpha1.ClusterCapacityReportContainerStateBreakdown{
		Count:  containerCount,
		Type:   containerConditionType,
		Reason: containerConditionReason,
		Example: v1alpha1.ClusterCapacityReportContainerBreakdownExample{
			Pod: podExampleName,
		},
	})
	return c
}

func (c *containerBreakdownBuilder) Build() v1alpha1.ClusterCapacityReportContainerBreakdown {
	sort.Slice(c.states, func(i, j int) bool {
		return c.states[i].Type < c.states[j].Type
	})

	return v1alpha1.ClusterCapacityReportContainerBreakdown{
		Name:   c.containerName,
		States: c.states,
	}
}
