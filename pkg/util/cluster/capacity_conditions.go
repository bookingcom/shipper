package cluster

import (
	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipperV1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	"sort"
)

var ConditionsShouldDiscardTimestamps = false

func NewClusterCapacityCondition(condType shipperV1.ClusterConditionType, status coreV1.ConditionStatus, reason, message string) *shipperV1.ClusterCapacityCondition {
	now := metaV1.Now()
	if ConditionsShouldDiscardTimestamps {
		now = metaV1.Time{}
	}
	return &shipperV1.ClusterCapacityCondition{
		Type:               condType,
		Status:             status,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	}
}

func GetClusterCapacityCondition(status *shipperV1.ClusterCapacityStatus, condType shipperV1.ClusterConditionType) *shipperV1.ClusterCapacityCondition {
	for _, c := range status.Conditions {
		if c.Type == condType {
			return &c
		}
	}
	return nil
}

func SetClusterCapacityCondition(status *shipperV1.ClusterCapacityStatus, condition shipperV1.ClusterCapacityCondition) {
	currentCond := GetClusterCapacityCondition(status, condition.Type)
	if currentCond != nil && currentCond.Type == condition.Type && currentCond.Reason == currentCond.Reason {
		return
	}
	if currentCond != nil && currentCond.Type == condition.Type {
		condition.LastTransitionTime = currentCond.LastTransitionTime
	}
	newConditions := filterOutCondition(status.Conditions, condition.Type)
	status.Conditions = append(newConditions, condition)
	sort.Slice(newConditions, func(i, j int) bool {
		return newConditions[i].Type < newConditions[j].Type
	})
}

func RemoveClusterCapacityCondition(status shipperV1.ClusterCapacityStatus, condType shipperV1.ClusterConditionType) {
	status.Conditions = filterOutCondition(status.Conditions, condType)
}

func filterOutCondition(conditions []shipperV1.ClusterCapacityCondition, condType shipperV1.ClusterConditionType) []shipperV1.ClusterCapacityCondition {
	var newConditions []shipperV1.ClusterCapacityCondition
	for _, c := range conditions {
		if c.Type == condType {
			continue
		}
		newConditions = append(newConditions, c)
	}
	return newConditions
}