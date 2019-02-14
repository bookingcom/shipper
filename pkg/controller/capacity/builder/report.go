package builder

import (
	"fmt"
	"sort"

	core_v1 "k8s.io/api/core/v1"

	"github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

type Report struct {
	ownerName  string
	breakdowns []v1alpha1.ClusterCapacityReportBreakdown
}

func NewReport(ownerName string) *Report {
	return &Report{ownerName: ownerName}
}

func (r *Report) AddBreakdown(breakdown v1alpha1.ClusterCapacityReportBreakdown) *Report {
	r.breakdowns = append(r.breakdowns, breakdown)
	return r
}

func (r *Report) Build() *v1alpha1.ClusterCapacityReport {

	sort.Slice(r.breakdowns, func(i, j int) bool {
		return r.breakdowns[i].Type < r.breakdowns[j].Type
	})

	report := v1alpha1.ClusterCapacityReport{
		Owner: v1alpha1.ClusterCapacityReportOwner{
			Name: r.ownerName,
		},
		Breakdown: r.breakdowns,
	}
	return &report
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
