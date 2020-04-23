package testing

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

func BuildTargetObjectsForRelease(release *shipper.Release) (*shipper.InstallationTarget, *shipper.TrafficTarget, *shipper.CapacityTarget) {
	labels := map[string]string{
		shipper.AppLabel:     release.Labels[shipper.AppLabel],
		shipper.ReleaseLabel: release.GetName(),
	}

	objmeta := metav1.ObjectMeta{
		Name:      release.GetName(),
		Namespace: release.GetNamespace(),
		Labels:    labels,
	}

	installationTarget := &shipper.InstallationTarget{
		ObjectMeta: *objmeta.DeepCopy(),
		Spec: shipper.InstallationTargetSpec{
			CanOverride: true,
			Chart:       release.Spec.Environment.Chart,
			Values:      release.Spec.Environment.Values,
		},
	}

	trafficTarget := &shipper.TrafficTarget{
		ObjectMeta: *objmeta.DeepCopy(),
		Spec:       shipper.TrafficTargetSpec{},
	}

	capacityTarget := &shipper.CapacityTarget{
		ObjectMeta: *objmeta.DeepCopy(),
		Spec: shipper.CapacityTargetSpec{
			Percent:           0,
			TotalReplicaCount: 12,
		},
	}

	return installationTarget, trafficTarget, capacityTarget
}
