package capacity

import (
	"fmt"
	"sort"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
	capacityutil "github.com/bookingcom/shipper/pkg/util/capacity"
	targetutil "github.com/bookingcom/shipper/pkg/util/target"
)

const (
	clusterA = "cluster-a"
	clusterB = "cluster-b"
	ctName   = "foobar"
)

type capacityTargetTestExpectation struct {
	capacityTarget    *shipper.CapacityTarget
	status            shipper.CapacityTargetStatus
	replicasByCluster map[string]int32
}

func init() {
	capacityutil.CapacityConditionsShouldDiscardTimestamps = true
	targetutil.ConditionsShouldDiscardTimestamps = true
}

// TestSingleCluster verifies that the capacity controller patches existing
// deployments to match the capacity requirements, and reports achieved
// capacity and readiness.
func TestSingleCluster(t *testing.T) {
	// We define a final total replica count, but expect to have only a
	// fraction of that, because of percent.
	percent := int32(50)
	totalReplicaCount := int32(10)
	expectedReplicaCount := int32(5)
	ct := buildCapacityTarget(shippertesting.TestApp, ctName, []shipper.ClusterCapacityTarget{
		{
			Name:              clusterA,
			Percent:           percent,
			TotalReplicaCount: totalReplicaCount,
		},
	})

	runCapacityControllerTest(t,
		map[string][]runtime.Object{
			clusterA: []runtime.Object{buildDeployment(shippertesting.TestApp, ctName, 0, expectedReplicaCount)},
		},
		[]capacityTargetTestExpectation{
			{
				capacityTarget: ct,
				status:         buildSuccessStatus(ctName, ct.Spec.Clusters),
				replicasByCluster: map[string]int32{
					clusterA: expectedReplicaCount,
				},
			},
		},
	)
}

// TestMultipleClusters does the same thing as TestSingleCluster, but does so
// for multiple clusters.
func TestMultipleClusters(t *testing.T) {
	percent := int32(50)
	totalReplicaCount := int32(10)
	expectedReplicaCount := int32(5)
	ct := buildCapacityTarget(shippertesting.TestApp, ctName, []shipper.ClusterCapacityTarget{
		{
			Name:              clusterA,
			Percent:           percent,
			TotalReplicaCount: totalReplicaCount,
		},
		{
			Name:              clusterB,
			Percent:           percent,
			TotalReplicaCount: totalReplicaCount,
		},
	})

	runCapacityControllerTest(t,
		map[string][]runtime.Object{
			clusterA: []runtime.Object{buildDeployment(shippertesting.TestApp, ctName, 0, expectedReplicaCount)},
			clusterB: []runtime.Object{buildDeployment(shippertesting.TestApp, ctName, 0, expectedReplicaCount)},
		},
		[]capacityTargetTestExpectation{
			{
				capacityTarget: ct,
				status:         buildSuccessStatus(ctName, ct.Spec.Clusters),
				replicasByCluster: map[string]int32{
					clusterA: expectedReplicaCount,
					clusterB: expectedReplicaCount,
				},
			},
		},
	)
}

// TestCapacityShiftingPodsNotSadButNotAvailable verifies that the traffic
// controller can handle cases where deployments are patched correctly, but
// pods have not been created yet, which is different from pods being created,
// but not being ready yet.
func TestCapacityShiftingPodsNotSadButNotAvailable(t *testing.T) {
	totalReplicaCount := int32(10)
	availableReplicaCount := int32(5)
	ct := buildCapacityTarget(shippertesting.TestApp, ctName, []shipper.ClusterCapacityTarget{
		{
			Name:              clusterA,
			Percent:           100,
			TotalReplicaCount: totalReplicaCount,
		},
	})

	status := shipper.CapacityTargetStatus{
		Clusters: []shipper.ClusterCapacityStatus{
			{
				Name:              clusterA,
				AchievedPercent:   50,
				AvailableReplicas: availableReplicaCount,
				Conditions: []shipper.ClusterCapacityCondition{
					ClusterCapacityOperational,
					{
						Type:   shipper.ClusterConditionTypeReady,
						Status: corev1.ConditionFalse,
						Reason: InProgress,
					},
				},
				Reports: []shipper.ClusterCapacityReport{
					{
						Owner: shipper.ClusterCapacityReportOwner{
							Name: ctName,
						},
						Breakdown: []shipper.ClusterCapacityReportBreakdown{},
					},
				},
			},
		},
		Conditions: []shipper.TargetCondition{
			TargetConditionOperational,
			{
				Type:    shipper.TargetConditionTypeReady,
				Status:  corev1.ConditionFalse,
				Reason:  ClustersNotReady,
				Message: fmt.Sprintf("%v", []string{clusterA}),
			},
		},
	}

	runCapacityControllerTest(t,
		map[string][]runtime.Object{
			clusterA: []runtime.Object{buildDeployment(shippertesting.TestApp, ctName, totalReplicaCount, availableReplicaCount)},
		},
		[]capacityTargetTestExpectation{
			{
				capacityTarget: ct,
				status:         status,
				replicasByCluster: map[string]int32{
					clusterA: totalReplicaCount,
				},
			},
		},
	)
}

// TestCapacityShiftingSadPods verifies that the traffic controller can handle
// cases where deployments are patched correctly, but pods are "sad" (aka not
// ready), which is different from pods not being created at all.
func TestCapacityShiftingSadPods(t *testing.T) {
	totalReplicaCount := int32(10)
	availableReplicaCount := int32(5)
	ct := buildCapacityTarget(shippertesting.TestApp, ctName, []shipper.ClusterCapacityTarget{
		{
			Name:              clusterA,
			Percent:           100,
			TotalReplicaCount: totalReplicaCount,
		},
	})

	deployment := buildDeployment(shippertesting.TestApp, ctName, totalReplicaCount, availableReplicaCount)
	sadPod := buildSadPodForDeployment(deployment)
	objects := []runtime.Object{deployment, sadPod}

	reports := []shipper.ClusterCapacityReport{
		{
			Owner: shipper.ClusterCapacityReportOwner{Name: ctName},
			Breakdown: []shipper.ClusterCapacityReportBreakdown{
				{
					Type:       "Ready",
					Status:     string(corev1.ConditionFalse),
					Reason:     "ExpectedFail",
					Count:      1,
					Containers: []shipper.ClusterCapacityReportContainerBreakdown{},
				},
			},
		},
	}
	status := shipper.CapacityTargetStatus{
		Clusters: []shipper.ClusterCapacityStatus{
			{
				Name:              clusterA,
				AchievedPercent:   50,
				AvailableReplicas: availableReplicaCount,
				Conditions: []shipper.ClusterCapacityCondition{
					ClusterCapacityOperational,
					{
						Type:    shipper.ClusterConditionTypeReady,
						Status:  corev1.ConditionFalse,
						Reason:  PodsNotReady,
						Message: "1 out of 10 pods are not Ready. this might require intervention, check SadPods in this object for more information",
					},
				},
				SadPods: []shipper.PodStatus{
					{
						Name:      sadPod.Name,
						Condition: sadPod.Status.Conditions[0],
					},
				},
				Reports: reports,
			},
		},
		Conditions: []shipper.TargetCondition{
			TargetConditionOperational,
			{
				Type:    shipper.TargetConditionTypeReady,
				Status:  corev1.ConditionFalse,
				Reason:  ClustersNotReady,
				Message: fmt.Sprintf("%v", []string{clusterA}),
			},
		},
	}

	runCapacityControllerTest(t,
		map[string][]runtime.Object{
			clusterA: objects,
		},
		[]capacityTargetTestExpectation{
			{
				capacityTarget: ct,
				status:         status,
				replicasByCluster: map[string]int32{
					clusterA: totalReplicaCount,
				},
			},
		},
	)
}

func runCapacityControllerTest(
	t *testing.T,
	objectsByCluster map[string][]runtime.Object,
	expectations []capacityTargetTestExpectation,
) {
	f := shippertesting.NewControllerTestFixture()

	clusterNames := []string{}
	for clusterName, objects := range objectsByCluster {
		cluster := f.AddNamedCluster(clusterName)
		cluster.AddMany(objects)
		clusterNames = append(clusterNames, clusterName)
	}

	sort.Strings(clusterNames)

	for _, expectation := range expectations {
		f.ShipperClient.Tracker().Add(expectation.capacityTarget)
	}

	runController(f)

	ctGVR := shipper.SchemeGroupVersion.WithResource("capacitytargets")
	for _, expectation := range expectations {
		initialCT := expectation.capacityTarget
		ctKey := fmt.Sprintf("%s/%s", initialCT.Namespace, initialCT.Name)
		object, err := f.ShipperClient.Tracker().Get(ctGVR, initialCT.Namespace, initialCT.Name)
		if err != nil {
			t.Errorf("could not Get CapacityTarget %q: %s", ctKey, err)
			continue
		}

		ct := object.(*shipper.CapacityTarget)

		actualStatus := ct.Status
		eq, diff := shippertesting.DeepEqualDiff(expectation.status, actualStatus)
		if !eq {
			t.Errorf(
				"CapacityTarget %q has Status different from expected:\n%s",
				ctKey, diff)
			continue
		}

		for _, clusterName := range clusterNames {
			expectedReplicas := expectation.replicasByCluster[clusterName]
			assertDeploymentReplicas(t, ct, f.Clusters[clusterName], expectedReplicas)
		}
	}
}

func assertDeploymentReplicas(
	t *testing.T,
	ct *shipper.CapacityTarget,
	cluster *shippertesting.FakeCluster,
	expectedReplicas int32,
) {
	deploymentGVR := appsv1.SchemeGroupVersion.WithResource("deployments")
	ctKey := fmt.Sprintf("%s/%s", ct.Namespace, ct.Name)
	object, err := cluster.Client.Tracker().Get(deploymentGVR, ct.Namespace, ct.Name)
	if err != nil {
		t.Errorf(`could not Get Deployment %q: %s`, ctKey, err)
		return
	}

	deployment := object.(*appsv1.Deployment)

	if *deployment.Spec.Replicas != expectedReplicas {
		t.Errorf(
			"CapacityTarget %q expected Deployment in cluster %q to have %d Replicas in its spec, got %d instead",
			ctKey, cluster.Name, expectedReplicas, *deployment.Spec.Replicas,
		)
	}
}

func runController(f *shippertesting.ControllerTestFixture) {
	controller := NewController(
		f.ShipperClient,
		f.ShipperInformerFactory,
		f.ClusterClientStore,
		f.Recorder,
	)

	stopCh := make(chan struct{})
	defer close(stopCh)

	f.Run(stopCh)

	for controller.processNextWorkItem() {
		if controller.workqueue.Len() == 0 {
			time.Sleep(20 * time.Millisecond)
		}
		if controller.workqueue.Len() == 0 {
			return
		}
	}
}
