package capacity

import (
	core_v1 "k8s.io/api/core/v1"
	"strings"

	shipper_v1alpha1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

type containerState struct {
	cluster string
	owner   string
	pod     string

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

func getContainerStateField(c core_v1.ContainerState, f string) string {
	if c.Running != nil {
		switch f {
		case "type":
			return "Running"
		case "reason", "message":
			return ""
		}
	} else if c.Waiting != nil {
		switch f {
		case "type":
			return "Waiting"
		case "reason":
			return c.Waiting.Reason
		case "message":
			return c.Waiting.Message
		}
	} else if c.Terminated != nil {
		switch f {
		case "type":
			return "Terminated"
		case "reason":
			return c.Terminated.Reason
		case "message":
			return c.Terminated.Message
		}
	} else {
		return ""
	}
	panic("Shouldn't get in here")
}

func buildContainerStateEntries(podsList []*core_v1.Pod) []containerState {
	containerStates := make([]containerState, 0, 0)

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

			for _, containerStatus := range pod.Status.ContainerStatuses {
				state.containerName = string2ptr(containerStatus.Name)
				state.containerStateType = string2ptr(getContainerStateField(containerStatus.State, "type"))
				state.containerStateReason = string2ptr(getContainerStateField(containerStatus.State, "reason"))
				state.containerStateMessage = string2ptr(getContainerStateField(containerStatus.State, "message"))

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
			containers: containerStates,
			status:     ptr2string(state.conditionStatus),
			reason:     ptr2string(state.conditionReason),
			typ:        ptr2string(state.conditionType),
		}
	} else {
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
