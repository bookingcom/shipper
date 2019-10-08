package application

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

type Diff struct {
	Original interface{}
	Updated  interface{}
}

func (d *Diff) IsEmpty() bool {
	return d == nil || reflect.DeepEqual(d.Original, d.Updated)
}

func (d *Diff) String() string {
	if d.IsEmpty() {
		return ""
	}
	return fmt.Sprintf("[%+v] -> [%+v]", d.Original, d.Updated)
}

func ComputeConditionDiff(cond1, cond2 *shipper.ApplicationCondition) *ApplicationConditionDiff {
	var typeDiff, statusDiff, reasonDiff, messageDiff Diff
	if cond1 != nil {
		typeDiff.Original = cond1.Type
		statusDiff.Original = cond1.Status
		reasonDiff.Original = cond1.Reason
		messageDiff.Original = cond1.Message
	}
	if cond2 != nil {
		typeDiff.Updated = cond2.Type
		statusDiff.Updated = cond2.Status
		reasonDiff.Updated = cond2.Reason
		messageDiff.Updated = cond2.Message
	}
	diff := &ApplicationConditionDiff{}
	if !typeDiff.IsEmpty() {
		diff.TypeDiff = &typeDiff
	}
	if !statusDiff.IsEmpty() {
		diff.StatusDiff = &statusDiff
	}
	if !reasonDiff.IsEmpty() {
		diff.ReasonDiff = &reasonDiff
	}
	if !messageDiff.IsEmpty() {
		diff.MessageDiff = &messageDiff
	}
	if diff.IsEmpty() {
		return nil
	}
	return diff
}

type ApplicationConditionDiff struct {
	TypeDiff    *Diff
	StatusDiff  *Diff
	ReasonDiff  *Diff
	MessageDiff *Diff
}

func (d *ApplicationConditionDiff) IsEmpty() bool {
	return d == nil ||
		(d.TypeDiff.IsEmpty() &&
			d.StatusDiff.IsEmpty() &&
			d.ReasonDiff.IsEmpty() &&
			d.MessageDiff.IsEmpty())
}

func (d *ApplicationConditionDiff) String() string {
	if d.IsEmpty() {
		return ""
	}
	b := make([]string, 0, 4)
	for _, d := range []*Diff{d.TypeDiff, d.StatusDiff, d.ReasonDiff, d.MessageDiff} {
		if !d.IsEmpty() {
			b = append(b, d.String())
		}
	}
	return strings.Join(b, ", ")
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

func SetApplicationCondition(status *shipper.ApplicationStatus, condition shipper.ApplicationCondition) *ApplicationConditionDiff {
	currentCond := GetApplicationCondition(*status, condition.Type)

	diff := ComputeConditionDiff(currentCond, &condition)
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
