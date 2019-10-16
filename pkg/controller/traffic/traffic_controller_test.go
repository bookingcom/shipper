package traffic

import (
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	runtimeutil "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubefake "k8s.io/client-go/kubernetes/fake"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	"github.com/bookingcom/shipper/pkg/conditions"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

const (
	trafficLabel = "gets-the-traffic"
	trafficValue = "you-betcha"
)

func init() {
	conditions.TrafficConditionsShouldDiscardTimestamps = true
}

func TestSingleCluster(t *testing.T) {
	f := newFixture(t)
	app := "test-app"
	release := "test-app-1234"
	cluster := f.newCluster()
	cluster.AddOne(buildService(app))

	const noTraffic = false
	pods := buildPods(app, release, 1, noTraffic)
	cluster.AddMany(pods)

	tt := buildTrafficTarget(
		app, release,
		map[string]uint32{
			cluster.Name: 10,
		},
	)

	f.addTrafficTarget(tt)
	updatedTT := tt.DeepCopy()
	updatedTT.Status.Clusters = buildTotalSuccessStatus(updatedTT)

	pod := pods[0].(*corev1.Pod)
	gvr := corev1.SchemeGroupVersion.WithResource("pods")
	patchString := fmt.Sprintf(`[{"op":"replace","path":"/metadata/labels/%s","value":"%s"}]`, shipper.PodTrafficStatusLabel, shipper.Enabled)
	cluster.Expect(kubetesting.NewPatchAction(gvr, shippertesting.TestNamespace, pod.Name, types.JSONPatchType, []byte(patchString)))

	f.expectTrafficTargetUpdate(updatedTT)
	f.run()
}

func TestExtraClustersNoExtraStatuses(t *testing.T) {
	f := newFixture(t)
	app := "test-app"
	releaseA := "test-app-1234"
	releaseB := "test-app-4567"

	clusterA := f.newCluster()
	clusterB := f.newCluster()

	clusterA.AddOne(buildService(app))
	clusterB.AddOne(buildService(app))

	const withTraffic = true
	podsA := buildPods(app, releaseA, 1, withTraffic)
	clusterA.AddMany(podsA)

	podsB := buildPods(app, releaseB, 1, withTraffic)
	clusterB.AddMany(podsB)

	ttA := buildTrafficTarget(
		app, releaseA,
		map[string]uint32{
			clusterA.Name: 10,
		},
	)

	ttB := buildTrafficTarget(
		app, releaseB,
		map[string]uint32{
			clusterB.Name: 10,
		},
	)

	f.addTrafficTarget(ttA)
	f.addTrafficTarget(ttB)

	updatedA := ttA.DeepCopy()
	updatedA.Status.Clusters = buildTotalSuccessStatus(updatedA)

	updatedB := ttB.DeepCopy()
	updatedB.Status.Clusters = buildTotalSuccessStatus(updatedB)

	f.expectTrafficTargetUpdate(updatedA)
	f.expectTrafficTargetUpdate(updatedB)
	f.run()
}

type fixture struct {
	t *testing.T

	trafficTargetCount int

	objects []runtime.Object
	actions []kubetesting.Action

	clusters map[string]*shippertesting.FakeCluster
}

func newFixture(t *testing.T) *fixture {
	return &fixture{
		t: t,

		clusters: make(map[string]*shippertesting.FakeCluster),
	}
}

func (f *fixture) newCluster() *shippertesting.FakeCluster {
	cluster := shippertesting.NewNamedFakeCluster(
		fmt.Sprintf("cluster-%d", len(f.clusters)),
		kubefake.NewSimpleClientset(), nil)

	f.clusters[cluster.Name] = cluster

	return cluster
}

func (f *fixture) newController(
	stopCh chan struct{},
) (
	*shipperfake.Clientset,
	*Controller,
	*shippertesting.FakeClusterClientStore,
	shipperinformers.SharedInformerFactory,
) {
	store := shippertesting.NewFakeClusterClientStore(f.clusters)

	shipperClient := shipperfake.NewSimpleClientset(f.objects...)
	shipperInformerFactory := shipperinformers.NewSharedInformerFactory(shipperClient, shippertesting.NoResyncPeriod)
	c := NewController(
		shipperClient, shipperInformerFactory, store, record.NewFakeRecorder(42),
	)

	return shipperClient, c, store, shipperInformerFactory
}

func (f *fixture) run() {
	stopCh := make(chan struct{})
	defer close(stopCh)

	client, controller, store, informer := f.newController(stopCh)

	runtimeutil.ErrorHandlers = []func(error){
		func(err error) {
			f.t.Errorf("runtime.Error invoked: %q", err)
		},
	}

	informer.Start(stopCh)
	informer.WaitForCacheSync(stopCh)

	store.Run(stopCh)

	wait.PollUntil(
		10*time.Millisecond,
		func() (bool, error) { return controller.workqueue.Len() >= f.trafficTargetCount, nil },
		stopCh,
	)

	for i := 0; i < f.trafficTargetCount; i++ {
		controller.processNextWorkItem()
	}

	actual := shippertesting.FilterActions(client.Actions())
	shippertesting.CheckActions(f.actions, actual, f.t)

	for _, cluster := range f.clusters {
		actual := shippertesting.FilterActions(cluster.Client.Actions())
		shippertesting.CheckActions(cluster.ExpectedActions(), actual, f.t)
	}
}

func (f *fixture) addTrafficTarget(tt *shipper.TrafficTarget) {
	f.trafficTargetCount++
	f.objects = append(f.objects, tt)
}

func (f *fixture) expectTrafficTargetUpdate(tt *shipper.TrafficTarget) {
	gvr := shipper.SchemeGroupVersion.WithResource("traffictargets")
	action := kubetesting.NewUpdateAction(gvr, tt.GetNamespace(), tt)
	f.actions = append(f.actions, action)
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
			Status:          "Synced",
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
