package traffic

import (
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	corev1informer "k8s.io/client-go/informers/core/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	kubetesting "k8s.io/client-go/testing"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

const clusterName = "test-cluster"

type releaseWeights []uint
type releasePodCounts []int
type releaseExpectedTrafficPods []int

func TestIsShifter(t *testing.T) {
	var _ TrafficShifter = &podLabelShifter{}
}

func TestSyncCluster(t *testing.T) {
	// the 'no release' case doesn't work, and also doesn't make sense
	clusterSyncTestCase(t, "one empty release",
		releaseWeights{0},
		releasePodCounts{0},
		releaseExpectedTrafficPods{0},
	)

	clusterSyncTestCase(t, "one no-weight release",
		releaseWeights{0},
		releasePodCounts{1},
		releaseExpectedTrafficPods{0},
	)

	clusterSyncTestCase(t, "one normal release",
		releaseWeights{1},
		releasePodCounts{10},
		releaseExpectedTrafficPods{10},
	)

	clusterSyncTestCase(t, "two empty releases",
		releaseWeights{0, 0},
		releasePodCounts{0, 0},
		releaseExpectedTrafficPods{0, 0},
	)

	clusterSyncTestCase(t, "two no-weight",
		releaseWeights{0, 0},
		releasePodCounts{1, 1},
		releaseExpectedTrafficPods{0, 0},
	)

	clusterSyncTestCase(t, "two equal releases",
		releaseWeights{2, 2},
		releasePodCounts{10, 10},
		// 2/4 * 20 (total pods) = 10 pods
		releaseExpectedTrafficPods{10, 10},
	)

	clusterSyncTestCase(t, "five equal releases",
		releaseWeights{1, 1, 1, 1, 1},
		releasePodCounts{10, 10, 10, 10, 10},
		// 1/5 * 50 (total pods) = 10 pods
		releaseExpectedTrafficPods{10, 10, 10, 10, 10},
	)

	clusterSyncTestCase(t, "UNequal weight, equal pods",
		releaseWeights{1, 2},
		releasePodCounts{10, 10},
		// 1/3 * 20 (total pods) = 6.66 -> round up to 7 pods
		releaseExpectedTrafficPods{7, 10},
	)

	clusterSyncTestCase(t, "massive weight disparity, equal pods",
		releaseWeights{1, 10000},
		releasePodCounts{10, 10},
		// 1/10001 * 20 (total pods) = 0.00198 -> round up to 1 pod
		releaseExpectedTrafficPods{1, 10},
	)

	clusterSyncTestCase(t, "one empty / one present",
		releaseWeights{0, 1},
		releasePodCounts{10, 10},
		// 0/1 * 20 (total pods) = 0 pods
		releaseExpectedTrafficPods{0, 10},
	)
}

func clusterSyncTestCase(
	t *testing.T,
	name string,
	weights releaseWeights,
	podCounts releasePodCounts,
	expectedTrafficCounts releaseExpectedTrafficPods,
) {
	if len(weights) != len(podCounts) || len(weights) != len(expectedTrafficCounts) {
		// programmer error
		panic(
			fmt.Sprintf(
				"len() of weights (%d), podCounts (%d) and expectedTrafficCounts (%d) must be == in every test case",
				len(weights), len(podCounts), len(expectedTrafficCounts),
			),
		)
	}

	releaseNames := make([]string, 0, len(weights))
	for i, _ := range weights {
		releaseNames = append(releaseNames, fmt.Sprintf("release-%d", i))
	}

	f := newFixture(t, name)

	for i, weight := range weights {
		f.addTrafficTarget(releaseNames[i], weight)
	}

	for i, podCount := range podCounts {
		f.addPods(releaseNames[i], podCount)
	}

	f.run()

	for i, expectedPodsWithTraffic := range expectedTrafficCounts {
		f.checkReleasePodsWithTraffic(releaseNames[i], expectedPodsWithTraffic)
	}
}

type fixture struct {
	t              *testing.T
	name           string
	svc            *corev1.Service
	client         *kubefake.Clientset
	actions        []kubetesting.Action
	objects        []runtime.Object
	pods           []*corev1.Pod
	trafficTargets []*shipperv1.TrafficTarget
}

func newFixture(t *testing.T, name string) *fixture {
	f := &fixture{t: t, name: name}
	svc := newService(shippertesting.TestNamespace)
	f.svc = svc
	f.objects = append(f.objects, svc)
	return f
}

func (f *fixture) Errorf(template string, args ...interface{}) {
	argsWithName := make([]interface{}, 0, len(args)+1)
	argsWithName = append(argsWithName, f.name)
	argsWithName = append(argsWithName, args...)
	f.t.Errorf("%s: "+template, argsWithName...)
}

// each TT pertains to exactly one release
func (f *fixture) addTrafficTarget(release string, weight uint) {
	tt := newTrafficTarget(release, map[string]uint{
		clusterName: weight,
	})
	f.trafficTargets = append(f.trafficTargets, tt)
}

func (f *fixture) addPods(releaseName string, count int) {
	for _, pod := range newReleasePods(releaseName, count) {
		f.objects = append(f.objects, pod)
		f.pods = append(f.pods, pod)
	}
}

func (f *fixture) run() {
	clientset := kubefake.NewSimpleClientset(f.objects...)
	f.client = clientset

	const noResyncPeriod time.Duration = 0
	informers := informers.NewSharedInformerFactory(f.client, noResyncPeriod)
	for _, pod := range f.pods {
		informers.Core().V1().Pods().Informer().GetIndexer().Add(pod)
	}

	shifter, err := newPodLabelShifter(
		shippertesting.TestNamespace,
		f.trafficTargets,
		map[string]corev1informer.PodInformer{
			clusterName: informers.Core().V1().Pods(),
		},
	)

	if err != nil {
		f.Errorf("failed to create labelShifter: %s", err.Error())
		return
	}

	errs := shifter.SyncCluster(clusterName, f.client)
	for _, err := range errs {
		f.Errorf("failed to sync cluster: %s", err.Error())
	}
}

func (f *fixture) checkReleasePodsWithTraffic(release string, expectedCount int) {
	trafficSelector := f.svc.Spec.Selector
	trafficCount := 0
	for _, action := range f.client.Actions() {
		switch a := action.(type) {
		case kubetesting.UpdateAction:
			update, _ := a.(kubetesting.UpdateAction)
			obj := update.GetObject()

			switch p := obj.(type) {
			case *corev1.Pod:
				podRelease, ok := p.GetLabels()[shipperv1.ReleaseLabel]
				if !ok || podRelease != release {
					break
				}
				if getsTraffic(p, trafficSelector) {
					trafficCount++
				}
			}
		}
	}
	if trafficCount != expectedCount {
		f.Errorf("expected %d pods with traffic (using selector %v), but got %d", expectedCount, trafficSelector, trafficCount)
	}
}

func newTrafficTarget(release string, clusterWeights map[string]uint) *shipperv1.TrafficTarget {
	tt := &shipperv1.TrafficTarget{
		ObjectMeta: metav1.ObjectMeta{
			// NOTE(btyler) using release name for TTs?
			Name:      release,
			Namespace: shippertesting.TestNamespace,
			Labels: map[string]string{
				shipperv1.ReleaseLabel: release,
			},
		},
		Spec: shipperv1.TrafficTargetSpec{
			Clusters: []shipperv1.ClusterTrafficTarget{},
		},
	}

	for cluster, weight := range clusterWeights {
		tt.Spec.Clusters = append(tt.Spec.Clusters, shipperv1.ClusterTrafficTarget{
			Name:          cluster,
			TargetTraffic: weight,
		})
	}
	return tt
}

func newService(name string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      getAppLBName(name),
			Namespace: shippertesting.TestNamespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"test-traffic": "fake",
			},
		},
	}
}

func newReleasePods(release string, count int) []*corev1.Pod {
	pods := make([]*corev1.Pod, 0, count)
	for i := 0; i < count; i++ {
		pods = append(pods, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%d", release, i),
				Namespace: shippertesting.TestNamespace,
				Labels: map[string]string{
					shipperv1.ReleaseLabel: release,
				},
			},
		})
	}
	return pods
}
