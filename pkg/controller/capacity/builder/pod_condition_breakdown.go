package builder

import (
	"sort"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

type containerStateBreakdownBuilders map[string]*ContainerStateBreakdown

func (c containerStateBreakdownBuilders) Get(containerName string) *ContainerStateBreakdown {
	var b *ContainerStateBreakdown
	var ok bool
	if b, ok = c[containerName]; !ok {
		b = NewContainerBreakdown(containerName)
		c[containerName] = b
	}
	return b
}

type PodConditionBreakdown struct {
	podCount           uint32
	podConditionType   string
	podConditionStatus string
	podConditionReason string

	containerStateBreakdownBuilders containerStateBreakdownBuilders
}

func NewPodConditionBreakdown(
	initialPodCount uint32,
	podConditionType string,
	podConditionStatus string,
	podConditionReason string,
) *PodConditionBreakdown {
	return &PodConditionBreakdown{
		podCount:                        initialPodCount,
		podConditionType:                podConditionType,
		podConditionStatus:              podConditionStatus,
		podConditionReason:              podConditionReason,
		containerStateBreakdownBuilders: make(containerStateBreakdownBuilders),
	}
}

func PodConditionBreakdownKey(typ, status, reason string) string {
	return typ + status + reason
}

func (p *PodConditionBreakdown) Key() string {
	return PodConditionBreakdownKey(p.podConditionType, p.podConditionStatus, p.podConditionReason)
}

func (p *PodConditionBreakdown) AddOrIncrementContainerState(
	containerName string,
	podExampleName string,
	containerConditionType string,
	containerConditionReason string,
	containerExampleMessage string,
) *PodConditionBreakdown {
	p.containerStateBreakdownBuilders.
		Get(containerName).
		AddOrIncrementState(podExampleName, containerConditionType, containerConditionReason, containerExampleMessage)
	return p
}

func (p *PodConditionBreakdown) IncrementCount() *PodConditionBreakdown {
	p.podCount += 1
	return p
}

func (p *PodConditionBreakdown) Build() shipper.ClusterCapacityReportBreakdown {

	orderedContainers := make([]shipper.ClusterCapacityReportContainerBreakdown, 0)

	for _, v := range p.containerStateBreakdownBuilders {
		orderedContainers = append(orderedContainers, v.Build())
	}

	sort.Slice(orderedContainers, func(i, j int) bool {
		return orderedContainers[i].Name < orderedContainers[j].Name
	})

	return shipper.ClusterCapacityReportBreakdown{
		Type:       p.podConditionType,
		Status:     p.podConditionStatus,
		Count:      p.podCount,
		Reason:     p.podConditionReason,
		Containers: orderedContainers,
	}
}
