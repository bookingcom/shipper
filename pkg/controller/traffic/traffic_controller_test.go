package traffic

import (
	"fmt"
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
	trafficutil "github.com/bookingcom/shipper/pkg/util/traffic"
)

const (
	trafficLabel = "gets-the-traffic"
	trafficValue = "you-betcha"
)

func init() {
	trafficutil.TrafficConditionsShouldDiscardTimestamps = true
}

func TestSingleCluster(t *testing.T) {
	app := "shipper-test"
	release := "shipper-test-1"
	cluster := "cluster-1"

	tt := buildTrafficTarget(app, release, map[string]uint32{cluster: 10})

	spec := TrafficControllerTestSpec{
		ObjectsByCluster: map[string][]runtime.Object{
			cluster: buildWorldWithPods(app, release, 1, false),
		},
		TrafficTargets: []*shipper.TrafficTarget{tt},
		Expectations: []TrafficTargetTestExpectations{
			TrafficTargetTestExpectations{
				Statuses: buildTotalSuccessStatus(tt),
				PodsByCluster: map[string]PodStatus{
					cluster: PodStatus{WithTraffic: 1},
				},
			},
		},
	}

	spec.Run(t)
}

func TestExtraClustersNoExtraStatuses(t *testing.T) {
	app := "test-app"
	releaseA := "test-app-1234"
	releaseB := "test-app-4567"

	clusterA := "cluster-a"
	clusterB := "cluster-b"

	ttA := buildTrafficTarget(app, releaseA, map[string]uint32{clusterA: 10})
	ttB := buildTrafficTarget(app, releaseB, map[string]uint32{clusterB: 10})

	spec := TrafficControllerTestSpec{
		ObjectsByCluster: map[string][]runtime.Object{
			clusterA: buildWorldWithPods(app, releaseA, 1, true),
			clusterB: buildWorldWithPods(app, releaseB, 1, true),
		},
		TrafficTargets: []*shipper.TrafficTarget{
			ttA, ttB,
		},
		Expectations: []TrafficTargetTestExpectations{
			TrafficTargetTestExpectations{
				Statuses: buildTotalSuccessStatus(ttA),
				PodsByCluster: map[string]PodStatus{
					clusterA: PodStatus{WithTraffic: 1},
					clusterB: PodStatus{},
				},
			},
			TrafficTargetTestExpectations{
				Statuses: buildTotalSuccessStatus(ttB),
				PodsByCluster: map[string]PodStatus{
					clusterA: PodStatus{},
					clusterB: PodStatus{WithTraffic: 1},
				},
			},
		},
	}

	spec.Run(t)
}

type TrafficControllerTestSpec struct {
	ObjectsByCluster map[string][]runtime.Object
	TrafficTargets   []*shipper.TrafficTarget
	Expectations     []TrafficTargetTestExpectations
}

type PodStatus struct {
	WithTraffic    int
	WithoutTraffic int
}

type TrafficTargetTestExpectations struct {
	Statuses      []*shipper.ClusterTrafficStatus
	PodsByCluster map[string]PodStatus
}

func (spec TrafficControllerTestSpec) Run(t *testing.T) {
	if len(spec.TrafficTargets) != len(spec.Expectations) {
		panic(fmt.Sprintf(
			"programmer error: TrafficControllerTestTable contains %d TrafficTargets but %d Expectations to compare against",
			len(spec.TrafficTargets), len(spec.Expectations),
		))
	}

	f := NewControllerTestFixture()
	for clusterName, objects := range spec.ObjectsByCluster {
		cluster := f.AddNamedCluster(clusterName)
		cluster.AddMany(objects)
	}

	for _, tt := range spec.TrafficTargets {
		f.ShipperClient.Tracker().Add(tt)
	}

	spec.RunController(f)

	ttGVR := shipper.SchemeGroupVersion.WithResource("traffictargets")
	for i, expectation := range spec.Expectations {
		if len(expectation.PodsByCluster) != len(spec.ObjectsByCluster) {
			panic(fmt.Sprintf(
				"programmer error: TrafficControllerTestTable contains %d ObjectsByCluster but expectation checks against %d",
				len(spec.ObjectsByCluster), len(expectation.PodsByCluster),
			))
		}

		initialTT := spec.TrafficTargets[i]
		ttKey := fmt.Sprintf("%s/%s", initialTT.Namespace, initialTT.Name)
		object, err := f.ShipperClient.Tracker().Get(ttGVR, initialTT.Namespace, initialTT.Name)
		if err != nil {
			t.Errorf("could not Get TrafficTarget %q: %s", ttKey, err)
			continue
		}

		tt := object.(*shipper.TrafficTarget)

		actualStatus := tt.Status.Clusters
		eq, diff := shippertesting.DeepEqualDiff(expectation.Statuses, actualStatus)
		if !eq {
			t.Errorf(
				"TrafficTarget %q has Status.Clusters different from expected:\n%s",
				ttKey, diff)
			continue
		}

		for clusterName, expectedPods := range expectation.PodsByCluster {
			spec.AssertPodTraffic(t, tt, f.Clusters[clusterName], expectedPods)
		}
	}
}

func (spec TrafficControllerTestSpec) AssertPodTraffic(
	t *testing.T,
	tt *shipper.TrafficTarget,
	cluster *shippertesting.FakeCluster,
	expectedPods PodStatus,
) {
	podGVR := corev1.SchemeGroupVersion.WithResource("pods")
	podGVK := corev1.SchemeGroupVersion.WithKind("Pod")
	list, err := cluster.Client.Tracker().List(podGVR, podGVK, tt.Namespace)
	if err != nil {
		t.Errorf("could not List Pods in namespace %q: %s", tt.Namespace, err)
		return
	}

	podsWithTraffic := 0
	podsWithoutTraffic := 0

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
			podsWithTraffic++
		} else {
			podsWithoutTraffic++
		}
	}

	ttKey := fmt.Sprintf("%s/%s", tt.Namespace, tt.Name)
	if podsWithTraffic != expectedPods.WithTraffic {
		t.Errorf(
			"TrafficTarget %q expects %d pods with traffic in cluster %q, got %d instead",
			ttKey, expectedPods.WithTraffic, cluster.Name, podsWithTraffic)
		return
	}

	if podsWithoutTraffic != expectedPods.WithoutTraffic {
		t.Errorf(
			"TrafficTarget %q expects %d pods without traffic in cluster %q, got %d instead",
			ttKey, expectedPods.WithoutTraffic, cluster.Name, podsWithoutTraffic)
		return
	}
}

func (spec TrafficControllerTestSpec) RunController(f *ControllerTestFixture) {
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

		handlerFn := func(pod *corev1.Pod) {
			endpointsList, err := corev1Informers.Endpoints().Lister().List(labels.Everything())
			if err != nil {
				panic(fmt.Sprintf("can't list endpoints: %s", err))
			}
			if len(endpointsList) != 1 {
				panic(fmt.Sprintf("expected a single endpoint, got %d", len(endpointsList)))
			}

			endpoints := shiftPodInEndpoints(pod, endpointsList[0])
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
			time.Sleep(10 * time.Millisecond)
		}
		if controller.workqueue.Len() == 0 {
			return
		}
	}
}

func buildTrafficTarget(app, release string, clusterWeights map[string]uint32) *shipper.TrafficTarget {
	clusters := make([]shipper.ClusterTrafficTarget, 0, len(clusterWeights))

	for cluster, weight := range clusterWeights {
		clusters = append(clusters, shipper.ClusterTrafficTarget{
			Name:   cluster,
			Weight: weight,
		})
	}

	return &shipper.TrafficTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      release,
			Namespace: shippertesting.TestNamespace,
			Labels: map[string]string{
				shipper.AppLabel:     app,
				shipper.ReleaseLabel: release,
			},
		},
		Spec: shipper.TrafficTargetSpec{
			Clusters: clusters,
		},
	}
}

func buildTotalSuccessStatus(tt *shipper.TrafficTarget) []*shipper.ClusterTrafficStatus {
	clusterStatuses := make([]*shipper.ClusterTrafficStatus, 0, len(tt.Spec.Clusters))

	for _, cluster := range tt.Spec.Clusters {
		clusterStatuses = append(clusterStatuses, &shipper.ClusterTrafficStatus{
			Name:            cluster.Name,
			AchievedTraffic: cluster.Weight,
			Conditions: []shipper.ClusterTrafficCondition{
				shipper.ClusterTrafficCondition{
					Type:   shipper.ClusterConditionTypeOperational,
					Status: corev1.ConditionTrue,
				},
				shipper.ClusterTrafficCondition{
					Type:   shipper.ClusterConditionTypeReady,
					Status: corev1.ConditionTrue,
				},
			},
		})
	}

	return clusterStatuses
}

func buildService(app string) runtime.Object {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-prod", app),
			Namespace: shippertesting.TestNamespace,
			Labels: map[string]string{
				shipper.LBLabel:  shipper.LBForProduction,
				shipper.AppLabel: app,
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				trafficLabel: trafficValue,
			},
		},
	}
}

func buildEndpoints(app string) runtime.Object {
	return &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-prod", app),
			Namespace: shippertesting.TestNamespace,
			Labels: map[string]string{
				shipper.LBLabel:  shipper.LBForProduction,
				shipper.AppLabel: app,
			},
		},
		Subsets: []corev1.EndpointSubset{
			corev1.EndpointSubset{
				Addresses: []corev1.EndpointAddress{},
			},
		},
	}
}

func buildPods(app, release string, count int, withTraffic bool) []runtime.Object {
	pods := make([]runtime.Object, 0, count)
	for i := 0; i < count; i++ {
		getsTraffic := shipper.Enabled
		if !withTraffic {
			getsTraffic = shipper.Disabled
		}
		pods = append(pods, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%d", release, i),
				Namespace: shippertesting.TestNamespace,
				Labels: map[string]string{
					shipper.PodTrafficStatusLabel: getsTraffic,
					shipper.AppLabel:              app,
					shipper.ReleaseLabel:          release,
				},
			},
		})
	}
	return pods
}

func buildWorldWithPods(app, release string, n int, traffic bool) []runtime.Object {
	objects := []runtime.Object{
		buildService(app),
		buildEndpoints(app),
	}

	objects = append(objects, buildPods(app, release, n, traffic)...)

	return objects
}
