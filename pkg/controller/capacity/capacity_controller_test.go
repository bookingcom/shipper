package capacity

import (
	"encoding/json"
	"fmt"
	"strconv"
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

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

func TestUpdatingCapacityTargetUpdatesDeployment(t *testing.T) {
	f := NewFixture(t)

	release := newRelease("0.0.1", "reviewsapi", 10)
	capacityTarget := newCapacityTargetForRelease(release, "capacity-v0.0.1", "reviewsapi", 50)
	f.managementObjects = append(f.managementObjects, release, capacityTarget)

	deployment := newDeploymentForRelease(release, "nginx", "reviewsapi", 0)
	f.targetClusterObjects = append(f.targetClusterObjects, deployment)

	f.ExpectDeploymentPatchWithReplicas(deployment, 5)

	f.RunCapacityTargetSyncHandler()
}

func TestUpdatingDeploymentsUpdatesTheCapacityTargetStatus(t *testing.T) {
	f := NewFixture(t)

	release := newRelease("0.0.1", "reviewsapi", 10)
	capacityTarget := newCapacityTargetForRelease(release, "capacity-v0.0.1", "reviewsapi", 50)
	f.managementObjects = append(f.managementObjects, release, capacityTarget)

	deployment := newDeploymentForRelease(release, "nginx", "reviewsapi", 5)
	f.targetClusterObjects = append(f.targetClusterObjects, deployment)

	f.expectCapacityTargetStatusPatch(capacityTarget, 5, 50)

	f.runDeploymentSyncHandler()
}

// TestSadPodsAreReflectedInCapacityTargetStatus tests a case where
// the deployment should have 5 available pods, but it has 4 happy
// pods and 1 sad pod.
func TestSadPodsAreReflectedInCapacityTargetStatus(t *testing.T) {
	f := NewFixture(t)

	release := newRelease("0.0.1", "reviewsapi", 10)
	capacityTarget := newCapacityTargetForRelease(release, "capacity-v0.0.1", "reviewsapi", 50)
	f.managementObjects = append(f.managementObjects, release, capacityTarget)

	deployment := newDeploymentForRelease(release, "nginx", "reviewsapi", 4)
	sadPod := createSadPodForDeployment(deployment)
	f.targetClusterObjects = append(f.targetClusterObjects, deployment, sadPod)

	f.expectCapacityTargetStatusPatch(capacityTarget, 4, 40, createSadPodConditionFromPod(sadPod))

	f.runDeploymentSyncHandler()
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

func (f *fixture) RunCapacityTargetSyncHandler() {
	controller := f.runInternal()
	if err := controller.capacityTargetSyncHandler("reviewsapi/capacity-v0.0.1"); err != nil {
		f.t.Error(err)
	}

	targetClusterActual := shippertesting.FilterActions(f.targetClusterClientset.Actions())

	if len(f.targetClusterActions) == 0 {
		f.t.Error("The list of expected target cluster actions is empty!")
	}

	shippertesting.CheckActions(f.targetClusterActions, targetClusterActual, f.t)
}

func (f *fixture) runDeploymentSyncHandler() {
	controller := f.runInternal()
	if err := controller.deploymentSyncHandler(f.createDeploymentWorkQueueItem()); err != nil {
		f.t.Error(err)
	}

	managementClusterActual := shippertesting.FilterActions(f.managementClientset.Actions())

	if len(f.managementClusterActions) == 0 {
		f.t.Error("The list of expected management cluster actions is empty!")
	}

	shippertesting.CheckActions(f.managementClusterActions, managementClusterActual, f.t)
}

func (f *fixture) createDeploymentWorkQueueItem() deploymentWorkqueueItem {
	return deploymentWorkqueueItem{
		Key:         "reviewsapi/nginx",
		ClusterName: "minikube",
	}
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

func (f *fixture) expectCapacityTargetStatusPatch(capacityTarget *shipperv1.CapacityTarget, availableReplicas, achievedPercent int32, sadPods ...shipperv1.PodStatus) {
	// have to do this for the patches to match, otherwise the
	// value for "sadPods" will be an empty array, but we expect
	// null
	if len(sadPods) == 0 {
		sadPods = nil
	}

	cluster := shipperv1.ClusterCapacityStatus{
		Name:              capacityTarget.Spec.Clusters[0].Name,
		AvailableReplicas: availableReplicas,
		AchievedPercent:   achievedPercent,
		SadPods:           sadPods,
	}

	status := shipperv1.CapacityTargetStatus{
		Clusters: []shipperv1.ClusterCapacityStatus{cluster},
	}

	// Doing this weird assignment here because Kubernetes expects
	// the patch to be a key-value pair, with the key being set to
	// "status"
	patchData := map[string]shipperv1.CapacityTargetStatus{
		"status": status,
	}
	patchBytes, _ := json.Marshal(patchData)

	patchAction := kubetesting.NewPatchSubresourceAction(
		schema.GroupVersionResource{Group: "shipper.booking.com", Version: "v1", Resource: "capacitytargets"},
		capacityTarget.GetNamespace(),
		capacityTarget.GetName(),
		patchBytes,
	)

	f.managementClusterActions = append(f.managementClusterActions, patchAction)
}

func newCapacityTargetForRelease(release *shipperv1.Release, name, namespace string, percent int32) *shipperv1.CapacityTarget {
	minikube := shipperv1.ClusterCapacityTarget{
		Name:    "minikube",
		Percent: percent,
	}

	clusters := []shipperv1.ClusterCapacityTarget{minikube}

	labels := map[string]string{
		"release": release.Name,
	}

	return &shipperv1.CapacityTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
			OwnerReferences: []metav1.OwnerReference{
				metav1.OwnerReference{
					APIVersion: "shipper.booking.com/v1",
					Kind:       "Release",
					Name:       release.GetName(),
					UID:        release.GetUID(),
				},
			},
		},
		Spec: shipperv1.CapacityTargetSpec{
			Clusters: clusters,
		},
	}
}

func newRelease(name, namespace string, replicas int32) *shipperv1.Release {
	releaseMeta := shipperv1.ReleaseMeta{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				shipperv1.ReleaseReplicasAnnotation: strconv.Itoa(int(replicas)),
			},
		},
		Environment: shipperv1.ReleaseEnvironment{},
	}

	return &shipperv1.Release{
		ReleaseMeta: releaseMeta,
	}
}

func newDeploymentForRelease(release *shipperv1.Release, name, namespace string, replicas int32) *appsv1.Deployment {
	status := appsv1.DeploymentStatus{
		AvailableReplicas: replicas,
	}

	labels := map[string]string{
		"release": release.Name,
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
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
			Name:      "nginx-1a93Y2",
			Namespace: "reviewsapi",
			Labels: map[string]string{
				"release": deployment.Labels["release"],
			},
		},
		Status: status,
	}
}

func createSadPodConditionFromPod(sadPod *corev1.Pod) shipperv1.PodStatus {
	return shipperv1.PodStatus{
		Name:      sadPod.Name,
		Condition: sadPod.Status.Conditions[0],
	}
}
