package builder

import (
	"github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"sort"
)

type ContainerStateBreakdown struct {
	containerName string
	states        []*v1alpha1.ClusterCapacityReportContainerStateBreakdown
}

func NewContainerBreakdown(containerName string) *ContainerStateBreakdown {
	return &ContainerStateBreakdown{containerName: containerName}
}

func (c *ContainerStateBreakdown) AddState(
	containerCount uint32,
	podExampleName string,
	containerConditionType string,
	containerConditionReason string,
) *ContainerStateBreakdown {

	breakdown := v1alpha1.ClusterCapacityReportContainerStateBreakdown{
		Count:  containerCount,
		Type:   containerConditionType,
		Reason: containerConditionReason,
		Example: v1alpha1.ClusterCapacityReportContainerBreakdownExample{
			Pod: podExampleName,
		},
	}

	for _, s := range c.states {
		if s.Type == breakdown.Type && s.Reason == breakdown.Reason {
			s.Count += 1
			return c
		}
	}

	c.states = append(c.states, &breakdown)
	return c
}

func (c *ContainerStateBreakdown) Build() v1alpha1.ClusterCapacityReportContainerBreakdown {
	stateCount := len(c.states)
	orderedStates := make([]v1alpha1.ClusterCapacityReportContainerStateBreakdown, stateCount)
	for i, v := range c.states {
		orderedStates[i] = *v
	}

	sort.Slice(orderedStates, func(i, j int) bool {
		return orderedStates[i].Type < orderedStates[j].Type
	})

	return v1alpha1.ClusterCapacityReportContainerBreakdown{
		Name:   c.containerName,
		States: orderedStates,
	}
}
