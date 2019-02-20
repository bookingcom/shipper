package builder

import (
	"sort"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

type ContainerStateBreakdown struct {
	containerName string
	states        []*shipper.ClusterCapacityReportContainerStateBreakdown
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

	breakdown := shipper.ClusterCapacityReportContainerStateBreakdown{
		Count:  containerCount,
		Type:   containerConditionType,
		Reason: containerConditionReason,
		Example: shipper.ClusterCapacityReportContainerBreakdownExample{
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

func (c *ContainerStateBreakdown) Build() shipper.ClusterCapacityReportContainerBreakdown {
	stateCount := len(c.states)
	orderedStates := make([]shipper.ClusterCapacityReportContainerStateBreakdown, stateCount)
	for i, v := range c.states {
		orderedStates[i] = *v
	}

	sort.Slice(orderedStates, func(i, j int) bool {
		return orderedStates[i].Type < orderedStates[j].Type
	})

	return shipper.ClusterCapacityReportContainerBreakdown{
		Name:   c.containerName,
		States: orderedStates,
	}
}
