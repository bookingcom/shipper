package clusterstatus

import (
	"fmt"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

func IsClusterTrafficReady(conditions []shipper.ClusterTrafficCondition) (bool, string) {
	var readyCond shipper.ClusterTrafficCondition
	for _, c := range conditions {
		if c.Type == shipper.ClusterConditionTypeReady {
			readyCond = c
			break
		}
	}

	if readyCond.Status != corev1.ConditionTrue {
		msg := readyCond.Reason

		if readyCond.Message != "" {
			msg = fmt.Sprintf("%s %s", msg, readyCond.Message)
		}

		return false, msg
	}

	return true, ""
}
