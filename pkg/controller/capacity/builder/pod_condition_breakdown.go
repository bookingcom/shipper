package builder

import "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"

type PodConditionBreakdown struct {
	podCount           uint32
	podConditionType   string
	podConditionStatus string
	name               string
	podConditionReason string
	states             []v1alpha1.ClusterCapacityReportContainerStateBreakdown
	containers         []v1alpha1.ClusterCapacityReportContainerBreakdown
}

func NewPodConditionBreakdown(
	podCount uint32,
	podConditionType string,
	podConditionStatus string,
	podConditionReason string,
) *PodConditionBreakdown {
	return &PodConditionBreakdown{
		podCount:           podCount,
		podConditionType:   podConditionType,
		podConditionStatus: podConditionStatus,
		podConditionReason: podConditionReason,
	}
}

func (p *PodConditionBreakdown) AddContainerBreakdown(container v1alpha1.ClusterCapacityReportContainerBreakdown) *PodConditionBreakdown {
	p.containers = append(p.containers, container)
	return p
}

func (p *PodConditionBreakdown) Build() v1alpha1.ClusterCapacityReportBreakdown {
	return v1alpha1.ClusterCapacityReportBreakdown{
		Type:       p.podConditionType,
		Status:     p.podConditionStatus,
		Count:      p.podCount,
		Reason:     p.podConditionReason,
		Containers: p.containers,
	}
}
