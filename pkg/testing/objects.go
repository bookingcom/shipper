package testing

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

func BuildTargetObjectsForRelease(release *shipper.Release) (*shipper.InstallationTarget, *shipper.TrafficTarget, *shipper.CapacityTarget) {
	ownerReferences := []metav1.OwnerReference{
		metav1.OwnerReference{
			APIVersion: shipper.SchemeGroupVersion.String(),
			Kind:       "Release",
			Name:       release.GetName(),
			UID:        release.GetUID(),
		},
	}
	labels := map[string]string{
		shipper.AppLabel:     release.Labels[shipper.AppLabel],
		shipper.ReleaseLabel: release.GetName(),
	}

	clusters := releaseutil.GetSelectedClusters(release)

	clusterTrafficTargets := make([]shipper.ClusterTrafficTarget, 0, len(clusters))

	for _, cluster := range clusters {
		clusterTrafficTargets = append(
			clusterTrafficTargets,
			shipper.ClusterTrafficTarget{
				Name: cluster,
			})
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
		ObjectMeta: metav1.ObjectMeta{
			Name:            release.Name,
			Namespace:       release.GetNamespace(),
			OwnerReferences: ownerReferences,
			Labels:          labels,
		},
		Spec: shipper.TrafficTargetSpec{
			Clusters: clusterTrafficTargets,
		},
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
