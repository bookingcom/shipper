package application

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

func TestConditionDiffIsEmpty(t *testing.T) {
	tests := []struct {
		Name         string
		Cond1, Cond2 *shipper.ApplicationCondition
		Expected     bool
	}{
		{
			Name:     "nil-nil",
			Cond1:    nil,
			Cond2:    nil,
			Expected: true,
		},
		{
			Name:     "first nil",
			Cond1:    nil,
			Cond2:    &shipper.ApplicationCondition{},
			Expected: false,
		},
		{
			Name:     "second nil",
			Cond1:    &shipper.ApplicationCondition{},
			Cond2:    nil,
			Expected: false,
		},
		{
			Name:     "empty conditions",
			Cond1:    &shipper.ApplicationCondition{},
			Cond2:    &shipper.ApplicationCondition{},
			Expected: true,
		},
		{
			Name: "equivalent conditions",
			Cond1: &shipper.ApplicationCondition{
				Type:               shipper.ApplicationConditionType("42"),
				Status:             corev1.ConditionStatus("42"),
				LastTransitionTime: metav1.Time{Time: time.Now()},
				Reason:             "reason 42",
				Message:            "message 42",
			},
			Cond2: &shipper.ApplicationCondition{
				Type:               shipper.ApplicationConditionType("42"),
				Status:             corev1.ConditionStatus("42"),
				LastTransitionTime: metav1.Time{Time: time.Now()},
				Reason:             "reason 42",
				Message:            "message 42",
			},
			Expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			d := NewApplicationConditionDiff(tt.Cond1, tt.Cond2)
			isEmpty := d.IsEmpty()
			if isEmpty != tt.Expected {
				t.Errorf("Unexpected result returned by IsEmpty: (diff: %#v): got: %t, want: %t", d, isEmpty, tt.Expected)
			}
		})
	}
}
