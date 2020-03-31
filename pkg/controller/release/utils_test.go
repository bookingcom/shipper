package release

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubetesting "k8s.io/client-go/testing"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

var (
	TargetConditionOperational = shipper.TargetCondition{
		Type:   shipper.TargetConditionTypeOperational,
		Status: corev1.ConditionTrue,
	}
	TargetConditionReady = shipper.TargetCondition{
		Type:   shipper.TargetConditionTypeReady,
		Status: corev1.ConditionTrue,
	}

	vanguard = shipper.RolloutStrategy{
		Steps: []shipper.RolloutStrategyStep{
			{
				Name:     "staging",
				Capacity: shipper.RolloutStrategyStepValue{Incumbent: 100, Contender: 1},
				Traffic:  shipper.RolloutStrategyStepValue{Incumbent: 100, Contender: 0},
			},
			{
				Name:     "50/50",
				Capacity: shipper.RolloutStrategyStepValue{Incumbent: 50, Contender: 50},
				Traffic:  shipper.RolloutStrategyStepValue{Incumbent: 50, Contender: 50},
			},
			{
				Name:     "full on",
				Capacity: shipper.RolloutStrategyStepValue{Incumbent: 0, Contender: 100},
				Traffic:  shipper.RolloutStrategyStepValue{Incumbent: 0, Contender: 100},
			},
		},
	}
)

func pint32(i int32) *int32 {
	return &i
}

func buildRelease(
	namespace, app, name string,
	replicaCount int32,
) *shipper.Release {
	relName := fmt.Sprintf("%s-%s", app, name)
	ownerRef := metav1.OwnerReference{
		APIVersion: shipper.SchemeGroupVersion.String(),
		Kind:       "Application",
		Name:       app,
	}
	clusterRequirements := shipper.ClusterRequirements{
		Regions: []shipper.RegionRequirement{
			{
				Name:     shippertesting.TestRegion,
				Replicas: pint32(replicaCount),
			},
		},
	}

	return &shipper.Release{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       namespace,
			Name:            relName,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
			Annotations: map[string]string{
				shipper.ReleaseGenerationAnnotation: GenerationInactive,
			},
			Labels: map[string]string{
				shipper.ReleaseLabel: relName,
				shipper.AppLabel:     app,
			},
		},
		Spec: shipper.ReleaseSpec{
			Environment: shipper.ReleaseEnvironment{
				Strategy: &vanguard,
				Chart: shipper.Chart{
					Name:    "simple",
					Version: "0.0.1",
				},
				ClusterRequirements: clusterRequirements,
			},
		},
	}
}

func buildCluster(name string) *shipper.Cluster {
	return &shipper.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: shipper.ClusterSpec{
			Capabilities: []string{},
			Region:       shippertesting.TestRegion,
		},
	}
}

func buildRolloutBlock(namespace, name string) *shipper.RolloutBlock {
	return &shipper.RolloutBlock{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: shipper.RolloutBlockSpec{
			Message: "Simple test rollout block",
			Author: shipper.RolloutBlockAuthor{
				Type: "user",
				Name: "testUser",
			},
		},
	}
}

func buildAssociatedObjects(release *shipper.Release, clusters []*shipper.Cluster) (*shipper.InstallationTarget, *shipper.TrafficTarget, *shipper.CapacityTarget) {
	ownerReferences := []metav1.OwnerReference{
		createOwnerRefFromRelease(release),
	}
	labels := map[string]string{
		shipper.AppLabel:     release.OwnerReferences[0].Name,
		shipper.ReleaseLabel: release.GetName(),
	}

	clusterInstallationTargets := make([]string, 0, len(clusters))
	clusterCapacityTargets := make([]shipper.ClusterCapacityTarget, 0, len(clusters))
	clusterTrafficTargets := make([]shipper.ClusterTrafficTarget, 0, len(clusters))

	for _, cluster := range clusters {
		clusterInstallationTargets = append(
			clusterInstallationTargets,
			cluster.GetName())

		clusterCapacityTargets = append(
			clusterCapacityTargets,
			shipper.ClusterCapacityTarget{
				Name:              cluster.GetName(),
				Percent:           0,
				TotalReplicaCount: 12,
			})

		clusterTrafficTargets = append(
			clusterTrafficTargets,
			shipper.ClusterTrafficTarget{
				Name: cluster.GetName(),
			})
	}

	installationTarget := &shipper.InstallationTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:            release.GetName(),
			Namespace:       release.GetNamespace(),
			OwnerReferences: ownerReferences,
			Labels:          labels,
		},
		Spec: shipper.InstallationTargetSpec{
			Clusters:    clusterInstallationTargets,
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
		ObjectMeta: metav1.ObjectMeta{
			Name:            release.Name,
			Namespace:       release.GetNamespace(),
			OwnerReferences: ownerReferences,
			Labels:          labels,
		},
		Spec: shipper.CapacityTargetSpec{
			Clusters: clusterCapacityTargets,
		},
	}

	return installationTarget, trafficTarget, capacityTarget
}

func buildAssociatedObjectsWithStatus(
	release *shipper.Release,
	clusters []*shipper.Cluster,
	achievedStep *int32,
) (*shipper.InstallationTarget, *shipper.TrafficTarget, *shipper.CapacityTarget) {
	it, tt, ct := buildAssociatedObjects(release, clusters)

	if achievedStep != nil {
		it.Status = shipper.InstallationTargetStatus{
			Conditions: []shipper.TargetCondition{
				TargetConditionOperational,
				TargetConditionReady,
			},
		}

		ct.Status = shipper.CapacityTargetStatus{
			Conditions: []shipper.TargetCondition{
				TargetConditionOperational,
				TargetConditionReady,
			},
		}

		tt.Status = shipper.TrafficTargetStatus{
			Conditions: []shipper.TargetCondition{
				TargetConditionOperational,
				TargetConditionReady,
			},
		}
	}

	return it, tt, ct
}

func buildExpectedActions(release *shipper.Release, clusters []*shipper.Cluster) []kubetesting.Action {
	installationTarget, trafficTarget, capacityTarget := buildAssociatedObjects(release, clusters)

	return []kubetesting.Action{
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("installationtargets"),
			release.GetNamespace(),
			installationTarget),
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("traffictargets"),
			release.GetNamespace(),
			trafficTarget),
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("capacitytargets"),
			release.GetNamespace(),
			capacityTarget,
		),
	}
}
