package application

import (
	"fmt"
	"sort"
	"strings"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	diffutil "github.com/bookingcom/shipper/pkg/util/diff"
)

type ConditionDiff struct {
	cond1, cond2 *shipper.ApplicationCondition
}

var _ diffutil.Diff = (*ConditionDiff)(nil)

func NewConditionDiff(cond1, cond2 *shipper.ApplicationCondition) *ConditionDiff {
	return &ConditionDiff{cond1: cond1, cond2: cond2}
}

func (d *ConditionDiff) IsEmpty() bool {
	if d == nil || (d.cond1 == nil && d.cond2 == nil) {
		return true
	}
	if d.cond1 != nil && d.cond2 != nil {
		return d.cond1.Type == d.cond2.Type &&
			d.cond1.Message == d.cond2.Message &&
			d.cond1.Reason == d.cond2.Reason &&
			d.cond1.Status == d.cond2.Status
	}
	return false
}

func (d *ConditionDiff) String() string {
	if d.IsEmpty() {
		return ""
	}
	var cond1str, cond2str string
	if d.cond1 != nil {
		cond1str = condToString(d.cond1)
	}
	if d.cond2 != nil {
		cond2str = condToString(d.cond2)
	}
	return fmt.Sprintf("[%s] -> [%s]", cond1str, cond2str)
}

func condToString(cond *shipper.ApplicationCondition) string {
	if cond == nil {
		return ""
	}
	b := strings.Builder{}
	b.WriteString(fmt.Sprintf("%v", cond.Type))
	b.WriteString(fmt.Sprintf(" %v", cond.Status))
	if cond.Reason != "" {
		b.WriteString(fmt.Sprintf(" %q", cond.Reason))
	}
	if cond.Message != "" {
		b.WriteString(fmt.Sprintf(" %q", cond.Message))
	}
	return b.String()
}

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

	diff := NewConditionDiff(currentCond, &condition)
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
