package capacity

import (
	"github.com/bookingcom/shipper/pkg/controller/capacity/builder"
	"sort"
	"strings"

	core_v1 "k8s.io/api/core/v1"

	shipper_v1alpha1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

type containerState struct {
	cluster string
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

func buildContainerStateEntries(clusterName string, podsList []*core_v1.Pod) []containerState {
	containerStates := make([]containerState, 0)

	// Sort pods list to offer a stable pod as example.
	sort.Slice(podsList, func(i, j int) bool {
		return podsList[i].Name < podsList[j].Name
	})

	for _, pod := range podsList {
		state := containerState{
			cluster: clusterName,
			pod:     pod.Name,
		}

		for _, cond := range pod.Status.Conditions {
			state.conditionType = string2ptr(string(cond.Type))
			state.conditionStatus = string2ptr(string(cond.Status))
			state.conditionReason = string2ptr(string(cond.Reason))
			for _, containerStatus := range pod.Status.ContainerStatuses {
				state.containerName = string2ptr(containerStatus.Name)
				state.containerStateType = string2ptr(builder.GetContainerStateField(containerStatus.State, builder.ContainerStateFieldType))
				state.containerStateReason = string2ptr(builder.GetContainerStateField(containerStatus.State, builder.ContainerStateFieldReason))
				state.containerStateMessage = string2ptr(builder.GetContainerStateField(containerStatus.State, builder.ContainerStateFieldMessage))

				containerStates = append(containerStates, state)
			}
		}
	}

	return containerStates
}

type containerStateSummary struct {
	containerCount uint32
	example        shipper_v1alpha1.ClusterCapacityReportContainerBreakdownExample
	name           string
	reason         string
	typ            string
}

type conditionSummary struct {
	podCount   uint32
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
			containerCount: 1,
			example:        shipper_v1alpha1.ClusterCapacityReportContainerBreakdownExample{Pod: state.pod},
			name:           ptr2string(state.containerName),
			reason:         ptr2string(state.containerStateReason),
			typ:            ptr2string(state.containerStateType),
		}

		conditionSummaries[conditionSummaryKey] = conditionSummary{
			podCount:   1,
			containers: containerStates,
			status:     ptr2string(state.conditionStatus),
			reason:     ptr2string(state.conditionReason),
			typ:        ptr2string(state.conditionType),
		}
	} else {
		if existingState, ok := summary.containers[containerStateKey]; !ok {
			summary.containers[containerStateKey] = containerStateSummary{
				containerCount: 1,
				example:        shipper_v1alpha1.ClusterCapacityReportContainerBreakdownExample{Pod: state.pod},
				name:           ptr2string(state.containerName),
				reason:         ptr2string(state.containerStateReason),
				typ:            ptr2string(state.containerStateType),
			}
		} else {
			existingState.containerCount = existingState.containerCount + 1
			summary.containers[containerStateKey] = existingState
		}

		summary.podCount = summary.podCount + 1
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

type conditionSummaryMap map[string]conditionSummary

func (c conditionSummaryMap) SortedByKeyAsc() []conditionSummary {
	var keys = []string{}
	for k := range c {
		keys = append(keys, k)
	}

	sort.Slice(keys, func(i, j int) bool {
		return c[keys[i]].typ < c[keys[j]].typ
	})

	var conds = []conditionSummary{}
	for k := range keys {
		conds = append(conds, c[keys[k]])
	}

	return conds
}

type containerStateBreakdownBuilders map[string]*builder.ContainerStateBreakdown

func (c containerStateBreakdownBuilders) Get(containerName string) *builder.ContainerStateBreakdown {
	var b *builder.ContainerStateBreakdown
	var ok bool
	if b, ok = c[containerName]; !ok {
		b = builder.NewContainerBreakdown(containerName)
		c[containerName] = b
	}
	return b
}

func buildReport(ownerName string, conditionSummaries conditionSummaryMap) *shipper_v1alpha1.ClusterCapacityReport {
	reportBuilder := builder.NewReport(ownerName)
	containerStateBreakdownBuilders := make(containerStateBreakdownBuilders)

	for _, cond := range conditionSummaries.SortedByKeyAsc() {
		breakdownBuilder := builder.NewPodConditionBreakdown(cond.podCount, cond.typ, cond.status, cond.reason)
		for _, container := range cond.containers {
			containerStateBreakdownBuilders.Get(container.name).AddState(container.containerCount, container.example.Pod, container.typ, container.reason)
		}
		for _, v := range containerStateBreakdownBuilders {
			breakdownBuilder.AddContainerBreakdown(v.Build())
		}
		reportBuilder.AddBreakdown(breakdownBuilder.Build())
	}
	return reportBuilder.Build()
}
