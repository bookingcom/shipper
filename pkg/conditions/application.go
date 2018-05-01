package conditions

import (
	"sort"
	"time"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipperV1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
)

var ApplicationConditionsShouldDiscardTimestamps = false

func SetApplicationCondition(
	conditions []shipperV1.ApplicationCondition,
	typ shipperV1.ApplicationConditionType,
	status coreV1.ConditionStatus,
	reason string,
	message string,
) []shipperV1.ApplicationCondition {

	conditionIndex := -1
	for i, e := range conditions {
		if e.Type == typ {
			conditionIndex = i
			break
		}
	}

	if conditionIndex == -1 {
		lastTransitionTime := metaV1.Time{}
		if !ApplicationConditionsShouldDiscardTimestamps {
			lastTransitionTime = metaV1.NewTime(time.Now())
		}
		aCondition := shipperV1.ApplicationCondition{
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
			if ApplicationConditionsShouldDiscardTimestamps {
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
