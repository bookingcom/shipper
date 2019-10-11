package condition

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

type NoopCond struct{}

var _ Condition = (*NoopCond)(nil)

func TestCondEqual(t *testing.T) {
	tests := []struct {
		Name         string
		Cond1, Cond2 Condition
		Expected     bool
	}{
		{
			Name:     "nil-nil",
			Cond1:    nil,
			Cond2:    nil,
			Expected: true,
		},
		{
			Name:     "typed nil-nil",
			Cond1:    (*NoopCond)(nil),
			Cond2:    (*NoopCond)(nil),
			Expected: true,
		},
		{
			Name:     "first nil",
			Cond1:    nil,
			Cond2:    &NoopCond{},
			Expected: false,
		},
		{
			Name:     "second nil",
			Cond1:    &NoopCond{},
			Cond2:    nil,
			Expected: false,
		},
		{
			Name:     "both set but type is unknown",
			Cond1:    &NoopCond{},
			Cond2:    &NoopCond{},
			Expected: false,
		},
		{
			Name:     "empty ApplicationCondition",
			Cond1:    &shipper.ApplicationCondition{},
			Cond2:    &shipper.ApplicationCondition{},
			Expected: true,
		},
		{
			Name: "equal ApplicationCondition",
			Cond1: &shipper.ApplicationCondition{
				Type:    shipper.ApplicationConditionType("type 42"),
				Status:  corev1.ConditionStatus("status 42"),
				Reason:  "reason 42",
				Message: "message 42",
			},
			Cond2: &shipper.ApplicationCondition{
				Type:    shipper.ApplicationConditionType("type 42"),
				Status:  corev1.ConditionStatus("status 42"),
				Reason:  "reason 42",
				Message: "message 42",
			},
			Expected: true,
		},
		{
			Name: "distinct ApplicationCondition",
			Cond1: &shipper.ApplicationCondition{
				Type:    shipper.ApplicationConditionType("type 1"),
				Status:  corev1.ConditionStatus("status 1"),
				Reason:  "reason 1",
				Message: "message 1",
			},
			Cond2: &shipper.ApplicationCondition{
				Type:    shipper.ApplicationConditionType("type 2"),
				Status:  corev1.ConditionStatus("status 2"),
				Reason:  "reason 2",
				Message: "message 2",
			},
			Expected: false,
		},
		{
			Name:     "empty ReleaseCondition",
			Cond1:    &shipper.ReleaseCondition{},
			Cond2:    &shipper.ReleaseCondition{},
			Expected: true,
		},
		{
			Name: "equal ReleaseCondition",
			Cond1: &shipper.ReleaseCondition{
				Type:    shipper.ReleaseConditionType("type 42"),
				Status:  corev1.ConditionStatus("status 42"),
				Reason:  "reason 42",
				Message: "message 42",
			},
			Cond2: &shipper.ReleaseCondition{
				Type:    shipper.ReleaseConditionType("type 42"),
				Status:  corev1.ConditionStatus("status 42"),
				Reason:  "reason 42",
				Message: "message 42",
			},
			Expected: true,
		},
		{
			Name: "distinct ReleaseCondition",
			Cond1: &shipper.ReleaseCondition{
				Type:    shipper.ReleaseConditionType("type 1"),
				Status:  corev1.ConditionStatus("status 1"),
				Reason:  "reason 1",
				Message: "message 1",
			},
			Cond2: &shipper.ReleaseCondition{
				Type:    shipper.ReleaseConditionType("type 2"),
				Status:  corev1.ConditionStatus("status 2"),
				Reason:  "reason 2",
				Message: "message 2",
			},
			Expected: false,
		},

		{
			Name:     "empty ClusterInstallationCondition",
			Cond1:    &shipper.ClusterInstallationCondition{},
			Cond2:    &shipper.ClusterInstallationCondition{},
			Expected: true,
		},
		{
			Name: "equal ClusterInstallationCondition",
			Cond1: &shipper.ClusterInstallationCondition{
				Type:    shipper.ClusterConditionType("type 42"),
				Status:  corev1.ConditionStatus("status 42"),
				Reason:  "reason 42",
				Message: "message 42",
			},
			Cond2: &shipper.ClusterInstallationCondition{
				Type:    shipper.ClusterConditionType("type 42"),
				Status:  corev1.ConditionStatus("status 42"),
				Reason:  "reason 42",
				Message: "message 42",
			},
			Expected: true,
		},
		{
			Name: "distinct ClusterInstallationCondition",
			Cond1: &shipper.ClusterInstallationCondition{
				Type:    shipper.ClusterConditionType("type 1"),
				Status:  corev1.ConditionStatus("status 1"),
				Reason:  "reason 1",
				Message: "message 1",
			},
			Cond2: &shipper.ClusterInstallationCondition{
				Type:    shipper.ClusterConditionType("type 2"),
				Status:  corev1.ConditionStatus("status 2"),
				Reason:  "reason 2",
				Message: "message 2",
			},
			Expected: false,
		},

		{
			Name:     "empty ClusterCapacityCondition",
			Cond1:    &shipper.ClusterCapacityCondition{},
			Cond2:    &shipper.ClusterCapacityCondition{},
			Expected: true,
		},
		{
			Name: "equal ClusterCapacityCondition",
			Cond1: &shipper.ClusterCapacityCondition{
				Type:    shipper.ClusterConditionType("type 42"),
				Status:  corev1.ConditionStatus("status 42"),
				Reason:  "reason 42",
				Message: "message 42",
			},
			Cond2: &shipper.ClusterCapacityCondition{
				Type:    shipper.ClusterConditionType("type 42"),
				Status:  corev1.ConditionStatus("status 42"),
				Reason:  "reason 42",
				Message: "message 42",
			},
			Expected: true,
		},
		{
			Name: "distinct ClusterCapacityCondition",
			Cond1: &shipper.ClusterCapacityCondition{
				Type:    shipper.ClusterConditionType("type 1"),
				Status:  corev1.ConditionStatus("status 1"),
				Reason:  "reason 1",
				Message: "message 1",
			},
			Cond2: &shipper.ClusterCapacityCondition{
				Type:    shipper.ClusterConditionType("type 2"),
				Status:  corev1.ConditionStatus("status 2"),
				Reason:  "reason 2",
				Message: "message 2",
			},
			Expected: false,
		},

		{
			Name:     "empty ClusterTrafficCondition",
			Cond1:    &shipper.ClusterTrafficCondition{},
			Cond2:    &shipper.ClusterTrafficCondition{},
			Expected: true,
		},
		{
			Name: "equal ClusterTrafficCondition",
			Cond1: &shipper.ClusterTrafficCondition{
				Type:    shipper.ClusterConditionType("type 42"),
				Status:  corev1.ConditionStatus("status 42"),
				Reason:  "reason 42",
				Message: "message 42",
			},
			Cond2: &shipper.ClusterTrafficCondition{
				Type:    shipper.ClusterConditionType("type 42"),
				Status:  corev1.ConditionStatus("status 42"),
				Reason:  "reason 42",
				Message: "message 42",
			},
			Expected: true,
		},
		{
			Name: "distinct ClusterTrafficCondition",
			Cond1: &shipper.ClusterTrafficCondition{
				Type:    shipper.ClusterConditionType("type 1"),
				Status:  corev1.ConditionStatus("status 1"),
				Reason:  "reason 1",
				Message: "message 1",
			},
			Cond2: &shipper.ClusterTrafficCondition{
				Type:    shipper.ClusterConditionType("type 2"),
				Status:  corev1.ConditionStatus("status 2"),
				Reason:  "reason 2",
				Message: "message 2",
			},
			Expected: false,
		},

		{
			Name:     "empty ReleaseStrategyCondition",
			Cond1:    &shipper.ReleaseStrategyCondition{},
			Cond2:    &shipper.ReleaseStrategyCondition{},
			Expected: true,
		},
		{
			Name: "equal ReleaseStrategyCondition",
			Cond1: &shipper.ReleaseStrategyCondition{
				Type:    shipper.StrategyConditionType("type 42"),
				Status:  corev1.ConditionStatus("status 42"),
				Step:    42,
				Reason:  "reason 42",
				Message: "message 42",
			},
			Cond2: &shipper.ReleaseStrategyCondition{
				Type:    shipper.StrategyConditionType("type 42"),
				Status:  corev1.ConditionStatus("status 42"),
				Step:    42,
				Reason:  "reason 42",
				Message: "message 42",
			},
			Expected: true,
		},
		{
			Name: "distinct ReleaseStrategyCondition",
			Cond1: &shipper.ReleaseStrategyCondition{
				Type:    shipper.StrategyConditionType("type 1"),
				Status:  corev1.ConditionStatus("status 1"),
				Step:    42,
				Reason:  "reason 1",
				Message: "message 1",
			},
			Cond2: &shipper.ReleaseStrategyCondition{
				Type:    shipper.StrategyConditionType("type 2"),
				Status:  corev1.ConditionStatus("status 2"),
				Step:    42,
				Reason:  "reason 2",
				Message: "message 2",
			},
			Expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			eq := condEqual(tt.Cond1, tt.Cond2)
			if eq != tt.Expected {
				t.Errorf("unexpected result rerurned by condEqual(): got: %t, want: %t", eq, tt.Expected)
			}
		})
	}
}
