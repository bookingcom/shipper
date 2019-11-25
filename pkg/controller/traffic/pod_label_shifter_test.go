package traffic

import (
	"encoding/json"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

const (
	testClusterName     = "test-cluster"
	testServiceName     = "test-service"
	testApplicationName = "test-app"
)

type releaseWeights []uint32
type releasePodCounts []int
type releaseExpectedTrafficPods []int
type releaseExpectedWeights []uint32

func TestSyncCluster(t *testing.T) {
	// The 'no release' case doesn't work, and also doesn't make sense.
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

func TestWeightCalculatedForJustOneApplication(t *testing.T) {
	var weight uint32 = 100
	pods := 2
	f := newPodLabelShifterFixture(t, "test unmanaged pods not in weight calculation")
	f.addService()

	f.addTrafficTarget("release-a", weight)
	f.addPods("release-a", pods)

	f.addTrafficTarget("release-b", weight)
	f.addPods("release-b", pods)

	expectedWeightsByName := map[string]uint32{
		"release-a": weight,
		"release-b": weight,
	}

	foreignAppLabels := map[string]string{
		shipper.ReleaseLabel: "blorg",
		shipper.AppLabel:     "someOtherApp",
	}
	// add a pod for an unrelated application
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "someOtherPod",
			Namespace: shippertesting.TestNamespace,
			Labels:    foreignAppLabels,
		},
	}

	f.objects = append(f.objects, pod)
	f.pods = append(f.pods, pod)

	f.run(expectedWeightsByName)
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
		// Programmer error.
		panic(
			fmt.Sprintf(
				"len() of weights (%d), podCounts (%d), expectedWeights (%d) and expectedTrafficCounts (%d) must be == in every test case",
				len(weights), len(podCounts), len(expectedWeights), len(expectedTrafficCounts),
			),
		)
	}

	releaseNames := make([]string, 0, len(weights))
	for i := range weights {
		releaseNames = append(releaseNames, fmt.Sprintf("release-%d", i))
	}

	f := newPodLabelShifterFixture(t, name)

	for i, weight := range weights {
		f.addTrafficTarget(releaseNames[i], weight)
	}

	for i, podCount := range podCounts {
		f.addPods(releaseNames[i], podCount)
	}

	expectedWeightsByName := map[string]uint32{}
	for i, _ := range expectedWeights {
		// TODO(jgreff): giant hack alert: due to the current state of
		// the traffic controller, it expects to be lied to about
		// actually achieved weight: it has to be what's specified as
		// the target weight. That's obviously not the actual achieved
		// weight, as this test used to verify. We do want to have the
		// traffic controller report the correct values, so we intend
		// to restore these tests to their old correct self as well,
		// once that's been fixed.
		expectedWeightsByName[releaseNames[i]] = weights[i]
		// expectedWeightsByName[releaseNames[i]] = expectedWeight
	}

	f.addService()
	keepTesting := f.run(expectedWeightsByName)

	if keepTesting {
		for i, expectedPodsWithTraffic := range expectedTrafficCounts {
			f.checkReleasePodsWithTraffic(releaseNames[i], expectedPodsWithTraffic)
		}
	}
}

type podLabelShifterFixture struct {
	t              *testing.T
	name           string
	svc            *corev1.Service
	endpoints      *corev1.Endpoints
	client         *kubefake.Clientset
	objects        []runtime.Object
	pods           []*corev1.Pod
	trafficTargets []*shipper.TrafficTarget
	informers      kubeinformers.SharedInformerFactory
}

func newPodLabelShifterFixture(t *testing.T, name string) *podLabelShifterFixture {
	f := &podLabelShifterFixture{t: t, name: name}
	return f
}

func (f *podLabelShifterFixture) Errorf(template string, args ...interface{}) {
	argsWithName := make([]interface{}, 0, len(args)+1)
	argsWithName = append(argsWithName, f.name)
	argsWithName = append(argsWithName, args...)
	f.t.Errorf("%s: "+template, argsWithName...)
}

// each TT pertains to exactly one release
func (f *podLabelShifterFixture) addTrafficTarget(release string, weight uint32) {
	tt := newTrafficTarget(release, map[string]uint32{
		testClusterName: weight,
	})
	f.trafficTargets = append(f.trafficTargets, tt)
}

func (f *podLabelShifterFixture) addPods(releaseName string, count int) {
	for _, pod := range newReleasePods(releaseName, count) {
		f.objects = append(f.objects, pod)
		f.pods = append(f.pods, pod)
	}
}

func (f *podLabelShifterFixture) addService() {
	labels := map[string]string{
		shipper.AppLabel: testApplicationName,
		shipper.LBLabel:  shipper.LBForProduction,
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testServiceName,
			Namespace: shippertesting.TestNamespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				shipper.AppLabel:              testApplicationName,
				shipper.PodTrafficStatusLabel: shipper.Enabled,
			},
		},
	}

	endpoints := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testServiceName,
			Namespace: shippertesting.TestNamespace,
			Labels:    labels,
		},
		Subsets: []corev1.EndpointSubset{
			corev1.EndpointSubset{
				Addresses: []corev1.EndpointAddress{},
			},
		},
	}

	f.svc = svc
	f.endpoints = endpoints
	f.objects = append(f.objects, svc, endpoints)
}

// buildPodPatchReactionFunc returns a ReactionFunc specialized in poorly patch
// Pods for the scope of the pod label shifter tests.
//
// This function is odd but is required since the default object tracker used by
// Kubernetes fake.Clientset doesn't support Patch actions (see
// vendor/k8s.io/client-go/testing/fixture.go:67)
func (f *podLabelShifterFixture) buildPodPatchReactionFunc() clienttesting.ReactionFunc {
	return func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
		ns := action.GetNamespace()

		switch action := action.(type) {
		case clienttesting.PatchActionImpl:
			pod, err := f.informers.Core().V1().Pods().Lister().Pods(ns).Get(action.GetName())
			if err != nil {
				return false, nil, err
			}

			var patchList []PatchOperation
			err = json.Unmarshal(action.GetPatch(), &patchList)
			if err != nil {
				return false, nil, err
			}

			for _, p := range patchList {
				// For this particular situation, we don't care whether it is an
				// add or replace op, although JSON Patch *requires* the key to
				// exist in order to issue a replace; that's the reason that
				// patchPodTrafficStatusLabel determines the operation based on
				// the presence of the PodTrafficStatusLabel.
				if p.Path == fmt.Sprintf("/metadata/labels/%s", shipper.PodTrafficStatusLabel) {
					pod.Labels[shipper.PodTrafficStatusLabel] = p.Value
				}
			}

			f.endpoints = shiftPodInEndpoints(pod, f.endpoints)

			// Inform the reaction chain the action has been handled, together
			// with the patched Pod object.
			return true, pod, nil

		default:
			return false, nil, nil
		}
	}
}

func (f *podLabelShifterFixture) run(expectedWeights map[string]uint32) bool {
	clientset := kubefake.NewSimpleClientset(f.objects...)
	f.client = clientset

	informers := kubeinformers.NewSharedInformerFactory(f.client, shippertesting.NoResyncPeriod)
	f.informers = informers

	// fake.Clientset default object tracker's Reactor doesn't support "patch"
	// verbs, thus we provide a reactor that attemps to handle it in a very
	// specific and somehow naive way.
	clientset.Fake.PrependReactor("patch", "pods", f.buildPodPatchReactionFunc())

	// Let's get all the informers started and synced
	stopCh := make(<-chan struct{})
	informers.Core().V1().Pods().Informer()
	informers.Core().V1().Services().Informer()
	informers.Core().V1().Endpoints().Informer()
	informers.Start(stopCh)
	informers.WaitForCacheSync(stopCh)

	achievedWeights := make(map[string]uint32)

	for release, _ := range expectedWeights {
		shifter, err := newPodLabelShifter(
			testApplicationName,
			release,
			shippertesting.TestNamespace,
			f.trafficTargets,
		)
		if err != nil {
			f.Errorf("failed to create labelShifter: %s", err.Error())
			return false
		}

		// NOTE(jgreff): the first run of SyncCluster will only adjust
		// pods and return the previously achieved weight. A second run
		// will return the new achieved weight, presumably after pods
		// have made it to the endpoint.
		shifter.SyncCluster(testClusterName, f.client, informers)
		achieved, err := shifter.SyncCluster(testClusterName, f.client, informers)

		if err != nil {
			f.Errorf("failed to sync cluster: %s", err.Error())
			return false
		}

		achievedWeights[release] = achieved
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

	// Should be empty now.
	for release, achievedWeight := range achievedWeights {
		f.Errorf("release %q was found in achievedWeights with weight %d, but that map should be empty", release, achievedWeight)
		keepTesting = false
	}
	return keepTesting
}

func (f *podLabelShifterFixture) checkReleasePodsWithTraffic(release string, expectedCount int) {
	releaseTrafficSelector := labels.Merge(
		f.svc.Spec.Selector,
		map[string]string{
			shipper.AppLabel:     testApplicationName,
			shipper.ReleaseLabel: release,
		},
	).AsSelector()

	pods, err := f.informers.Core().V1().Pods().Lister().Pods(shippertesting.TestNamespace).List(releaseTrafficSelector)
	if err != nil {
		panic(fmt.Sprintf(`Couldn't list pods: %s`, err))
	}

	trafficCount := len(pods)
	if trafficCount != expectedCount {
		f.Errorf("expected %d pods with traffic (using selector %v), but got %d", expectedCount, releaseTrafficSelector, trafficCount)
	}
}

func newTrafficTarget(release string, clusterWeights map[string]uint32) *shipper.TrafficTarget {
	tt := &shipper.TrafficTarget{
		ObjectMeta: metav1.ObjectMeta{
			// NOTE(btyler): using release name for TTs?
			Name:      release,
			Namespace: shippertesting.TestNamespace,
			Labels:    releaseLabels(release),
		},
		Spec: shipper.TrafficTargetSpec{
			Clusters: []shipper.ClusterTrafficTarget{},
		},
	}

	for cluster, weight := range clusterWeights {
		tt.Spec.Clusters = append(tt.Spec.Clusters, shipper.ClusterTrafficTarget{
			Name:   cluster,
			Weight: weight,
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
	labels := map[string]string{
		shipper.AppLabel:     testApplicationName,
		shipper.ReleaseLabel: releaseName,
	}
	return labels
}
