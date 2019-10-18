package traffic

import (
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/util/conditions"
	"github.com/bookingcom/shipper/pkg/util/diff"
)

var TrafficConditionsShouldDiscardTimestamps = false

type ClusterTrafficConditionDiff struct {
	c1, c2 *shipper.ClusterTrafficCondition
}

var _ diff.Diff = (*ClusterTrafficConditionDiff)(nil)

func NewClusterTrafficConditionDiff(c1, c2 *shipper.ClusterTrafficCondition) *ClusterTrafficConditionDiff {
	return &ClusterTrafficConditionDiff{
		c1: c1,
		c2: c2,
	}
}

func (d *ClusterTrafficConditionDiff) IsEmpty() bool {
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

func (d *ClusterTrafficConditionDiff) String() string {
	if d.IsEmpty() {
		return ""
	}
	c1str, c2str := conditions.CondStr(d.c1), conditions.CondStr(d.c2)
	return fmt.Sprintf("[%s] -> [%s]", c1str, c2str)
}

func NewClusterTrafficCondition(condType shipper.ClusterConditionType, status corev1.ConditionStatus, reason, message string) *shipper.ClusterTrafficCondition {
	now := metav1.Now()
	if TrafficConditionsShouldDiscardTimestamps {
		now = metav1.Time{}
	}
	return &shipper.ClusterTrafficCondition{
		Type:               condType,
		Status:             status,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	}
}

func SetClusterTrafficCondition(status *shipper.ClusterTrafficStatus, condition shipper.ClusterTrafficCondition) diff.Diff {
	currentCond := GetClusterTrafficCondition(*status, condition.Type)

	diff := NewClusterTrafficConditionDiff(currentCond, &condition)
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

func GetClusterTrafficCondition(status shipper.ClusterTrafficStatus, condType shipper.ClusterConditionType) *shipper.ClusterTrafficCondition {
	for _, c := range status.Conditions {
		if c.Type == condType {
			return &c
		}
	}
	return nil
}

func filterOutCondition(conditions []shipper.ClusterTrafficCondition, condType shipper.ClusterConditionType) []shipper.ClusterTrafficCondition {
	var newConditions []shipper.ClusterTrafficCondition
	for _, c := range conditions {
		if c.Type == condType {
			continue
		}
		newConditions = append(newConditions, c)
	}
	return newConditions
}
