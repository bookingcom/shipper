package conditions

import (
	"fmt"
	"reflect"
	"strings"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

func CondStr(ci interface{}) string {
	if ci == nil || reflect.ValueOf(ci).IsNil() {
		return ""
	}
	var chunks []string
	switch c := ci.(type) {
	case *shipper.ApplicationCondition:
		chunks = []string{
			fmt.Sprintf("%v", c.Type),
			fmt.Sprintf("%v", c.Status),
			c.Reason,
			c.Message,
		}
	case *shipper.ReleaseCondition:
		chunks = []string{
			fmt.Sprintf("%v", c.Type),
			fmt.Sprintf("%v", c.Status),
			c.Reason,
			c.Message,
		}
	case *shipper.ClusterCapacityCondition:
		chunks = []string{
			fmt.Sprintf("%v", c.Type),
			fmt.Sprintf("%v", c.Status),
			c.Reason,
			c.Message,
		}
	case *shipper.ClusterInstallationCondition:
		chunks = []string{
			fmt.Sprintf("%v", c.Type),
			fmt.Sprintf("%v", c.Status),
			c.Reason,
			c.Message,
		}
	case *shipper.ClusterTrafficCondition:
		chunks = []string{
			fmt.Sprintf("%v", c.Type),
			fmt.Sprintf("%v", c.Status),
			c.Reason,
			c.Message,
		}
	default:
		chunks = []string{fmt.Sprintf("Condition %v is not classified", ci)}
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
