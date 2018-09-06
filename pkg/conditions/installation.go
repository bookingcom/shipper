package conditions

import (
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
)

var InstallationConditionsShouldDiscardTimestamps = false

func IsInstallationConditionTrue(
	conditions []shipperv1.ClusterInstallationCondition,
	typ shipperv1.ClusterConditionType,
) bool {
	for _, e := range conditions {
		if e.Type == typ {
			return e.Status == corev1.ConditionTrue
		}
	}
	return false
}

func SetInstallationCondition(
	conditions []shipperv1.ClusterInstallationCondition,
	typ shipperv1.ClusterConditionType,
	status corev1.ConditionStatus,
	reason string,
	message string,
) []shipperv1.ClusterInstallationCondition {
	conditionIndex := -1
	for i, e := range conditions {
		if e.Type == typ {
			conditionIndex = i
			break
		}
	}

	if conditionIndex == -1 {
		lastTransitionTime := metav1.Time{}
		if !InstallationConditionsShouldDiscardTimestamps {
			lastTransitionTime = metav1.NewTime(time.Now())
		}
		aCondition := shipperv1.ClusterInstallationCondition{
			Type:               typ,
			Status:             status,
			LastTransitionTime: lastTransitionTime,
			Reason:             reason,
			Message:            message,
		}
		conditions = append(conditions, aCondition)
		sort.Slice(conditions, func(i, j int) bool {
			return conditions[i].Type < conditions[j].Type
		})
	} else {
		aCondition := &conditions[conditionIndex]
		if aCondition.Status != status {
			if InstallationConditionsShouldDiscardTimestamps {
				aCondition.LastTransitionTime = metav1.Time{}
			} else {
				aCondition.LastTransitionTime = metav1.NewTime(time.Now())
			}
		}
		aCondition.Status = status
		aCondition.Reason = reason
		aCondition.Message = message
	}

	return conditions
}
