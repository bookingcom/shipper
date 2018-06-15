package conditions

import (
	"sort"
	"time"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipperV1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
)

var ReleaseConditionsShouldDiscardTimestamps = false

func IsReleaseConditionTrue(
	conditions []shipperV1.ReleaseCondition,
	typ shipperV1.ReleaseConditionType,
) bool {
	for _, e := range conditions {
		if e.Type == typ {
			return e.Status == coreV1.ConditionTrue
		}
	}
	return false
}

func SetReleaseCondition(
	conditions []shipperV1.ReleaseCondition,
	typ shipperV1.ReleaseConditionType,
	status coreV1.ConditionStatus,
	reason string,
	message string,
) []shipperV1.ReleaseCondition {

	conditionIndex := -1
	for i, e := range conditions {
		if e.Type == typ {
			conditionIndex = i
			break
		}
	}

	if conditionIndex == -1 {
		lastTransitionTime := metaV1.Time{}
		if !ReleaseConditionsShouldDiscardTimestamps {
			lastTransitionTime = metaV1.NewTime(time.Now())
		}
		aCondition := shipperV1.ReleaseCondition{
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
			if ReleaseConditionsShouldDiscardTimestamps {
				aCondition.LastTransitionTime = metaV1.Time{}
			} else {
				aCondition.LastTransitionTime = metaV1.NewTime(time.Now())
			}
		}
		aCondition.Status = status
		aCondition.Reason = reason
		aCondition.Message = message
	}

	return conditions
}

func IsReleaseInstalled(release *shipperV1.Release) bool {
	return release.Status.Phase == shipperV1.ReleasePhaseInstalled
}
