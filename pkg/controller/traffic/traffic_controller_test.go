package traffic

import (
	"fmt"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
	targetutil "github.com/bookingcom/shipper/pkg/util/target"
)

const (
	ttName = "foobar"
)

type trafficTargetTestExpectation struct {
	trafficTarget *shipper.TrafficTarget
	status        shipper.TrafficTargetStatus
	pods          podStatus
}

func init() {
	targetutil.ConditionsShouldDiscardTimestamps = true
}

// TestSuccess verifies that the traffic controller labels existing pods to
// match the traffic requirements, and reports achieved traffic and readiness.
func TestSuccess(t *testing.T) {
	podCount := 1
	tt := buildTrafficTarget(shippertesting.TestApp, ttName, 10)

	runTrafficControllerTest(t,
		buildWorldWithPods(shippertesting.TestApp, ttName, podCount, noTraffic),
		[]trafficTargetTestExpectation{
			{
				trafficTarget: tt,
				status:        buildSuccessStatus(tt.Spec),
				pods:          podStatus{withTraffic: podCount},
			},
		},
	)
}

// TestMultipleTrafficTargets verifies that the traffic controller can handle
// multiple traffic targets of the same release, since traffic shifting is
// based on weight, and the number of pods labeled for traffic in each release
// depends on the total number of pods, and the weight of the other traffic
// targets for the same app.
func TestMultipleTrafficTargets(t *testing.T) {
	// We setup traffic to be 60/40...
	foobarA := buildTrafficTarget(
		shippertesting.TestApp, "foobar-a", 60,
	)
	foobarB := buildTrafficTarget(
		shippertesting.TestApp, "foobar-b", 40,
	)

	clusterObjects := []runtime.Object{
		buildService(shippertesting.TestApp),
		buildEndpoints(shippertesting.TestApp),
	}

	// ... giving the same capacity to both releases in the cluster ...
	podCount := 5
	clusterObjects = addPodsToList(clusterObjects,
		buildPods(shippertesting.TestApp,
			foobarA.Name, podCount, noTraffic))

	clusterObjects = addPodsToList(clusterObjects,
		buildPods(shippertesting.TestApp,
			foobarB.Name, podCount, noTraffic))

	// ... and we expect foobar-a to have 5 pods with traffic (even though
	// the spec says it should have 6: there's just not enough capacity),
	// while foobar-b has 4.
	podsForFoobarA := podStatus{withTraffic: 5}
	podsForFoobarB := podStatus{withTraffic: 4, withoutTraffic: 1}

	// Since it's impossible to actually achieve 60/40 in this scenario,
	// the status needs to reflect the actual achieved weight. It should
	// still be Ready, though, as we've applied the optimal weights under
	// the circumstances.
	foobarAStatus := buildSuccessStatus(foobarA.Spec)
	foobarAStatus.AchievedTraffic = 50
	foobarBStatus := buildSuccessStatus(foobarB.Spec)
	foobarBStatus.AchievedTraffic = 40

	runTrafficControllerTest(t,
		clusterObjects,
		[]trafficTargetTestExpectation{
			{
				trafficTarget: foobarA,
				status:        foobarAStatus,
				pods:          podsForFoobarA,
			},
			{
				trafficTarget: foobarB,
				status:        foobarBStatus,
				pods:          podsForFoobarB,
			},
		},
	)
}

// TestTrafficShiftingWithPodsNotReady verifies that the traffic controller can
// handle cases where label shifting happened correctly, but pods report not
// ready through endpoints.
func TestTrafficShiftingWithPodsNotReady(t *testing.T) {
	tt := buildTrafficTarget(shippertesting.TestApp, ttName, 10)

	pods := []runtime.Object{
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod-ready",
				Namespace: shippertesting.TestNamespace,
				Labels: map[string]string{
					shipper.AppLabel:     shippertesting.TestApp,
					shipper.ReleaseLabel: ttName,
				},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod-also-ready",
				Namespace: shippertesting.TestNamespace,
				Labels: map[string]string{
					shipper.AppLabel:     shippertesting.TestApp,
					shipper.ReleaseLabel: ttName,
				},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod-not-ready",
				Namespace: shippertesting.TestNamespace,
				Labels: map[string]string{
					shipper.AppLabel:     shippertesting.TestApp,
					shipper.ReleaseLabel: ttName,
					podReadinessLabel:    podNotReady,
				},
			},
		},
	}

	objects := []runtime.Object{
		buildService(shippertesting.TestApp),
		buildEndpoints(shippertesting.TestApp),
	}
	objects = append(objects, pods...)

	status := shipper.TrafficTargetStatus{
		AchievedTraffic: 7,
		Conditions: []shipper.TargetCondition{
			{
				Type:   shipper.TargetConditionTypeOperational,
				Status: corev1.ConditionTrue,
			},
			{
				Type:    shipper.TargetConditionTypeReady,
				Status:  corev1.ConditionFalse,
				Reason:  PodsNotReady,
				Message: "1/3 pods designated to receive traffic are not ready",
			},
		},
	}

	runTrafficControllerTest(t,
		objects,
		[]trafficTargetTestExpectation{
			{
				trafficTarget: tt,
				status:        status,
				pods:          podStatus{withTraffic: len(pods)},
			},
		},
	)
}

func runTrafficControllerTest(
	t *testing.T,
	objects []runtime.Object,
	expectations []trafficTargetTestExpectation,
) {
	f := shippertesting.NewControllerTestFixture()

	for _, object := range objects {
		f.KubeClient.Tracker().Add(object)
	}

	for _, expectation := range expectations {
		f.ShipperClient.Tracker().Add(expectation.trafficTarget)
	}

	runController(f)

	ttGVR := shipper.SchemeGroupVersion.WithResource("traffictargets")
	for _, expectation := range expectations {
		initialTT := expectation.trafficTarget
		ttKey := fmt.Sprintf("%s/%s", initialTT.Namespace, initialTT.Name)
		object, err := f.ShipperClient.Tracker().Get(ttGVR, initialTT.Namespace, initialTT.Name)
		if err != nil {
			t.Errorf("could not Get TrafficTarget %q: %s", ttKey, err)
			continue
		}

		tt := object.(*shipper.TrafficTarget)

		actualStatus := tt.Status
		eq, diff := shippertesting.DeepEqualDiff(expectation.status, actualStatus)
		if !eq {
			t.Errorf(
				"TrafficTarget %q has Status different from expected:\n%s",
				ttKey, diff)
			continue
		}

		assertPodTraffic(t, tt, f.FakeCluster, expectation.pods)
	}
}

func assertPodTraffic(
	t *testing.T,
	tt *shipper.TrafficTarget,
	cluster *shippertesting.FakeCluster,
	expectedPods podStatus,
) {
	podGVR := corev1.SchemeGroupVersion.WithResource("pods")
	podGVK := corev1.SchemeGroupVersion.WithKind("Pod")
	list, err := cluster.KubeClient.Tracker().List(podGVR, podGVK, tt.Namespace)
	if err != nil {
		t.Errorf("could not List Pods in namespace %q: %s", tt.Namespace, err)
		return
	}

	podswithTraffic := 0
	podswithoutTraffic := 0

	pods, err := meta.ExtractList(list)
	if err != nil {
		t.Errorf("could not extract list of Pods: %s", err)
		return
	}

	ttSelector := labels.Set(map[string]string{
		shipper.AppLabel:     tt.Labels[shipper.AppLabel],
		shipper.ReleaseLabel: tt.Labels[shipper.ReleaseLabel],
	}).AsSelector()
	trafficSelector := labels.Set(map[string]string{
		shipper.PodTrafficStatusLabel: shipper.Enabled,
	}).AsSelector()
	for _, podObj := range pods {
		pod := podObj.(*corev1.Pod)
		podLabels := labels.Set(pod.Labels)

		if !ttSelector.Matches(podLabels) {
			continue
		}

		if trafficSelector.Matches(podLabels) {
			podswithTraffic++
		} else {
			podswithoutTraffic++
		}
	}

	ttKey := fmt.Sprintf("%s/%s", tt.Namespace, tt.Name)
	if podswithTraffic != expectedPods.withTraffic {
		t.Errorf(
			"TrafficTarget %q expects %d pods with traffic labels in cluster %q, got %d instead",
			ttKey, expectedPods.withTraffic, cluster.Name, podswithTraffic)
		return
	}

	if podswithoutTraffic != expectedPods.withoutTraffic {
		t.Errorf(
			"TrafficTarget %q expects %d pods without traffic labels in cluster %q, got %d instead",
			ttKey, expectedPods.withoutTraffic, cluster.Name, podswithoutTraffic)
		return
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

	kubeclient := f.KubeClient
	corev1Informers := f.KubeInformerFactory.Core().V1()

	endpointsList, err := corev1Informers.Endpoints().Lister().List(labels.Everything())
	if err != nil {
		panic(fmt.Sprintf("can't list endpoints: %s", err))
	}
	if len(endpointsList) != 1 {
		panic(fmt.Sprintf("expected a single endpoint, got %d", len(endpointsList)))
	}

	var mutex sync.Mutex
	endpoints := endpointsList[0]
	handlerFn := func(pod *corev1.Pod) {
		mutex.Lock()
		defer mutex.Unlock()

		endpoints = shiftPodInEndpoints(pod, endpoints)
		_, err = kubeclient.CoreV1().Endpoints(endpoints.Namespace).Update(endpoints)
		if err != nil {
			panic(fmt.Sprintf("can't update endpoints: %s", err))
		}
	}

	corev1Informers.Pods().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				handlerFn(obj.(*corev1.Pod))
			},
			UpdateFunc: func(old, new interface{}) {
				handlerFn(new.(*corev1.Pod))
			},
		},
	)

	for controller.processNextWorkItem() {
		if controller.workqueue.Len() == 0 {
			time.Sleep(20 * time.Millisecond)
		}
		if controller.workqueue.Len() == 0 {
			return
		}
	}
}
