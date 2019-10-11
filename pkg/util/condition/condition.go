package condition

import (
	"fmt"
	"reflect"
	"strings"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	diffutil "github.com/bookingcom/shipper/pkg/util/diff"
)

type Condition interface{}

type ConditionDiff struct {
	c1, c2 Condition
}

var _ diffutil.Diff = (*ConditionDiff)(nil)

func NewConditionDiff(c1, c2 Condition) *ConditionDiff {
	return &ConditionDiff{
		c1: c1,
		c2: c2,
	}
}

func (cd *ConditionDiff) IsEmpty() bool {
	if cd == nil || (condIsNil(cd.c1) && condIsNil(cd.c2)) {
		return true
	}
	if condIsNil(cd.c1) || condIsNil(cd.c2) {
		return false
	}
	return condEqual(cd.c1, cd.c2)
}

func (cd *ConditionDiff) String() string {
	if cd.IsEmpty() {
		return ""
	}
	var c1str, c2str string
	if cd.c1 != nil {
		c1str = condStr(cd.c1)
	}
	if cd.c2 != nil {
		c2str = condStr(cd.c2)
	}
	return fmt.Sprintf("[%s] -> [%s]", c1str, c2str)
}

func condEqual(c1, c2 Condition) bool {
	if condIsNil(c1) && condIsNil(c2) {
		return true
	}
	if condIsNil(c1) || condIsNil(c2) {
		return false
	}
	// Clumsy :-(
	switch c1t := c1.(type) {
	case *shipper.ApplicationCondition:
		if c2t, ok := c2.(*shipper.ApplicationCondition); ok {
			return c1t.Type == c2t.Type &&
				c1t.Status == c2t.Status &&
				c1t.Reason == c2t.Reason &&
				c1t.Message == c2t.Message
		}
	case *shipper.ReleaseCondition:
		if c2t, ok := c2.(*shipper.ReleaseCondition); ok {
			return c1t.Type == c2t.Type &&
				c1t.Status == c2t.Status &&
				c1t.Reason == c2t.Reason &&
				c1t.Message == c2t.Message
		}
	case *shipper.ClusterInstallationCondition:
		if c2t, ok := c2.(*shipper.ClusterInstallationCondition); ok {
			return c1t.Type == c2t.Type &&
				c1t.Status == c2t.Status &&
				c1t.Reason == c2t.Reason &&
				c1t.Message == c2t.Message
		}
	case *shipper.ClusterCapacityCondition:
		if c2t, ok := c2.(*shipper.ClusterCapacityCondition); ok {
			return c1t.Type == c2t.Type &&
				c1t.Status == c2t.Status &&
				c1t.Reason == c2t.Reason &&
				c1t.Message == c2t.Message
		}
	case *shipper.ClusterTrafficCondition:
		if c2t, ok := c2.(*shipper.ClusterTrafficCondition); ok {
			return c1t.Type == c2t.Type &&
				c1t.Status == c2t.Status &&
				c1t.Reason == c2t.Reason &&
				c1t.Message == c2t.Message
		}
	case *shipper.ReleaseStrategyCondition:
		if c2t, ok := c2.(*shipper.ReleaseStrategyCondition); ok {
			return c1t.Type == c2t.Type &&
				c1t.Status == c2t.Status &&
				c1t.Step == c2t.Step &&
				c1t.Reason == c2t.Reason &&
				c1t.Message == c2t.Message
		}
	}
	return false
}

func condStr(c Condition) string {
	if condIsNil(c) {
		return ""
	}
	var chunks []string
	// Clumsy again
	switch ct := c.(type) {
	case *shipper.ApplicationCondition:
		chunks = []string{
			fmt.Sprintf("%v", ct.Type),
			fmt.Sprintf("%v", ct.Status),
			ct.Reason,
			ct.Message,
		}
	case *shipper.ReleaseCondition:
		chunks = []string{
			fmt.Sprintf("%v", ct.Type),
			fmt.Sprintf("%v", ct.Status),
			ct.Reason,
			ct.Message,
		}
	case *shipper.ClusterInstallationCondition:
		chunks = []string{
			fmt.Sprintf("%v", ct.Type),
			fmt.Sprintf("%v", ct.Status),
			ct.Reason,
			ct.Message,
		}
	case *shipper.ClusterCapacityCondition:
		chunks = []string{
			fmt.Sprintf("%v", ct.Type),
			fmt.Sprintf("%v", ct.Status),
			ct.Reason,
			ct.Message,
		}
	case *shipper.ClusterTrafficCondition:
		chunks = []string{
			fmt.Sprintf("%v", ct.Type),
			fmt.Sprintf("%v", ct.Status),
			ct.Reason,
			ct.Message,
		}
	case *shipper.ReleaseStrategyCondition:
		chunks = []string{
			fmt.Sprintf("%v", ct.Type),
			fmt.Sprintf("%v", ct.Status),
			fmt.Sprintf("%d", ct.Step),
			ct.Reason,
			ct.Message,
		}
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

func condIsNil(c Condition) bool {
	return c == nil || reflect.ValueOf(c).IsNil()
}
