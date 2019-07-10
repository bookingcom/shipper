package release

import (
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

var ConditionsShouldDiscardTimestamps = false

func NewReleaseCondition(condType shipper.ReleaseConditionType, status corev1.ConditionStatus, reason, message string) *shipper.ReleaseCondition {
	now := metav1.Now()
	if ConditionsShouldDiscardTimestamps {
		now = metav1.Time{}
	}
	return &shipper.ReleaseCondition{
		Type:               condType,
		Status:             status,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	}
}

func SetReleaseCondition(status *shipper.ReleaseStatus, condition shipper.ReleaseCondition) {
	currentCond := GetReleaseCondition(*status, condition.Type)
	if currentCond != nil && currentCond.Status == condition.Status && currentCond.Reason == condition.Reason {
		return
	}
	if currentCond != nil && currentCond.Status != condition.Status {
		condition.LastTransitionTime = currentCond.LastTransitionTime
	}
	newConditions := filterOutCondition(status.Conditions, condition.Type)
	status.Conditions = append(newConditions, condition)
	sort.Slice(status.Conditions, func(i, j int) bool {
		return status.Conditions[i].Type < status.Conditions[j].Type
	})
}

func GetReleaseCondition(status shipper.ReleaseStatus, condType shipper.ReleaseConditionType) *shipper.ReleaseCondition {
	for i := range status.Conditions {
		c := status.Conditions[i]
		if c.Type == condType {
			return &c
		}
	}
	return nil
}

func RemoveReleaseCondition(status shipper.ReleaseStatus, condType shipper.ReleaseConditionType) {
	status.Conditions = filterOutCondition(status.Conditions, condType)
}

func ReleaseInstalled(release *shipper.Release) bool {
	installedCond := GetReleaseCondition(release.Status, shipper.ReleaseConditionTypeInstalled)
	return installedCond != nil && installedCond.Status == corev1.ConditionTrue
}

func ReleaseScheduled(release *shipper.Release) bool {
	scheduledCond := GetReleaseCondition(release.Status, shipper.ReleaseConditionTypeScheduled)
	return scheduledCond != nil && scheduledCond.Status == corev1.ConditionTrue
}

func ReleaseComplete(release *shipper.Release) bool {
	releasedCond := GetReleaseCondition(release.Status, shipper.ReleaseConditionTypeComplete)
	return releasedCond != nil && releasedCond.Status == corev1.ConditionTrue
}

func ReleaseProgressing(release *shipper.Release) bool {
	return !(ReleaseComplete(release))
}

func filterOutCondition(conditions []shipper.ReleaseCondition, condType shipper.ReleaseConditionType) []shipper.ReleaseCondition {
	var newConditions []shipper.ReleaseCondition
	for _, c := range conditions {
		if c.Type == condType {
			continue
		}
		newConditions = append(newConditions, c)
	}
	return newConditions
}
