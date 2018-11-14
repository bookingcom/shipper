package capacity

import (
	"fmt"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubeinformers "k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	"github.com/bookingcom/shipper/pkg/conditions"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

func init() {
	conditions.CapacityConditionsShouldDiscardTimestamps = true
}

func TestUpdatingCapacityTargetUpdatesDeployment(t *testing.T) {
	f := NewFixture(t)

	capacityTarget := newCapacityTarget(10, 50)
	f.managementObjects = append(f.managementObjects, capacityTarget)

	deployment := newDeployment(0, 0)
	f.targetClusterObjects = append(f.targetClusterObjects, deployment)

	f.ExpectDeploymentPatchWithReplicas(deployment, 5)

	expectedClusterConditions := []shipper.ClusterCapacityCondition{
		{
			Type:   shipper.ClusterConditionTypeOperational,
			Status: corev1.ConditionTrue,
		},
		{
			Type:   shipper.ClusterConditionTypeReady,
			Status: corev1.ConditionTrue,
		},
	}

	f.expectCapacityTargetStatusUpdate(capacityTarget, 0, 0, expectedClusterConditions)

	f.runCapacityTargetSyncHandler()
}

func TestUpdatingDeploymentsUpdatesTheCapacityTargetStatus(t *testing.T) {
	f := NewFixture(t)

	capacityTarget := newCapacityTarget(10, 50)
	f.managementObjects = append(f.managementObjects, capacityTarget)

	deployment := newDeployment(5, 5)
	f.targetClusterObjects = append(f.targetClusterObjects, deployment)

	clusterConditions := []shipper.ClusterCapacityCondition{
		{
			Type:    shipper.ClusterConditionTypeReady,
			Status:  corev1.ConditionFalse,
			Reason:  conditions.WrongPodCount,
			Message: "expected 5 replicas but have 0",
		},
	}
	f.expectCapacityTargetStatusUpdate(capacityTarget, 5, 50, clusterConditions)

	f.runCapacityTargetSyncHandler()
}

// TestSadPodsAreReflectedInCapacityTargetStatus tests a case where
// the deployment should have 5 available pods, but it has 4 happy
// pods and 1 sad pod.
func TestSadPodsAreReflectedInCapacityTargetStatus(t *testing.T) {
	f := NewFixture(t)

	capacityTarget := newCapacityTarget(2, 100)
	f.managementObjects = append(f.managementObjects, capacityTarget)

	deployment := newDeployment(2, 1)
	happyPod := createHappyPodForDeployment(deployment)
	sadPod := createSadPodForDeployment(deployment)
	f.targetClusterObjects = append(f.targetClusterObjects, deployment, happyPod, sadPod)

	clusterConditions := []shipper.ClusterCapacityCondition{
		{
			Type:    shipper.ClusterConditionTypeReady,
			Status:  corev1.ConditionFalse,
			Reason:  conditions.PodsNotReady,
			Message: "there are 1 sad pods",
		},
	}
	f.expectCapacityTargetStatusUpdate(capacityTarget, 1, 50, clusterConditions, createSadPodConditionFromPod(sadPod))

	f.runCapacityTargetSyncHandler()
}

func NewFixture(t *testing.T) *fixture {
	return &fixture{
		t: t,
	}
}

type fixture struct {
	t *testing.T

	targetClusterClientset       *kubefake.Clientset
	targetClusterInformerFactory kubeinformers.SharedInformerFactory
	targetClusterObjects         []runtime.Object

	managementClientset       *shipperfake.Clientset
	managementInformerFactory shipperinformers.SharedInformerFactory
	managementObjects         []runtime.Object

	store *shippertesting.FakeClusterClientStore

	targetClusterActions     []kubetesting.Action
	managementClusterActions []kubetesting.Action
}

func (f *fixture) initializeFixture() {
	f.targetClusterClientset = kubefake.NewSimpleClientset(f.targetClusterObjects...)
	f.managementClientset = shipperfake.NewSimpleClientset(f.managementObjects...)

	const noResyncPeriod time.Duration = 0
	f.targetClusterInformerFactory = kubeinformers.NewSharedInformerFactory(f.targetClusterClientset, noResyncPeriod)
	f.managementInformerFactory = shipperinformers.NewSharedInformerFactory(f.managementClientset, noResyncPeriod)

	f.store = shippertesting.NewFakeClusterClientStore(f.targetClusterClientset, f.targetClusterInformerFactory, "minikube")
}

func (f *fixture) newController() *Controller {
	controller := NewController(
		f.managementClientset,
		f.managementInformerFactory,
		f.store,
		record.NewFakeRecorder(10),
	)

	return controller
}

func (f *fixture) runInternal() *Controller {
	f.initializeFixture()

	controller := f.newController()

	stopCh := make(chan struct{})
	defer close(stopCh)

	f.store.Run(stopCh)

	f.managementInformerFactory.Start(stopCh)
	f.targetClusterInformerFactory.Start(stopCh)

	f.managementInformerFactory.WaitForCacheSync(stopCh)
	f.targetClusterInformerFactory.WaitForCacheSync(stopCh)

	return controller
}

func (f *fixture) runCapacityTargetSyncHandler() {
	controller := f.runInternal()
	if controller.capacityTargetSyncHandler("reviewsapi/capacity-v0.0.1") {
		f.t.Error("sync handler unexpectedly returned 'retry'")
	}

	targetClusterActual := shippertesting.FilterActions(f.targetClusterClientset.Actions())
	managementClusterActual := shippertesting.FilterActions(f.managementClientset.Actions())

	shippertesting.CheckActions(f.targetClusterActions, targetClusterActual, f.t)
	shippertesting.CheckActions(f.managementClusterActions, managementClusterActual, f.t)
}

func (f *fixture) ExpectDeploymentPatchWithReplicas(deployment *appsv1.Deployment, replicas int32) {
	patchAction := kubetesting.NewPatchSubresourceAction(
		schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
		deployment.GetNamespace(),
		deployment.GetName(),
		[]byte(fmt.Sprintf(`{"spec": {"replicas": %d}}`, replicas)),
	)
	f.targetClusterActions = append(f.targetClusterActions, patchAction)
}

func (f *fixture) expectCapacityTargetStatusUpdate(capacityTarget *shipper.CapacityTarget, availableReplicas, achievedPercent int32, clusterConditions []shipper.ClusterCapacityCondition, sadPods ...shipper.PodStatus) {
	clusterStatus := shipper.ClusterCapacityStatus{
		Name:              capacityTarget.Spec.Clusters[0].Name,
		AvailableReplicas: availableReplicas,
		AchievedPercent:   achievedPercent,
		Conditions:        clusterConditions,
		SadPods:           sadPods,
	}

	capacityTarget.Status.Clusters = append(capacityTarget.Status.Clusters, clusterStatus)

	updateAction := kubetesting.NewUpdateAction(
		schema.GroupVersionResource{Group: "shipper.booking.com", Version: "v1alpha1", Resource: "capacitytargets"},
		capacityTarget.GetNamespace(),
		capacityTarget,
	)

	f.managementClusterActions = append(f.managementClusterActions, updateAction)
}

func newCapacityTarget(totalReplicaCount, percent int32) *shipper.CapacityTarget {
	name := "capacity-v0.0.1"
	namespace := "reviewsapi"
	minikube := shipper.ClusterCapacityTarget{
		Name:              "minikube",
		Percent:           percent,
		TotalReplicaCount: totalReplicaCount,
	}

	clusters := []shipper.ClusterCapacityTarget{minikube}

	metaLabels := map[string]string{
		shipper.ReleaseLabel: "0.0.1",
	}

	return &shipper.CapacityTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    metaLabels,
			OwnerReferences: []metav1.OwnerReference{
				metav1.OwnerReference{
					APIVersion: "shipper.booking.com/v1alpha1",
					Kind:       "Release",
					Name:       "0.0.1",
				},
			},
		},
		Spec: shipper.CapacityTargetSpec{
			Clusters: clusters,
		},
	}
}

func newDeployment(replicas int32, availableReplicas int32) *appsv1.Deployment {
	name := "nginx"
	namespace := "reviewsapi"
	status := appsv1.DeploymentStatus{
		AvailableReplicas: availableReplicas,
	}

	metaLabels := map[string]string{
		shipper.ReleaseLabel: "0.0.1",
	}

	specSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			shipper.ReleaseLabel: "0.0.1",
		},
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    metaLabels,
		},
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: specSelector,
		},
		Status: status,
	}
}

func createSadPodForDeployment(deployment *appsv1.Deployment) *corev1.Pod {
	sadCondition := corev1.PodCondition{
		Type:    corev1.PodReady,
		Status:  corev1.ConditionFalse,
		Reason:  "ExpectedFail",
		Message: "This failure is meant to happen!",
	}

	status := corev1.PodStatus{
		Phase:      corev1.PodFailed,
		Conditions: []corev1.PodCondition{sadCondition},
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nginx-1a93Y2-sad",
			Namespace: "reviewsapi",
			Labels: map[string]string{
				shipper.ReleaseLabel: deployment.Labels[shipper.ReleaseLabel],
			},
		},
		Status: status,
	}
}

func createHappyPodForDeployment(deployment *appsv1.Deployment) *corev1.Pod {
	sadCondition := corev1.PodCondition{
		Type:   corev1.PodReady,
		Status: corev1.ConditionTrue,
	}

	status := corev1.PodStatus{
		Phase:      corev1.PodRunning,
		Conditions: []corev1.PodCondition{sadCondition},
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nginx-1a93Y2-happy",
			Namespace: "reviewsapi",
			Labels: map[string]string{
				shipper.ReleaseLabel: deployment.Labels[shipper.ReleaseLabel],
			},
		},
		Status: status,
	}
}

func createSadPodConditionFromPod(sadPod *corev1.Pod) shipper.PodStatus {
	return shipper.PodStatus{
		Name:      sadPod.Name,
		Condition: sadPod.Status.Conditions[0],
	}
}
