package builder

import (
	"fmt"
	"sort"

	core_v1 "k8s.io/api/core/v1"

	"github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

type podConditionBreakdownBuilders map[string]*PodConditionBreakdown

func (c podConditionBreakdownBuilders) Get(typ, status, reason string) *PodConditionBreakdown {
	var b *PodConditionBreakdown
	var ok bool
	key := typ + status + reason
	if b, ok = c[key]; !ok {
		b = NewPodConditionBreakdown(0, typ, status, reason)
		c[key] = b
	}
	return b
}

type Report struct {
	ownerName                     string
	breakdowns                    []v1alpha1.ClusterCapacityReportBreakdown
	podConditionBreakdownBuilders podConditionBreakdownBuilders
}

func NewReport(ownerName string) *Report {
	return &Report{
		ownerName:                     ownerName,
		podConditionBreakdownBuilders: make(podConditionBreakdownBuilders),
	}
}

func (r *Report) AddPod(pod *core_v1.Pod) {

	for _, cond := range pod.Status.Conditions {
		b := r.podConditionBreakdownBuilders.Get(string(cond.Type), string(cond.Status), string(cond.Reason))
		b.IncrementCount()

		for _, containerStatus := range pod.Status.ContainerStatuses {
			b.AddContainerState(
				containerStatus.Name,
				1,
				pod.Name,
				GetContainerStateField(containerStatus.State, ContainerStateFieldType),
				GetContainerStateField(containerStatus.State, ContainerStateFieldReason),
			)
		}
	}
}

func (r *Report) Build() *v1alpha1.ClusterCapacityReport {

	orderedBreakdowns := make([]v1alpha1.ClusterCapacityReportBreakdown, len(r.podConditionBreakdownBuilders))

	i := 0
	for _, v := range r.podConditionBreakdownBuilders {
		orderedBreakdowns[i] = v.Build()
		i++
	}

	sort.Slice(orderedBreakdowns, func(i, j int) bool {
		if orderedBreakdowns[i].Type == orderedBreakdowns[j].Type {
			return orderedBreakdowns[i].Status < orderedBreakdowns[j].Status
		}
		return orderedBreakdowns[i].Type < orderedBreakdowns[j].Type
	})

	report := v1alpha1.ClusterCapacityReport{
		Owner: v1alpha1.ClusterCapacityReportOwner{
			Name: r.ownerName,
		},
		Breakdown: orderedBreakdowns,
	}
	return &report
}

func (r *Report) AddPodConditionBreakdownBuilder(b *PodConditionBreakdown) *Report {
	r.podConditionBreakdownBuilders[b.Key()] = b
	return r
}

func GetRunningContainerStateField(field ContainerStateField) string {
	switch field {
	case ContainerStateFieldType:
		return "Running"
	case ContainerStateFieldReason, ContainerStateFieldMessage:
		return ""
	default:
		panic(fmt.Sprintf("Unknown field %s", field))
	}
}

func GetWaitingContainerStateField(stateWaiting *core_v1.ContainerStateWaiting, field ContainerStateField) string {
	switch field {
	case ContainerStateFieldType:
		return "Waiting"
	case ContainerStateFieldReason:
		return stateWaiting.Reason
	case ContainerStateFieldMessage:
		return stateWaiting.Message
	default:
		panic(fmt.Sprintf("Unknown field %s", field))
	}
}

func GetTerminatedContainerStateField(stateTerminated *core_v1.ContainerStateTerminated, f ContainerStateField) string {
	switch f {
	case ContainerStateFieldType:
		return "Terminated"
	case ContainerStateFieldReason:
		return stateTerminated.Reason
	case ContainerStateFieldMessage:
		return stateTerminated.Message
	default:
		panic(fmt.Sprintf("Unknown field %s", f))
	}
}

func GetContainerStateField(c core_v1.ContainerState, f ContainerStateField) string {
	if c.Running != nil {
		return GetRunningContainerStateField(f)
	} else if c.Waiting != nil {
		return GetWaitingContainerStateField(c.Waiting, f)
	} else if c.Terminated != nil {
		return GetTerminatedContainerStateField(c.Terminated, f)
	}

	panic("Programmer error: a container state must be either Running, Waiting or Terminated.")
}

type ContainerStateField string

const (
	ContainerStateFieldType    ContainerStateField = "type"
	ContainerStateFieldReason  ContainerStateField = "reason"
	ContainerStateFieldMessage ContainerStateField = "message"
)
