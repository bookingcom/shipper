package application

import (
	"fmt"
	"sort"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/util/conditions"
	"github.com/bookingcom/shipper/pkg/util/diff"
)

var ConditionsShouldDiscardTimestamps = false

type ApplicationConditionDiff struct {
	c1, c2 *shipper.ApplicationCondition
}

var _ diff.Diff = (*ApplicationConditionDiff)(nil)

func NewApplicationConditionDiff(c1, c2 *shipper.ApplicationCondition) *ApplicationConditionDiff {
	return &ApplicationConditionDiff{
		c1: c1,
		c2: c2,
	}
}

func (d *ApplicationConditionDiff) IsEmpty() bool {
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

func (d *ApplicationConditionDiff) String() string {
	if d.IsEmpty() {
		return ""
	}
	c1str, c2str := conditions.CondStr(d.c1), conditions.CondStr(d.c2)
	return fmt.Sprintf("[%s] -> [%s]", c1str, c2str)
}

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

func SetApplicationCondition(status *shipper.ApplicationStatus, condition shipper.ApplicationCondition) diff.Diff {
	currentCond := GetApplicationCondition(*status, condition.Type)

	diff := NewApplicationConditionDiff(currentCond, &condition)
	if !diff.IsEmpty() {
		if currentCond != nil && currentCond.Status == condition.Status {
			condition.LastTransitionTime = currentCond.LastTransitionTime
		}
		newConditions := filterOutCondition(status.Conditions, condition.Type)
		status.Conditions = append(newConditions, condition)
		sort.Slice(status.Conditions, func(i, j int) bool {
			return status.Conditions[i].Type < status.Conditions[j].Type
		})
	}

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
