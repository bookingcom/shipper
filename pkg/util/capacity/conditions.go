package capacity

import (
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/util/conditions"
	"github.com/bookingcom/shipper/pkg/util/diff"
)

var CapacityConditionsShouldDiscardTimestamps = false

type ClusterCapacityConditionDiff struct {
	c1, c2 *shipper.ClusterCapacityCondition
}

var _ diff.Diff = (*ClusterCapacityConditionDiff)(nil)

func NewClusterCapacityConditionDiff(c1, c2 *shipper.ClusterCapacityCondition) *ClusterCapacityConditionDiff {
	return &ClusterCapacityConditionDiff{
		c1: c1,
		c2: c2,
	}
}

func (d *ClusterCapacityConditionDiff) IsEmpty() bool {
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

func (d *ClusterCapacityConditionDiff) String() string {
	if d.IsEmpty() {
		return ""
	}
	c1str, c2str := conditions.CondStr(d.c1), conditions.CondStr(d.c2)
	return fmt.Sprintf("[%s] -> [%s]", c1str, c2str)
}

func NewClusterCapacityCondition(condType shipper.ClusterConditionType, status corev1.ConditionStatus, reason, message string) *shipper.ClusterCapacityCondition {
	now := metav1.Now()
	if CapacityConditionsShouldDiscardTimestamps {
		now = metav1.Time{}
	}
	return &shipper.ClusterCapacityCondition{
		Type:               condType,
		Status:             status,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	}
}

func SetClusterCapacityCondition(status *shipper.ClusterCapacityStatus, condition shipper.ClusterCapacityCondition) diff.Diff {
	currentCond := GetClusterCapacityCondition(*status, condition.Type)

	diff := NewClusterCapacityConditionDiff(currentCond, &condition)
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

func GetClusterCapacityCondition(status shipper.ClusterCapacityStatus, condType shipper.ClusterConditionType) *shipper.ClusterCapacityCondition {
	for _, c := range status.Conditions {
		if c.Type == condType {
			return &c
		}
	}
	return nil
}

func filterOutCondition(conditions []shipper.ClusterCapacityCondition, condType shipper.ClusterConditionType) []shipper.ClusterCapacityCondition {
	var newConditions []shipper.ClusterCapacityCondition
	for _, c := range conditions {
		if c.Type == condType {
			continue
		}
		newConditions = append(newConditions, c)
	}
	return newConditions
}
