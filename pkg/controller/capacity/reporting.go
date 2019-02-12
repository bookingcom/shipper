package capacity

import (
	"fmt"
	"sort"
	"strings"

	core_v1 "k8s.io/api/core/v1"

	shipper_v1alpha1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

type ContainerStateField string

const (
	ContainerStateFieldType    ContainerStateField = "type"
	ContainerStateFieldReason  ContainerStateField = "reason"
	ContainerStateFieldMessage ContainerStateField = "message"
)

type containerState struct {
	cluster string
	owner   string
	pod     string
	count   uint32

	conditionType   *string
	conditionStatus *string
	conditionReason *string

	containerName         *string
	containerStateType    *string
	containerStateReason  *string
	containerStateMessage *string
}

func ptr2string(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func string2ptr(v string) *string {
	return &v
}

func getRunningContainerStateField(field ContainerStateField) string {
	switch field {
	case ContainerStateFieldType:
		return "Running"
	case ContainerStateFieldReason, ContainerStateFieldMessage:
		return ""
	default:
		panic(fmt.Sprintf("Unknown field %s", field))
	}
}

func getWaitingContainerStateField(stateWaiting *core_v1.ContainerStateWaiting, field ContainerStateField) string {
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

func getTerminatedContainerStateField(stateTerminated *core_v1.ContainerStateTerminated, f ContainerStateField) string {
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

func getContainerStateField(c core_v1.ContainerState, f ContainerStateField) string {
	if c.Running != nil {
		return getRunningContainerStateField(f)
	} else if c.Waiting != nil {
		return getWaitingContainerStateField(c.Waiting, f)
	} else if c.Terminated != nil {
		return getTerminatedContainerStateField(c.Terminated, f)
	}

	// TODO: f should be a constant somehow.
	panic("Programmer error: should be one of 'type', 'reason' or 'message'")
}

func buildContainerStateEntries(podsList []*core_v1.Pod) []containerState {
	containerStates := make([]containerState, 0)

	// Sort pods list to offer a stable pod as example.
	sort.Slice(podsList, func(i, j int) bool {
		return podsList[i].Name < podsList[j].Name
	})

	for _, pod := range podsList {
		state := containerState{
			cluster: "microk8s",
			owner:   "shipper-deployment",
			pod:     pod.Name,
		}

		for _, cond := range pod.Status.Conditions {
			state.conditionType = string2ptr(string(cond.Type))
			state.conditionStatus = string2ptr(string(cond.Status))
			state.conditionReason = string2ptr(string(cond.Reason))
			state.count = state.count + 1
			for _, containerStatus := range pod.Status.ContainerStatuses {
				state.containerName = string2ptr(containerStatus.Name)
				state.containerStateType = string2ptr(getContainerStateField(containerStatus.State, ContainerStateFieldType))
				state.containerStateReason = string2ptr(getContainerStateField(containerStatus.State, ContainerStateFieldReason))
				state.containerStateMessage = string2ptr(getContainerStateField(containerStatus.State, ContainerStateFieldMessage))

				containerStates = append(containerStates, state)
			}
		}
	}

	return containerStates
}

type containerStateSummary struct {
	count   uint32
	example shipper_v1alpha1.ClusterCapacityReportContainerBreakdownExample
	name    string
	reason  string
	typ     string
}

type conditionSummary struct {
	count      uint32
	containers map[string]containerStateSummary
	reason     string
	status     string
	typ        string
}

func buildKey(args ...string) string {
	validArgs := make([]string, 0)

	for _, v := range args {
		if len(v) == 0 {
			break
		}
		validArgs = append(validArgs, v)
	}

	return strings.Join(validArgs, "/")
}

func summarizeContainerStateByCondition(conditionSummaries map[string]conditionSummary, state containerState) {

	conditionSummaryKey := buildKey(
		state.cluster,
		ptr2string(state.conditionType),
		ptr2string(state.conditionStatus),
		ptr2string(state.conditionReason),
	)

	containerStateKey := buildKey(
		state.cluster,
		ptr2string(state.containerName),
		ptr2string(state.containerStateType),
		ptr2string(state.containerStateReason),
	)

	if summary, ok := conditionSummaries[conditionSummaryKey]; !ok {

		containerStates := make(map[string]containerStateSummary)

		containerStates[containerStateKey] = containerStateSummary{
			count:   1,
			example: shipper_v1alpha1.ClusterCapacityReportContainerBreakdownExample{Pod: state.pod},
			name:    ptr2string(state.containerName),
			reason:  ptr2string(state.containerStateReason),
			typ:     ptr2string(state.containerStateType),
		}

		conditionSummaries[conditionSummaryKey] = conditionSummary{
			count:      1,
			containers: containerStates,
			status:     ptr2string(state.conditionStatus),
			reason:     ptr2string(state.conditionReason),
			typ:        ptr2string(state.conditionType),
		}
	} else {
		summary.count = summary.count + 1
		if existingState, ok := summary.containers[containerStateKey]; !ok {
			summary.containers[containerStateKey] = containerStateSummary{
				count:   1,
				example: shipper_v1alpha1.ClusterCapacityReportContainerBreakdownExample{Pod: state.pod},
				name:    ptr2string(state.containerName),
				reason:  ptr2string(state.containerStateReason),
				typ:     ptr2string(state.containerStateType),
			}
		} else {
			existingState.count = existingState.count + 1
			summary.containers[containerStateKey] = existingState
		}

		conditionSummaries[conditionSummaryKey] = summary
	}
}

func summarizeContainerStatesByCondition(containerStates []containerState) map[string]conditionSummary {
	conditionSummaries := make(map[string]conditionSummary)
	for _, state := range containerStates {
		summarizeContainerStateByCondition(conditionSummaries, state)
	}
	return conditionSummaries
}

func buildReport(ownerName string, conditionSummaries map[string]conditionSummary) *shipper_v1alpha1.ClusterCapacityReport {
	report := &shipper_v1alpha1.ClusterCapacityReport{
		Owner:     shipper_v1alpha1.ClusterCapacityReportOwner{Name: ownerName},
		Breakdown: []shipper_v1alpha1.ClusterCapacityReportBreakdown{},
	}

	for _, cond := range conditionSummaries {
		breakdown := shipper_v1alpha1.ClusterCapacityReportBreakdown{
			Type:   cond.typ,
			Status: cond.status,
			Reason: cond.reason,
			Count:  cond.count,
		}

		for _, container := range cond.containers {
			var containerBreakdown *shipper_v1alpha1.ClusterCapacityReportContainerBreakdown

			for _, c := range breakdown.Containers {
				if c.Name == container.name {
					containerBreakdown = &c
					break
				}
			}

			if containerBreakdown == nil {
				containerBreakdown = &shipper_v1alpha1.ClusterCapacityReportContainerBreakdown{
					Name: container.name,
				}
			}

			containerBreakdown.States = append(containerBreakdown.States, shipper_v1alpha1.ClusterCapacityReportContainerStateBreakdown{
				Reason:  container.reason,
				Type:    container.typ,
				Count:   container.count,
				Example: container.example,
			})

			breakdown.Containers = append(breakdown.Containers, *containerBreakdown)
		}

		report.Breakdown = append(report.Breakdown, breakdown)
	}

	return report
}
