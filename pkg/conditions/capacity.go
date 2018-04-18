package conditions

import (
	"sort"
	"time"

	shipperV1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var CapacityConditionsShouldDiscardTimestamps = false

func SetCapacityCondition(
	conditions []shipperV1.ClusterCapacityCondition,
	typ shipperV1.ClusterConditionType,
	status coreV1.ConditionStatus,
	reason string,
	message string,
) []shipperV1.ClusterCapacityCondition {

	conditionIndex := -1
	for i := range conditions {
		if conditions[i].Type == typ {
			conditionIndex = i
			break
		}
	}

	if conditionIndex == -1 {
		lastTransitionTime := metaV1.Time{}
		if !CapacityConditionsShouldDiscardTimestamps {
			lastTransitionTime = metaV1.NewTime(time.Now())
		}
		aCondition := shipperV1.ClusterCapacityCondition{
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
			if CapacityConditionsShouldDiscardTimestamps {
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
