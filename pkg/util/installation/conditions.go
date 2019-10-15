package installation

import (
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/util/diff"
)

var InstallationConditionsShouldDiscardTimestamps = false

type ClusterInstallationConditionDiff struct {
	c1, c2 *shipper.ClusterInstallationCondition
}

var _ diff.Diff = (*ClusterInstallationConditionDiff)(nil)

func NewClusterInstallationConditionDiff(c1, c2 *shipper.ClusterInstallationCondition) *ClusterInstallationConditionDiff {
	return &ClusterInstallationConditionDiff{
		c1: c1,
		c2: c2,
	}
}

func (d *ClusterInstallationConditionDiff) IsEmpty() bool {
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

func (d *ClusterInstallationConditionDiff) String() string {
	if d.IsEmpty() {
		return ""
	}
	c1str, c2str := condStr(d.c1), condStr(d.c2)
	return fmt.Sprintf("[%s] -> [%s]", c1str, c2str)
}

func NewClusterInstallationCondition(condType shipper.ClusterConditionType, status corev1.ConditionStatus, reason, message string) *shipper.ClusterInstallationCondition {
	now := metav1.Now()
	if InstallationConditionsShouldDiscardTimestamps {
		now = metav1.Time{}
	}
	return &shipper.ClusterInstallationCondition{
		Type:               condType,
		Status:             status,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	}
}

func SetClusterInstallationCondition(status *shipper.ClusterInstallationStatus, condition shipper.ClusterInstallationCondition) diff.Diff {
	currentCond := GetClusterInstallationCondition(*status, condition.Type)

	diff := NewClusterInstallationConditionDiff(currentCond, &condition)
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

func GetClusterInstallationCondition(status shipper.ClusterInstallationStatus, condType shipper.ClusterConditionType) *shipper.ClusterInstallationCondition {
	for _, c := range status.Conditions {
		if c.Type == condType {
			return &c
		}
	}
	return nil
}

func filterOutCondition(conditions []shipper.ClusterInstallationCondition, condType shipper.ClusterConditionType) []shipper.ClusterInstallationCondition {
	var newConditions []shipper.ClusterInstallationCondition
	for _, c := range conditions {
		if c.Type == condType {
			continue
		}
		newConditions = append(newConditions, c)
	}
	return newConditions
}

func condStr(c *shipper.ClusterInstallationCondition) string {
	if c == nil {
		return ""
	}
	chunks := []string{
		fmt.Sprintf("%v", c.Type),
		fmt.Sprintf("%v", c.Status),
		c.Reason,
		c.Message,
	}
	b := strings.Builder{}
	for _, ch := range chunks {
		if len(ch) > 0 {
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(ch)
		}
	}
	return b.String()
}
