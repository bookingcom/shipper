package application

import (
	"sort"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	condutil "github.com/bookingcom/shipper/pkg/util/condition"
	diffutil "github.com/bookingcom/shipper/pkg/util/diff"
)

var ConditionsShouldDiscardTimestamps = false

func NewApplicationCondition(condType shipper.ApplicationConditionType, status coreV1.ConditionStatus, reason, message string) *shipper.ApplicationCondition {
	now := metaV1.Now()
	if ConditionsShouldDiscardTimestamps {
		now = metaV1.Time{}
	}
	return &shipper.ApplicationCondition{
		Type:               condType,
		Status:             status,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	}
}

func SetApplicationCondition(status *shipper.ApplicationStatus, condition shipper.ApplicationCondition) diffutil.Diff {
	currentCond := GetApplicationCondition(*status, condition.Type)

	diff := condutil.NewConditionDiff(currentCond, &condition)
	if diff.IsEmpty() {
		return nil
	}

	if currentCond != nil && currentCond.Status == condition.Status {
		condition.LastTransitionTime = currentCond.LastTransitionTime
	}
	newConditions := filterOutCondition(status.Conditions, condition.Type)
	status.Conditions = append(newConditions, condition)
	sort.Slice(status.Conditions, func(i, j int) bool {
		return status.Conditions[i].Type < status.Conditions[j].Type
	})

	return diff
}

func GetApplicationCondition(status shipper.ApplicationStatus, condType shipper.ApplicationConditionType) *shipper.ApplicationCondition {
	for _, c := range status.Conditions {
		if c.Type == condType {
			return &c
		}
	}
	return nil
}

func filterOutCondition(conditions []shipper.ApplicationCondition, condType shipper.ApplicationConditionType) []shipper.ApplicationCondition {
	var newConditions []shipper.ApplicationCondition
	for _, c := range conditions {
		if c.Type == condType {
			continue
		}
		newConditions = append(newConditions, c)
	}
	return newConditions
}
