package cluster

import (
	"sort"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipperV1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
)

func NewClusterInstallationCondition(condType shipperV1.ClusterConditionType, status coreV1.ConditionStatus, reason, message string) *shipperV1.ClusterInstallationCondition {
	now := metaV1.Now()
	if ConditionsShouldDiscardTimestamps {
		now = metaV1.Time{}
	}
	return &shipperV1.ClusterInstallationCondition{
		Type:               condType,
		Status:             status,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	}
}

func GetClusterInstallationCondition(status *shipperV1.ClusterInstallationStatus, condType shipperV1.ClusterConditionType) *shipperV1.ClusterInstallationCondition {
	for _, c := range status.Conditions {
		if c.Type == condType {
			return &c
		}
	}
	return nil
}

func SetClusterInstallationCondition(status *shipperV1.ClusterInstallationStatus, condition shipperV1.ClusterInstallationCondition) {
	currentCond := GetClusterInstallationCondition(status, condition.Type)
	if currentCond != nil && currentCond.Type == condition.Type && currentCond.Reason == currentCond.Reason {
		return
	}
	if currentCond != nil && currentCond.Type == condition.Type {
		condition.LastTransitionTime = currentCond.LastTransitionTime
	}
	newConditions := filterOutInstallationCondition(status.Conditions, condition.Type)
	status.Conditions = append(newConditions, condition)
	sort.Slice(newConditions, func(i, j int) bool {
		return newConditions[i].Type < newConditions[j].Type
	})
}

func RemoveClusterInstallationCondition(status shipperV1.ClusterInstallationStatus, condType shipperV1.ClusterConditionType) {
	status.Conditions = filterOutInstallationCondition(status.Conditions, condType)
}

func filterOutInstallationCondition(conditions []shipperV1.ClusterInstallationCondition, condType shipperV1.ClusterConditionType) []shipperV1.ClusterInstallationCondition {
	var newConditions []shipperV1.ClusterInstallationCondition
	for _, c := range conditions {
		if c.Type == condType {
			continue
		}
		newConditions = append(newConditions, c)
	}
	return newConditions
}
