package cluster

import "sort"

import (
	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipperV1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
)

func NewClusterTrafficCondition(condType shipperV1.ClusterConditionType, status coreV1.ConditionStatus, reason, message string) *shipperV1.ClusterTrafficCondition {
	now := metaV1.Now()
	if ConditionsShouldDiscardTimestamps {
		now = metaV1.Time{}
	}
	return &shipperV1.ClusterTrafficCondition{
		Type:               condType,
		Status:             status,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	}
}

func GetClusterTrafficCondition(status *shipperV1.ClusterTrafficStatus, condType shipperV1.ClusterConditionType) *shipperV1.ClusterTrafficCondition {
	for _, c := range status.Conditions {
		if c.Type == condType {
			return &c
		}
	}
	return nil
}

func SetClusterTrafficCondition(status *shipperV1.ClusterTrafficStatus, condition shipperV1.ClusterTrafficCondition) {
	currentCond := GetClusterTrafficCondition(status, condition.Type)
	if currentCond != nil && currentCond.Type == condition.Type && currentCond.Reason == condition.Reason {
		return
	}
	if currentCond != nil && currentCond.Type == condition.Type {
		condition.LastTransitionTime = currentCond.LastTransitionTime
	}
	newConditions := filterOutTrafficCondition(status.Conditions, condition.Type)
	status.Conditions = append(newConditions, condition)
	sort.Slice(newConditions, func(i, j int) bool {
		return newConditions[i].Type < newConditions[j].Type
	})
}

func RemoveClusterTrafficCondition(status shipperV1.ClusterTrafficStatus, condType shipperV1.ClusterConditionType) {
	status.Conditions = filterOutTrafficCondition(status.Conditions, condType)
}

func filterOutTrafficCondition(conditions []shipperV1.ClusterTrafficCondition, condType shipperV1.ClusterConditionType) []shipperV1.ClusterTrafficCondition {
	var newConditions []shipperV1.ClusterTrafficCondition
	for _, c := range conditions {
		if c.Type == condType {
			continue
		}
		newConditions = append(newConditions, c)
	}
	return newConditions
}
