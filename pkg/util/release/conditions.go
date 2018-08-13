package release

import (
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
)

var ConditionsShouldDiscardTimestamps = false

const (
	NotReachableReason = "NotReachable"
	BadSyntaxReason    = "BadSyntax"
	BadObjectsReason   = "BadObjects"
)

func NewReleaseCondition(condType shipperv1.ReleaseConditionType, status corev1.ConditionStatus, reason, message string) *shipperv1.ReleaseCondition {
	now := metav1.Now()
	if ConditionsShouldDiscardTimestamps {
		now = metav1.Time{}
	}
	return &shipperv1.ReleaseCondition{
		Type:               condType,
		Status:             status,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	}
}

func SetReleaseCondition(status *shipperv1.ReleaseStatus, condition shipperv1.ReleaseCondition) {
	currentCond := GetReleaseCondition(*status, condition.Type)
	if currentCond != nil && currentCond.Status == condition.Status && currentCond.Reason == condition.Reason {
		return
	}
	if currentCond != nil && currentCond.Status == condition.Status {
		condition.LastTransitionTime = currentCond.LastTransitionTime
	}
	newConditions := filterOutCondition(status.Conditions, condition.Type)
	status.Conditions = append(newConditions, condition)
	sort.Slice(status.Conditions, func(i, j int) bool {
		return status.Conditions[i].Type < status.Conditions[j].Type
	})
}

func GetReleaseCondition(status shipperv1.ReleaseStatus, condType shipperv1.ReleaseConditionType) *shipperv1.ReleaseCondition {
	for i := range status.Conditions {
		c := status.Conditions[i]
		if c.Type == condType {
			return &c
		}
	}
	return nil
}

func RemoveReleaseCondition(status shipperv1.ReleaseStatus, condType shipperv1.ReleaseConditionType) {
	status.Conditions = filterOutCondition(status.Conditions, condType)
}

func ReleaseInstalled(release *shipperv1.Release) bool {
	installedCond := GetReleaseCondition(release.Status, shipperv1.ReleaseConditionTypeInstalled)
	return installedCond != nil && installedCond.Status == corev1.ConditionTrue
}

func ReleaseScheduled(release *shipperv1.Release) bool {
	scheduledCond := GetReleaseCondition(release.Status, shipperv1.ReleaseConditionTypeScheduled)
	return scheduledCond != nil && scheduledCond.Status == corev1.ConditionTrue
}

func ReleaseComplete(release *shipperv1.Release) bool {
	releasedCond := GetReleaseCondition(release.Status, shipperv1.ReleaseConditionTypeComplete)
	return releasedCond != nil && releasedCond.Status == corev1.ConditionTrue
}

func ReleaseProgressing(release *shipperv1.Release) bool {
	return !(ReleaseComplete(release))
}

func filterOutCondition(conditions []shipperv1.ReleaseCondition, condType shipperv1.ReleaseConditionType) []shipperv1.ReleaseCondition {
	var newConditions []shipperv1.ReleaseCondition
	for _, c := range conditions {
		if c.Type == condType {
			continue
		}
		newConditions = append(newConditions, c)
	}
	return newConditions
}
