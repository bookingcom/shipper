package target

import (
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/util/conditions"
	"github.com/bookingcom/shipper/pkg/util/diff"
)

var ConditionsShouldDiscardTimestamps = false

type TargetConditionDiff struct {
	c1, c2 *shipper.TargetCondition
}

var _ diff.Diff = (*TargetConditionDiff)(nil)

func NewTargetConditionDiff(c1, c2 *shipper.TargetCondition) *TargetConditionDiff {
	return &TargetConditionDiff{
		c1: c1,
		c2: c2,
	}
}

func (d *TargetConditionDiff) IsEmpty() bool {
	if d.c1 == nil && d.c2 == nil {
		return true
	}
	if d.c1 == nil || d.c2 == nil {
		return false
	}
	return d.c1.Type == d.c2.Type &&
		d.c1.Status == d.c2.Status &&
		d.c1.Reason == d.c2.Reason &&
		d.c1.Message == d.c2.Message
}

func (d *TargetConditionDiff) String() string {
	if d.IsEmpty() {
		return ""
	}
	c1str, c2str := conditions.CondStr(d.c1), conditions.CondStr(d.c2)
	return fmt.Sprintf("[%s] -> [%s]", c1str, c2str)
}

func IsReady(conditions []shipper.TargetCondition) (bool, string) {
	cond := GetTargetCondition(conditions, shipper.TargetConditionTypeReady)
	if cond == nil {
		return false, ""
	}

	return cond.Status == corev1.ConditionTrue, cond.Message
}

func TransitionToReady(
	multidiff *diff.MultiDiff,
	conditions []shipper.TargetCondition,
) []shipper.TargetCondition {
	newCond := NewTargetCondition(
		shipper.TargetConditionTypeReady,
		corev1.ConditionTrue, "", "")
	conditions, diff := SetTargetCondition(conditions, newCond)

	multidiff.Append(diff)

	return conditions
}

func TransitionToNotReady(
	multidiff *diff.MultiDiff,
	conditions []shipper.TargetCondition,
	reason, msg string,
) []shipper.TargetCondition {
	newCond := NewTargetCondition(
		shipper.TargetConditionTypeReady,
		corev1.ConditionFalse, reason, msg)
	conditions, diff := SetTargetCondition(conditions, newCond)

	multidiff.Append(diff)

	return conditions
}

func TransitionToOperational(
	multidiff *diff.MultiDiff,
	conditions []shipper.TargetCondition,
) []shipper.TargetCondition {
	newCond := NewTargetCondition(
		shipper.TargetConditionTypeOperational,
		corev1.ConditionTrue, "", "")
	conditions, diff := SetTargetCondition(conditions, newCond)

	multidiff.Append(diff)

	return conditions
}

func TransitionToNotOperational(
	multidiff *diff.MultiDiff,
	conditions []shipper.TargetCondition,
	reason, msg string,
) []shipper.TargetCondition {
	newCond := NewTargetCondition(
		shipper.TargetConditionTypeOperational,
		corev1.ConditionFalse, reason, msg)
	conditions, diff := SetTargetCondition(conditions, newCond)

	multidiff.Append(diff)

	return conditions
}

func SetTargetCondition(
	conditions []shipper.TargetCondition,
	condition shipper.TargetCondition,
) ([]shipper.TargetCondition, diff.Diff) {
	currentCond := GetTargetCondition(conditions, condition.Type)

	diff := NewTargetConditionDiff(currentCond, &condition)
	if !diff.IsEmpty() {
		if currentCond != nil && currentCond.Status == condition.Status {
			condition.LastTransitionTime = currentCond.LastTransitionTime
		}
		newConditions := filterOutCondition(conditions, condition.Type)
		conditions = append(newConditions, condition)
		sort.Slice(conditions, func(i, j int) bool {
			return conditions[i].Type < conditions[j].Type
		})
	}

	return conditions, diff
}

func GetTargetCondition(conditions []shipper.TargetCondition, condType shipper.TargetConditionType) *shipper.TargetCondition {
	for _, c := range conditions {
		if c.Type == condType {
			return &c
		}
	}

	return nil
}

func NewTargetCondition(
	condType shipper.TargetConditionType,
	status corev1.ConditionStatus,
	reason, message string,
) shipper.TargetCondition {
	now := metav1.Now()
	if ConditionsShouldDiscardTimestamps {
		now = metav1.Time{}
	}
	return shipper.TargetCondition{
		Type:               condType,
		Status:             status,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	}
}

func filterOutCondition(
	conditions []shipper.TargetCondition,
	condType shipper.TargetConditionType,
) []shipper.TargetCondition {
	var newConditions []shipper.TargetCondition
	for _, c := range conditions {
		if c.Type == condType {
			continue
		}
		newConditions = append(newConditions, c)
	}
	return newConditions
}
