package traffic

import (
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	kubetesting "k8s.io/client-go/testing"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

const (
	testClusterName     = "test-cluster"
	testServiceName     = "test-service"
	testApplicationName = "test-app"
)

type releaseWeights []uint
type releasePodCounts []int
type releaseExpectedTrafficPods []int
type releaseExpectedWeights []int

// this is a private func, but other tests make use of it, so it's better tested in isolation
func TestGetsTraffic(t *testing.T) {
	singleSelector := map[string]string{
		"test-gets-traffic": "firehose",
	}

	doubleSelector := map[string]string{
		"test-gets-traffic": "firehose",
		"test-is-in-lb":     "prod",
	}

	getsTrafficTestCase(t, "good single label", true, singleSelector, singleSelector)
	getsTrafficTestCase(t, "good double label", true, doubleSelector, doubleSelector)

	getsTrafficTestCase(t, "no label", false, singleSelector, map[string]string{})
	getsTrafficTestCase(t, "partial label", false, doubleSelector, map[string]string{
		"test-gets-traffic": "firehose",
	})
	getsTrafficTestCase(t, "correct single label, wrong values", false, singleSelector, map[string]string{
		"test-gets-traffic": "dripfeed",
	})
	getsTrafficTestCase(t, "correct double label, one wrong value", false, doubleSelector, map[string]string{
		"test-gets-traffic": "firehose",
		"test-is-in-lb":     "staging",
	})
	getsTrafficTestCase(t, "correct double label, two wrong values", false, doubleSelector, map[string]string{
		"test-gets-traffic": "dripfeed",
		"test-is-in-lb":     "staging",
	})
}

func getsTrafficTestCase(t *testing.T, name string, shouldGetTraffic bool, selector, labels map[string]string) {
	result := getsTraffic(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: labels,
		},
	}, selector)
	if result != shouldGetTraffic {
		t.Errorf("%s: getTraffic returned %v but expected %v", name, result, shouldGetTraffic)
	}
}

func TestSyncCluster(t *testing.T) {
	// the 'no release' case doesn't work, and also doesn't make sense
	clusterSyncTestCase(t, "one empty release",
		releaseWeights{0},
		releasePodCounts{0},
		releaseExpectedTrafficPods{0},
		releaseExpectedWeights{0},
	)

	clusterSyncTestCase(t, "one no-weight release",
		releaseWeights{0},
		releasePodCounts{1},
		releaseExpectedTrafficPods{0},
		releaseExpectedWeights{0},
	)

	clusterSyncTestCase(t, "one normal release",
		releaseWeights{1},
		releasePodCounts{10},
		releaseExpectedTrafficPods{10},
		releaseExpectedWeights{1},
	)

	clusterSyncTestCase(t, "two empty releases",
		releaseWeights{0, 0},
		releasePodCounts{0, 0},
		releaseExpectedTrafficPods{0, 0},
		releaseExpectedWeights{0, 0},
	)

	clusterSyncTestCase(t, "two no-weight",
		releaseWeights{0, 0},
		releasePodCounts{1, 1},
		releaseExpectedTrafficPods{0, 0},
		releaseExpectedWeights{0, 0},
	)

	clusterSyncTestCase(t, "two equal releases",
		releaseWeights{2, 2},
		releasePodCounts{10, 10},
		// 2/4 * 20 (total pods) = 10 pods
		releaseExpectedTrafficPods{10, 10},
		releaseExpectedWeights{2, 2},
	)

	clusterSyncTestCase(t, "five equal releases",
		releaseWeights{1, 1, 1, 1, 1},
		releasePodCounts{10, 10, 10, 10, 10},
		// 1/5 * 50 (total pods) = 10 pods
		releaseExpectedTrafficPods{10, 10, 10, 10, 10},
		releaseExpectedWeights{1, 1, 1, 1, 1},
	)

	clusterSyncTestCase(t, "UNequal weight, equal pods",
		releaseWeights{1, 2},
		releasePodCounts{10, 10},
		// 1/3 * 20 (total pods) = 6.66 -> round up to 7 pods
		releaseExpectedTrafficPods{7, 10},
		releaseExpectedWeights{1, 2},
	)

	clusterSyncTestCase(t, "massive weight disparity, equal pods",
		releaseWeights{1, 10000},
		releasePodCounts{10, 10},
		// 1/10001 * 20 (total pods) = 0.00198 -> round up to 1 pod
		releaseExpectedTrafficPods{1, 10},
		// 0.05 (1 pod / 20 total pods) * 10001 (total weight) = 500.05 rounds to 500 achieved weight for release 0
		// 0.5 (10 pods / 20 total pods) * 10001 (total weight) = 5000.5 rounds to 5001 achieved weight for release 1
		releaseExpectedWeights{500, 5001},
	)

	clusterSyncTestCase(t, "no rounding, cap on larger weight (too few pods)",
		releaseWeights{3, 7},
		releasePodCounts{50, 50},
		releaseExpectedTrafficPods{30, 50},
		// 0.3 (30 pods / 100 total pods) * 10 (total weight) = 3 achieved weight for release 0
		// 0.5 (50 pods / 100 total pods) * 10 (total weight) = 5 achieved weight for release 1
		releaseExpectedWeights{3, 5},
	)

	clusterSyncTestCase(t, "uneven pod counts, equal weights",
		releaseWeights{100, 100},
		releasePodCounts{10, 1},
		// 1/2 * 11 (total pods) = 5.5 -> round up to 6 pods
		// 1/2 * 1 = 0.5 -> round up to 1 pod
		releaseExpectedTrafficPods{6, 1},
		// 0.54 (6 pods / 11 total pods) * 200 (total weight) = 109.09 rounds to 109 achieved weight for release 0
		// 0.09 (1 pod / 11 total pods) * 200 (total weight) = 18 achieved weight for release 1
		releaseExpectedWeights{109, 18},
	)

	clusterSyncTestCase(t, "one empty / one present",
		releaseWeights{0, 1},
		releasePodCounts{10, 10},
		// 0/1 * 20 (total pods) = 0 pods
		releaseExpectedTrafficPods{0, 10},
		releaseExpectedWeights{0, 1},
	)
}

func clusterSyncTestCase(
	t *testing.T,
	name string,
	weights releaseWeights,
	podCounts releasePodCounts,
	expectedTrafficCounts releaseExpectedTrafficPods,
	expectedWeights releaseExpectedWeights,
) {
	if len(weights) != len(podCounts) || len(weights) != len(expectedTrafficCounts) || len(weights) != len(expectedWeights) {
		// programmer error
		panic(
			fmt.Sprintf(
				"len() of weights (%d), podCounts (%d), expectedWeights (%d) and expectedTrafficCounts (%d) must be == in every test case",
				len(weights), len(podCounts), len(expectedWeights), len(expectedTrafficCounts),
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

	expectedWeightsByName := map[string]int{}
	for i, expectedWeight := range expectedWeights {
		expectedWeightsByName[releaseNames[i]] = expectedWeight
	}

	f.contenderRelease = releaseNames[len(releaseNames)-1]
	f.addService()
	keepTesting := f.run(expectedWeightsByName)

	if keepTesting {
		for i, expectedPodsWithTraffic := range expectedTrafficCounts {
			f.checkReleasePodsWithTraffic(releaseNames[i], expectedPodsWithTraffic)
		}
	}
}

type fixture struct {
	t                *testing.T
	name             string
	contenderRelease string
	svc              *corev1.Service
	client           *kubefake.Clientset
	objects          []runtime.Object
	pods             []*corev1.Pod
	trafficTargets   []*shipperv1.TrafficTarget
}

func newFixture(t *testing.T, name string) *fixture {
	f := &fixture{t: t, name: name}
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
		testClusterName: weight,
	})
	f.trafficTargets = append(f.trafficTargets, tt)
}

func (f *fixture) addPods(releaseName string, count int) {
	for _, pod := range newReleasePods(releaseName, count) {
		f.objects = append(f.objects, pod)
		f.pods = append(f.pods, pod)
	}
}

func (f *fixture) addService() {
	labels := appLabels()
	labels[shipperv1.LBLabel] = shipperv1.LBForProduction
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testServiceName,
			Namespace: shippertesting.TestNamespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"test-traffic": "fake",
			},
		},
	}

	f.svc = svc
	f.objects = append(f.objects, svc)
}

func (f *fixture) run(expectedWeights map[string]int) bool {
	clientset := kubefake.NewSimpleClientset(f.objects...)
	f.client = clientset

	const noResyncPeriod time.Duration = 0
	informers := informers.NewSharedInformerFactory(f.client, noResyncPeriod)
	for _, pod := range f.pods {
		informers.Core().V1().Pods().Informer().GetIndexer().Add(pod)
	}

	shifter, err := newPodLabelShifter(
		shippertesting.TestNamespace,
		releaseLabels(f.contenderRelease),
		f.trafficTargets,
	)

	if err != nil {
		f.Errorf("failed to create labelShifter: %s", err.Error())
		return false
	}

	achievedWeights, errs, _ :=
		shifter.SyncCluster(testClusterName, f.client, informers.Core().V1().Pods())

	for _, err := range errs {
		f.Errorf("failed to sync cluster: %s", err.Error())
	}

	// no point inspecting anything else
	if len(errs) > 0 {
		return false
	}

	keepTesting := true
	for release, expectedWeight := range expectedWeights {
		achievedWeight, ok := achievedWeights[release]
		if !ok {
			f.Errorf("expected to find release %q in achievedWeights, but it wasn't there", release)
			keepTesting = false
			continue
		}
		if expectedWeight != achievedWeight {
			f.Errorf("release %q expected weight %d but got %d", release, expectedWeight, achievedWeight)
			keepTesting = false
		}
		delete(achievedWeights, release)
	}

	// should be empty now
	for release, achievedWeight := range achievedWeights {
		f.Errorf("release %q was found in achievedWeights with weight %d, but that map should be empty", release, achievedWeight)
		keepTesting = false
	}
	return keepTesting
}

func (f *fixture) checkReleasePodsWithTraffic(release string, expectedCount int) {
	trafficSelector := f.svc.Spec.Selector
	trafficCount := 0
	for _, action := range f.client.Actions() {
		switch a := action.(type) {
		case kubetesting.UpdateAction:
			update, _ := a.(kubetesting.UpdateAction)
			obj := update.GetObject()

			//NOTE(btyler) I feel like there must be a better way to take
			// a runtime.Object and decide whether it is a pod
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
			Labels:    releaseLabels(release),
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

func newReleasePods(release string, count int) []*corev1.Pod {
	pods := make([]*corev1.Pod, 0, count)
	for i := 0; i < count; i++ {
		pods = append(pods, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%d", release, i),
				Namespace: shippertesting.TestNamespace,
				Labels:    releaseLabels(release),
			},
		})
	}
	return pods
}

func releaseLabels(releaseName string) map[string]string {
	labels := appLabels()
	labels[shipperv1.ReleaseLabel] = releaseName
	return labels
}

func appLabels() map[string]string {
	return map[string]string{
		"coolapp": testApplicationName,
	}
}
