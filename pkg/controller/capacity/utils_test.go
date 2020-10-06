package capacity

import (
	"fmt"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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

	ClusterCapacityOperational = shipper.ClusterCapacityCondition{
		Type:   shipper.ClusterConditionTypeOperational,
		Status: corev1.ConditionTrue,
	}
	ClusterCapacityReady = shipper.ClusterCapacityCondition{
		Type:   shipper.ClusterConditionTypeReady,
		Status: corev1.ConditionTrue,
	}
)

func buildCapacityTarget(app, release string, clusters []shipper.ClusterCapacityTarget) *shipper.CapacityTarget {
	return &shipper.CapacityTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      release,
			Namespace: shippertesting.TestNamespace,
			Labels: map[string]string{
				shipper.AppLabel:     app,
				shipper.ReleaseLabel: release,
			},
		},
		Spec: shipper.CapacityTargetSpec{
			Clusters: clusters,
		},
	}
}

func buildSuccessStatus(name string, clusters []shipper.ClusterCapacityTarget) shipper.CapacityTargetStatus {
	clusterStatuses := make([]shipper.ClusterCapacityStatus, 0, len(clusters))

	for _, cluster := range clusters {
		clusterStatuses = append(clusterStatuses, shipper.ClusterCapacityStatus{
			Name:              cluster.Name,
			AchievedPercent:   cluster.Percent,
			AvailableReplicas: cluster.TotalReplicaCount * cluster.Percent / 100,
			Conditions: []shipper.ClusterCapacityCondition{
				ClusterCapacityOperational,
				ClusterCapacityReady,
			},
			Reports: []shipper.ClusterCapacityReport{
				{
					Owner: shipper.ClusterCapacityReportOwner{
						Name: name,
					},
					Breakdown: []shipper.ClusterCapacityReportBreakdown{},
				},
			},
		})
	}

	sort.Sort(byClusterName(clusterStatuses))

	return shipper.CapacityTargetStatus{
		Clusters: clusterStatuses,
		Conditions: []shipper.TargetCondition{
			TargetConditionOperational,
			TargetConditionReady,
		},
	}
}

func buildDeployment(app, release string, replicas int32, availableReplicas int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      release,
			Namespace: shippertesting.TestNamespace,
			Labels: map[string]string{
				shipper.AppLabel:     app,
				shipper.ReleaseLabel: release,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					shipper.AppLabel:     app,
					shipper.ReleaseLabel: release,
				},
			},
		},
		Status: appsv1.DeploymentStatus{
			AvailableReplicas: availableReplicas,
		},
	}
}

func buildSadPodForDeployment(deployment *appsv1.Deployment) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: deployment.Namespace,
			Name:      fmt.Sprintf("%s-deadbeef", deployment.Name),
			Labels:    deployment.Spec.Selector.MatchLabels,
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodFailed,
			Conditions: []corev1.PodCondition{
				{
					Type:    corev1.PodReady,
					Status:  corev1.ConditionFalse,
					Reason:  "ExpectedFail",
					Message: "This failure is meant to happen!",
				},
			},
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:  "app",
					Ready: false,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason: "ExpectedFail",
						},
					},
				},
			},
		},
	}
}
