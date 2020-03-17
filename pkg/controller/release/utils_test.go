package release

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

func pint32(i int32) *int32 {
	return &i
}

func buildRelease() *shipper.Release {
	return &shipper.Release{
		TypeMeta: metav1.TypeMeta{
			APIVersion: shipper.SchemeGroupVersion.String(),
			Kind:       "Release",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-release",
			Namespace: shippertesting.TestNamespace,
			Annotations: map[string]string{
				shipper.ReleaseGenerationAnnotation: "1",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: shipper.SchemeGroupVersion.String(),
					Kind:       "Application",
					Name:       "test-application",
				},
			},
			Labels: map[string]string{
				shipper.ReleaseLabel: "test-release",
				shipper.AppLabel:     "test-application",
			},
		},
		Spec: shipper.ReleaseSpec{
			Environment: shipper.ReleaseEnvironment{
				Strategy: &vanguard,
				Chart: shipper.Chart{
					Name:    "simple",
					Version: "0.0.1",
				},
				ClusterRequirements: shipper.ClusterRequirements{
					Regions: []shipper.RegionRequirement{{Name: shippertesting.TestRegion}},
				},
			},
		},
		Status: shipper.ReleaseStatus{
			Conditions: []shipper.ReleaseCondition{
				{Type: shipper.ReleaseConditionTypeBlocked, Status: corev1.ConditionFalse},
				{Type: shipper.ReleaseConditionTypeScheduled, Status: corev1.ConditionFalse},
			},
		},
	}
}
