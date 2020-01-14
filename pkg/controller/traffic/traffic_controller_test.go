package traffic

import (
	"fmt"
	"sort"
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
	trafficutil "github.com/bookingcom/shipper/pkg/util/traffic"
)

const (
	clusterA = "cluster-a"
	clusterB = "cluster-b"
	ttName   = "foobar"
)

type trafficTargetTestExpectation struct {
	trafficTarget *shipper.TrafficTarget
	status        shipper.TrafficTargetStatus
	podsByCluster map[string]podStatus
}

func init() {
	trafficutil.TrafficConditionsShouldDiscardTimestamps = true
	targetutil.ConditionsShouldDiscardTimestamps = true
}

// TestSingleCluster verifies that the traffic controller labels existing pods
// to match the traffic requirements, and reports achieved traffic and
// readiness.
func TestSingleCluster(t *testing.T) {
	podCount := 1
	tt := buildTrafficTarget(shippertesting.TestApp, ttName,
		map[string]uint32{clusterA: 10})

	runTrafficControllerTest(t,
		map[string][]runtime.Object{
			clusterA: buildWorldWithPods(shippertesting.TestApp, ttName, podCount, noTraffic),
		},
		[]trafficTargetTestExpectation{
			{
				trafficTarget: tt,
				status:        buildSuccessStatus(tt.Spec.Clusters),
				podsByCluster: map[string]podStatus{
					clusterA: {withTraffic: podCount},
				},
			},
		},
	)
}

// TestMultipleClusters does the same thing as TestSingleCluster, but does so
// for multiple clusters.
func TestMultipleClusters(t *testing.T) {
	podCount := 1
	tt := buildTrafficTarget(shippertesting.TestApp, ttName,
		map[string]uint32{clusterA: 10, clusterB: 10})

	runTrafficControllerTest(t,
		map[string][]runtime.Object{
			clusterA: buildWorldWithPods(shippertesting.TestApp, ttName, podCount, noTraffic),
			clusterB: buildWorldWithPods(shippertesting.TestApp, ttName, podCount, noTraffic),
		},
		[]trafficTargetTestExpectation{
			{
				trafficTarget: tt,
				status:        buildSuccessStatus(tt.Spec.Clusters),
				podsByCluster: map[string]podStatus{
					clusterA: {withTraffic: podCount},
					clusterB: {withTraffic: podCount},
				},
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
		shippertesting.TestApp, "foobar-a",
		map[string]uint32{clusterA: 60},
	)
	foobarB := buildTrafficTarget(
		shippertesting.TestApp, "foobar-b",
		map[string]uint32{clusterA: 40},
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
	foobarAStatus := buildSuccessStatus(foobarA.Spec.Clusters)
	foobarAStatus.Clusters[0].AchievedTraffic = 50
	foobarBStatus := buildSuccessStatus(foobarB.Spec.Clusters)
	foobarBStatus.Clusters[0].AchievedTraffic = 40

	runTrafficControllerTest(t,
		map[string][]runtime.Object{clusterA: clusterObjects},
		[]trafficTargetTestExpectation{
			{
				trafficTarget: foobarA,
				status:        foobarAStatus,
				podsByCluster: map[string]podStatus{
					clusterA: podsForFoobarA,
				},
			},
			{
				trafficTarget: foobarB,
				status:        foobarBStatus,
				podsByCluster: map[string]podStatus{
					clusterA: podsForFoobarB,
				},
			},
		},
	)
}

// TestTrafficShiftingWithPodsNotReady verifies that the traffic controller can
// handle cases where label shifting happened correctly, but pods report not
// ready through endpoints.
func TestTrafficShiftingWithPodsNotReady(t *testing.T) {
	tt := buildTrafficTarget(shippertesting.TestApp, ttName,
		map[string]uint32{clusterA: 10})

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
		Clusters: []*shipper.ClusterTrafficStatus{
			{
				Name:            clusterA,
				AchievedTraffic: 7,
				Conditions: []shipper.ClusterTrafficCondition{
					{
						Type:   shipper.ClusterConditionTypeOperational,
						Status: corev1.ConditionTrue,
					},
					{
						Type:   shipper.ClusterConditionTypeReady,
						Status: corev1.ConditionFalse,
						Reason: PodsNotReady,
						Message: fmt.Sprintf(
							"1 out of 3 pods designated to receive traffic are not ready. this might require intervention, try `kubectl describe ct %s` for more information",
							ttName,
						),
					},
				},
			},
		},
		Conditions: []shipper.TargetCondition{
			{
				Type:   shipper.TargetConditionTypeOperational,
				Status: corev1.ConditionTrue,
			},
			{
				Type:    shipper.TargetConditionTypeReady,
				Status:  corev1.ConditionFalse,
				Reason:  ClustersNotReady,
				Message: fmt.Sprintf("%v", []string{clusterA}),
			},
		},
	}

	runTrafficControllerTest(t,
		map[string][]runtime.Object{
			clusterA: objects,
		},
		[]trafficTargetTestExpectation{
			{
				trafficTarget: tt,
				status:        status,
				podsByCluster: map[string]podStatus{
					clusterA: {withTraffic: len(pods)},
				},
			},
		},
	)
}

func runTrafficControllerTest(
	t *testing.T,
	objectsByCluster map[string][]runtime.Object,
	expectations []trafficTargetTestExpectation,
) {
	f := NewControllerTestFixture()

	clusterNames := []string{}
	for clusterName, objects := range objectsByCluster {
		cluster := f.AddNamedCluster(clusterName)
		cluster.AddMany(objects)
		clusterNames = append(clusterNames, clusterName)
	}

	sort.Strings(clusterNames)

	for _, expectation := range expectations {
		f.ShipperClient.Tracker().Add(expectation.trafficTarget)
	}

	runController(f)

	ttGVR := shipper.SchemeGroupVersion.WithResource("traffictargets")
	for _, expectation := range expectations {
		if len(expectation.podsByCluster) != len(objectsByCluster) {
			panic(fmt.Sprintf(
				"programmer error: we have %d clusters in ObjectsByCluster but expectation checks against %d clusters",
				len(objectsByCluster), len(expectation.podsByCluster),
			))
		}

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

		for _, clusterName := range clusterNames {
			expectedPods := expectation.podsByCluster[clusterName]
			assertPodTraffic(t, tt, f.Clusters[clusterName], expectedPods)
		}
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
	list, err := cluster.Client.Tracker().List(podGVR, podGVK, tt.Namespace)
	if err != nil {
		t.Errorf("could not List Pods in namespace %q: %s", tt.Namespace, err)
		return
	}

	podswithTraffic := 0
	podswithoutTraffic := 0

	pods, err := meta.ExtractList(list)
	if err != nil {
		t.Errorf("could not extrat list of Pods: %s", err)
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

func runController(f *ControllerTestFixture) {
	controller := NewController(
		f.ShipperClient,
		f.ShipperInformerFactory,
		f.ClusterClientStore,
		f.Recorder,
	)

	stopCh := make(chan struct{})
	defer close(stopCh)

	f.Run(stopCh)

	for _, cluster := range f.Clusters {
		kubeclient := cluster.Client
		corev1Informers := cluster.InformerFactory.Core().V1()

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
	}

	for controller.processNextWorkItem() {
		if controller.workqueue.Len() == 0 {
			time.Sleep(20 * time.Millisecond)
		}
		if controller.workqueue.Len() == 0 {
			return
		}
	}
}
