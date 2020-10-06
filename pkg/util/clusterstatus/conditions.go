package clusterstatus

import (
	"fmt"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

// TODO(jgreff): needing to create a set of IsInstallationReady,
// IsCapacityReady, IsTrafficReady means there's opportunity to merge those
// conditions into a single type, or wrap them in an interface.

func IsClusterTrafficReady(conditions []shipper.ClusterTrafficCondition) bool {
	var readyCond shipper.ClusterTrafficCondition
	for _, c := range conditions {
		if c.Type == shipper.ClusterConditionTypeReady {
			readyCond = c
			break
		}
	}

	return readyCond.Status == corev1.ConditionTrue
}

func IsClusterCapacityReady(conditions []shipper.ClusterCapacityCondition) (bool, string) {
	var readyCond shipper.ClusterCapacityCondition
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

func IsClusterInstallationReady(conditions []shipper.ClusterInstallationCondition) bool {
	var readyCond shipper.ClusterInstallationCondition
	for _, c := range conditions {
		if c.Type == shipper.ClusterConditionTypeReady {
			readyCond = c
			break
		}
	}

	return readyCond.Status == corev1.ConditionTrue
}
