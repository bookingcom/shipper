package application

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

func TestComputeConditionDiff(t *testing.T) {
	tests := []struct {
		Name              string
		OriginalCondition *shipper.ApplicationCondition
		UpdatedCondition  *shipper.ApplicationCondition
		Expected          *ApplicationConditionDiff
	}{
		{
			Name:              "compare nils",
			OriginalCondition: nil,
			UpdatedCondition:  nil,
			Expected:          nil,
		},
		{
			Name: "compare equivalent types",
			OriginalCondition: &shipper.ApplicationCondition{
				Type: shipper.ApplicationConditionType("condition 1"),
			},
			UpdatedCondition: &shipper.ApplicationCondition{
				Type: shipper.ApplicationConditionType("condition 1"),
			},
			Expected: nil,
		},
		{
			Name: "compare distinct types",
			OriginalCondition: &shipper.ApplicationCondition{
				Type: shipper.ApplicationConditionType("condition 1"),
			},
			UpdatedCondition: &shipper.ApplicationCondition{
				Type: shipper.ApplicationConditionType("condition 2"),
			},
			Expected: &ApplicationConditionDiff{
				TypeDiff: &Diff{
					Original: shipper.ApplicationConditionType("condition 1"),
					Updated:  shipper.ApplicationConditionType("condition 2"),
				},
			},
		},
		{
			Name: "compare equivalent statuses",
			OriginalCondition: &shipper.ApplicationCondition{
				Status: corev1.ConditionStatus("status 1"),
			},
			UpdatedCondition: &shipper.ApplicationCondition{
				Status: corev1.ConditionStatus("status 1"),
			},
			Expected: nil,
		},
		{
			Name: "compare distinct statuses",
			OriginalCondition: &shipper.ApplicationCondition{
				Status: corev1.ConditionStatus("status 1"),
			},
			UpdatedCondition: &shipper.ApplicationCondition{
				Status: corev1.ConditionStatus("status 2"),
			},
			Expected: &ApplicationConditionDiff{
				StatusDiff: &Diff{
					Original: corev1.ConditionStatus("status 1"),
					Updated:  corev1.ConditionStatus("status 2"),
				},
			},
		},
		{
			Name: "compare equivalent reasons",
			OriginalCondition: &shipper.ApplicationCondition{
				Reason: "reason 1",
			},
			UpdatedCondition: &shipper.ApplicationCondition{
				Reason: "reason 1",
			},
			Expected: nil,
		},
		{
			Name: "compare distinct reasons",
			OriginalCondition: &shipper.ApplicationCondition{
				Reason: "reason 1",
			},
			UpdatedCondition: &shipper.ApplicationCondition{
				Reason: "reason 2",
			},
			Expected: &ApplicationConditionDiff{
				ReasonDiff: &Diff{
					Original: "reason 1",
					Updated:  "reason 2",
				},
			},
		},
		{
			Name: "compare equivalent messages",
			OriginalCondition: &shipper.ApplicationCondition{
				Message: "message 1",
			},
			UpdatedCondition: &shipper.ApplicationCondition{
				Message: "message 1",
			},
			Expected: nil,
		},
		{
			Name: "compare distinct messages",
			OriginalCondition: &shipper.ApplicationCondition{
				Message: "message 1",
			},
			UpdatedCondition: &shipper.ApplicationCondition{
				Message: "message 2",
			},
			Expected: &ApplicationConditionDiff{
				MessageDiff: &Diff{
					Original: "message 1",
					Updated:  "message 2",
				},
			},
		},
		{
			Name: "combined change",
			OriginalCondition: &shipper.ApplicationCondition{
				Type:    shipper.ApplicationConditionType("type 1"),
				Status:  corev1.ConditionStatus("status 1"),
				Reason:  "reason 1",
				Message: "message 1",
			},
			UpdatedCondition: &shipper.ApplicationCondition{
				Type:    shipper.ApplicationConditionType("type 2"),
				Status:  corev1.ConditionStatus("status 2"),
				Reason:  "reason 2",
				Message: "message 2",
			},
			Expected: &ApplicationConditionDiff{
				TypeDiff: &Diff{
					Original: shipper.ApplicationConditionType("type 1"),
					Updated:  shipper.ApplicationConditionType("type 2"),
				},
				StatusDiff: &Diff{
					Original: corev1.ConditionStatus("status 1"),
					Updated:  corev1.ConditionStatus("status 2"),
				},
				ReasonDiff: &Diff{
					Original: "reason 1",
					Updated:  "reason 2",
				},
				MessageDiff: &Diff{
					Original: "message 1",
					Updated:  "message 2",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			diff := ComputeConditionDiff(
				tt.OriginalCondition,
				tt.UpdatedCondition,
			)
			if !reflect.DeepEqual(diff, tt.Expected) {
				t.Errorf("Unexpected diff: want: %+v, got: %+v", tt.Expected, diff)
			}
		})
	}
}
