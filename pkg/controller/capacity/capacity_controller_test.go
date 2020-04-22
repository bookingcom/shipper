package capacity

import (
	"fmt"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
	targetutil "github.com/bookingcom/shipper/pkg/util/target"
)

const (
	ctName = "foobar"
)

func init() {
	targetutil.ConditionsShouldDiscardTimestamps = true
}

// TestSuccess verifies that the capacity controller patches existing
// deployments to match the capacity requirements, and reports achieved
// capacity and readiness.
func TestSuccess(t *testing.T) {
	// We define a final total replica count, but expect to have only a
	// fraction of that, because of percent.
	percent := int32(50)
	totalReplicaCount := int32(10)
	expectedReplicaCount := int32(5)
	ct := buildCapacityTarget(shippertesting.TestApp, ctName, shipper.CapacityTargetSpec{
		Percent:           percent,
		TotalReplicaCount: totalReplicaCount,
	})

	runCapacityControllerTest(t,
		[]runtime.Object{buildDeployment(shippertesting.TestApp, ctName, 0, expectedReplicaCount)},
		ct,
		buildSuccessStatus(ctName, ct.Spec),
		expectedReplicaCount,
	)
}

// TestCapacityShiftingPodsNotSadButNotAvailable verifies that the traffic
// controller can handle cases where deployments are patched correctly, but
// pods have not been created yet, which is different from pods being created,
// but not being ready yet.
func TestCapacityShiftingPodsNotSadButNotAvailable(t *testing.T) {
	totalReplicaCount := int32(10)
	availableReplicaCount := int32(5)
	ct := buildCapacityTarget(shippertesting.TestApp, ctName, shipper.CapacityTargetSpec{
		Percent:           100,
		TotalReplicaCount: totalReplicaCount,
	})

	status := shipper.CapacityTargetStatus{
		AchievedPercent:   50,
		AvailableReplicas: availableReplicaCount,
		Conditions: []shipper.TargetCondition{
			TargetConditionOperational,
			{
				Type:   shipper.TargetConditionTypeReady,
				Status: corev1.ConditionFalse,
				Reason: "InProgress",
			},
		},
	}

	runCapacityControllerTest(t,
		[]runtime.Object{buildDeployment(shippertesting.TestApp, ctName, totalReplicaCount, availableReplicaCount)},
		ct,
		status,
		totalReplicaCount,
	)
}

// TestCapacityShiftingSadPods verifies that the traffic controller can handle
// cases where deployments are patched correctly, but pods are "sad" (aka not
// ready), which is different from pods not being created at all.
func TestCapacityShiftingSadPods(t *testing.T) {
	totalReplicaCount := int32(10)
	availableReplicaCount := int32(5)
	ct := buildCapacityTarget(shippertesting.TestApp, ctName, shipper.CapacityTargetSpec{
		Percent:           100,
		TotalReplicaCount: totalReplicaCount,
	})

	deployment := buildDeployment(shippertesting.TestApp, ctName, totalReplicaCount, availableReplicaCount)
	sadPod := buildSadPodForDeployment(deployment)
	objects := []runtime.Object{deployment, sadPod}

	status := shipper.CapacityTargetStatus{
		AchievedPercent:   50,
		AvailableReplicas: availableReplicaCount,
		SadPods: []shipper.PodStatus{
			{
				Name:       sadPod.Name,
				Condition:  sadPod.Status.Conditions[0],
				Containers: sadPod.Status.ContainerStatuses,
			},
		},
		Conditions: []shipper.TargetCondition{
			TargetConditionOperational,
			{
				Type:    shipper.TargetConditionTypeReady,
				Status:  corev1.ConditionFalse,
				Reason:  PodsNotReady,
				Message: `1/10: 1x"app" containers with [ExpectedFail]`,
			},
		},
	}

	runCapacityControllerTest(t,
		objects,
		ct,
		status,
		totalReplicaCount,
	)
}

func runCapacityControllerTest(
	t *testing.T,
	objects []runtime.Object,
	ct *shipper.CapacityTarget,
	status shipper.CapacityTargetStatus,
	replicas int32,
) {
	f := shippertesting.NewControllerTestFixture()

	for _, object := range objects {
		f.KubeClient.Tracker().Add(object)
	}

	f.ShipperClient.Tracker().Add(ct)

	runController(f)

	ctGVR := shipper.SchemeGroupVersion.WithResource("capacitytargets")
	ctKey := fmt.Sprintf("%s/%s", ct.Namespace, ct.Name)
	object, err := f.ShipperClient.Tracker().Get(ctGVR, ct.Namespace, ct.Name)
	if err != nil {
		t.Fatalf("could not Get CapacityTarget %q: %s", ctKey, err)
	}

	actualCT := object.(*shipper.CapacityTarget)
	actualStatus := actualCT.Status
	eq, diff := shippertesting.DeepEqualDiff(status, actualStatus)
	if !eq {
		t.Fatalf(
			"CapacityTarget %q has Status different from expected:\n%s",
			ctKey, diff)
	}

	deploymentGVR := appsv1.SchemeGroupVersion.WithResource("deployments")
	object, err = f.FakeCluster.KubeClient.Tracker().Get(deploymentGVR, ct.Namespace, ct.Name)
	if err != nil {
		t.Fatalf(`could not Get Deployment %q: %s`, ctKey, err)
	}

	deployment := object.(*appsv1.Deployment)

	if *deployment.Spec.Replicas != replicas {
		t.Fatalf(
			"CapacityTarget %q expected Deployment to have %d Replicas in its spec, got %d instead",
			ctKey, replicas, *deployment.Spec.Replicas,
		)
	}
}

func runController(f *shippertesting.ControllerTestFixture) {
	controller := NewController(
		f.KubeClient,
		f.KubeInformerFactory,
		f.ShipperClient,
		f.ShipperInformerFactory,
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
