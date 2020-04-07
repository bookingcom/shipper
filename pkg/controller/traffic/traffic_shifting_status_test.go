package traffic

import (
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
	objectutil "github.com/bookingcom/shipper/pkg/util/object"
)

type release struct {
	weight   uint32
	podCount podStatus
}

type podsToShift struct {
	Enabled  int
	Disabled int
}

type trafficShiftingStatusTestExpectation struct {
	Release               release
	Ready                 bool
	AchievedTrafficWeight uint32
	PodsReady             int
	PodsLabeled           int
	PodsToShift           podsToShift
}

func TestTrafficShiftingEmptyRelease(t *testing.T) {
	runBuildTestTrafficShiftingStatus(t, []trafficShiftingStatusTestExpectation{
		{
			Release: release{weight: 0, podCount: podStatus{}},
			Ready:   true,
		},
	})
}

func TestTrafficShiftingNoWeightRelease(t *testing.T) {
	runBuildTestTrafficShiftingStatus(t, []trafficShiftingStatusTestExpectation{
		{
			Release: release{weight: 0, podCount: podStatus{withoutTraffic: 1}},
			Ready:   true,
		},
	})
}

func TestTrafficShiftingNewRelease(t *testing.T) {
	runBuildTestTrafficShiftingStatus(t, []trafficShiftingStatusTestExpectation{
		{
			Release:     release{weight: 1, podCount: podStatus{withoutTraffic: 10}},
			Ready:       false,
			PodsToShift: podsToShift{10, 0},
		},
	})
}

func TestTrafficShiftingCompletedRelease(t *testing.T) {
	runBuildTestTrafficShiftingStatus(t, []trafficShiftingStatusTestExpectation{
		{
			Release:               release{weight: 1, podCount: podStatus{withTraffic: 10}},
			Ready:                 true,
			AchievedTrafficWeight: 1,
			PodsReady:             10,
			PodsLabeled:           10,
		},
	})
}

func TestTrafficShiftingReleaseProgressionUp(t *testing.T) {
	runBuildTestTrafficShiftingStatus(t, []trafficShiftingStatusTestExpectation{
		{
			Release:               release{weight: 1, podCount: podStatus{withTraffic: 5, withoutTraffic: 5}},
			Ready:                 false,
			AchievedTrafficWeight: 1,
			PodsReady:             5,
			PodsLabeled:           5,
			PodsToShift:           podsToShift{5, 0},
		},
	})
}

func TestTrafficShiftingReleaseProgressionDown(t *testing.T) {
	runBuildTestTrafficShiftingStatus(t, []trafficShiftingStatusTestExpectation{
		{
			Release:               release{weight: 0, podCount: podStatus{withTraffic: 5}},
			Ready:                 false,
			AchievedTrafficWeight: 0,
			PodsReady:             5,
			PodsLabeled:           5,
			PodsToShift:           podsToShift{0, 5},
		},
	})
}

func TestTrafficShiftingReleaseProgressionDrainIncumbentReplenishContender(t *testing.T) {
	runBuildTestTrafficShiftingStatus(t, []trafficShiftingStatusTestExpectation{
		{
			Release:               release{weight: 0, podCount: podStatus{withTraffic: 10}},
			Ready:                 false,
			AchievedTrafficWeight: 1,
			PodsReady:             10,
			PodsLabeled:           10,
			PodsToShift:           podsToShift{0, 10},
		},
		{
			Release:               release{weight: 1, podCount: podStatus{withoutTraffic: 10}},
			Ready:                 false,
			AchievedTrafficWeight: 0,
			PodsToShift:           podsToShift{10, 0},
		},
	})
}

func TestTrafficShiftingReleaseSeveralAchievedReleases(t *testing.T) {
	var expectations []trafficShiftingStatusTestExpectation
	for i := 0; i < 5; i++ {
		expectations = append(expectations, trafficShiftingStatusTestExpectation{
			Release:               release{weight: 10, podCount: podStatus{withTraffic: 5}},
			Ready:                 true,
			AchievedTrafficWeight: 10,
			PodsReady:             5,
			PodsLabeled:           5,
		})
	}

	runBuildTestTrafficShiftingStatus(t, expectations)
}

func TestTrafficShiftingReleaseSeveralReleasesDifferentWeights(t *testing.T) {
	runBuildTestTrafficShiftingStatus(t, []trafficShiftingStatusTestExpectation{
		{
			Release:               release{weight: 10, podCount: podStatus{withTraffic: 10}},
			Ready:                 false,
			AchievedTrafficWeight: 15,
			PodsReady:             10,
			PodsLabeled:           10,
			PodsToShift:           podsToShift{0, 3},
		},
		{
			Release:               release{weight: 20, podCount: podStatus{withTraffic: 10}},
			Ready:                 true,
			AchievedTrafficWeight: 15,
			PodsReady:             10,
			PodsLabeled:           10,
		},
	})
}

func TestTrafficShiftingMassiveWeightDisparity(t *testing.T) {
	runBuildTestTrafficShiftingStatus(t, []trafficShiftingStatusTestExpectation{
		{
			Release:               release{weight: 1, podCount: podStatus{withTraffic: 10}},
			Ready:                 false,
			AchievedTrafficWeight: 5001,
			PodsReady:             10,
			PodsLabeled:           10,
			PodsToShift:           podsToShift{0, 9},
		},
		{
			Release:               release{weight: 10000, podCount: podStatus{withTraffic: 10}},
			Ready:                 true,
			AchievedTrafficWeight: 5001,
			PodsReady:             10,
			PodsLabeled:           10,
		},
	})
}

func TestTrafficShiftingUnevedPodsEqualWeights(t *testing.T) {
	runBuildTestTrafficShiftingStatus(t, []trafficShiftingStatusTestExpectation{
		{
			Release:               release{weight: 100, podCount: podStatus{withTraffic: 10}},
			Ready:                 false,
			AchievedTrafficWeight: 182,
			PodsReady:             10,
			PodsLabeled:           10,
			PodsToShift:           podsToShift{0, 4},
		},
		{
			Release:               release{weight: 100, podCount: podStatus{withTraffic: 1}},
			Ready:                 true,
			AchievedTrafficWeight: 18,
			PodsReady:             1,
			PodsLabeled:           1,
		},
	})
}

func TestTrafficShiftingPodsLabeledButNotReady(t *testing.T) {
	releaseName := "foobar"
	releaseWeight := uint32(10)

	appPods := []*corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod-ready",
				Namespace: shippertesting.TestNamespace,
				Labels: map[string]string{
					shipper.AppLabel:              shippertesting.TestApp,
					shipper.ReleaseLabel:          releaseName,
					shipper.PodTrafficStatusLabel: shipper.Enabled,
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod-not-ready",
				Namespace: shippertesting.TestNamespace,
				Labels: map[string]string{
					shipper.AppLabel:              shippertesting.TestApp,
					shipper.ReleaseLabel:          releaseName,
					shipper.PodTrafficStatusLabel: shipper.Enabled,
					podReadinessLabel:             podNotReady,
				},
			},
		},
	}

	endpoints := buildEndpoints(shippertesting.TestApp)
	for _, pod := range appPods {
		endpoints = shiftPodInEndpoints(pod, endpoints)
	}

	trafficStatus := buildTrafficShiftingStatus(
		shippertesting.TestCluster, shippertesting.TestApp, releaseName,
		clusterReleaseWeights{
			shippertesting.TestCluster: map[string]uint32{
				releaseName: releaseWeight,
			},
		},
		endpoints, appPods,
	)

	assertTrafficShiftingStatusExpectation(t, releaseName,
		trafficShiftingStatusTestExpectation{
			Release:               release{weight: releaseWeight},
			Ready:                 false,
			AchievedTrafficWeight: 5,
			PodsLabeled:           2,
			PodsReady:             1,
		}, trafficStatus)
}

func TestTrafficShiftingUnmanagedPods(t *testing.T) {
	releaseName := "foobar"
	releaseWeight := uint32(10)

	appPods := buildPods(shippertesting.TestApp, releaseName, 5, noTraffic)

	appPods = append(appPods, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unmanaged-pod",
			Namespace: shippertesting.TestNamespace,
			Labels: map[string]string{
				shipper.AppLabel:     "bar",
				shipper.ReleaseLabel: "baz",
			},
		},
	})

	endpoints := buildEndpoints(shippertesting.TestApp)
	trafficStatus := buildTrafficShiftingStatus(
		shippertesting.TestCluster, shippertesting.TestApp, releaseName,
		clusterReleaseWeights{
			shippertesting.TestCluster: map[string]uint32{
				releaseName: releaseWeight,
			},
		},
		endpoints, appPods,
	)

	assertTrafficShiftingStatusExpectation(t, releaseName,
		trafficShiftingStatusTestExpectation{
			Release:     release{weight: releaseWeight},
			Ready:       false,
			PodsToShift: podsToShift{5, 0},
		}, trafficStatus)
}

func runBuildTestTrafficShiftingStatus(
	t *testing.T,
	expectations []trafficShiftingStatusTestExpectation,
) {
	var appPods []*corev1.Pod
	endpoints := buildEndpoints(shippertesting.TestApp)
	trafficTargets := make([]*shipper.TrafficTarget, 0, len(expectations))
	for i, expectation := range expectations {
		release := expectation.Release
		releaseName := fmt.Sprintf("release-%d", i)

		trafficTargets = append(trafficTargets, buildTrafficTarget(
			shippertesting.TestApp, releaseName, map[string]uint32{
				shippertesting.TestCluster: release.weight,
			},
		))

		podsWithTraffic := buildPods(
			shippertesting.TestApp,
			releaseName,
			release.podCount.withTraffic,
			withTraffic)

		podsWithoutTraffic := buildPods(
			shippertesting.TestApp,
			releaseName,
			release.podCount.withoutTraffic,
			noTraffic)

		for _, pod := range podsWithTraffic {
			endpoints = shiftPodInEndpoints(pod, endpoints)
		}

		appPods = append(appPods, podsWithTraffic...)
		appPods = append(appPods, podsWithoutTraffic...)
	}

	clusterReleaseWeights, err := buildClusterReleaseWeights(trafficTargets)
	if err != nil {
		t.Fatalf("cannot build cluster release weights: %s", err)
	}

	for i, expectation := range expectations {
		tt := trafficTargets[i]
		relName, err := objectutil.GetReleaseLabel(tt)
		if err != nil {
			t.Fatalf(
				"traffic target %q does not belong to a release: %s",
				objectutil.MetaKey(tt), err.Error())
		}

		trafficStatus := buildTrafficShiftingStatus(
			shippertesting.TestCluster, shippertesting.TestApp, relName,
			clusterReleaseWeights,
			endpoints, appPods,
		)

		assertTrafficShiftingStatusExpectation(t, relName, expectation, trafficStatus)
	}
}

func assertTrafficShiftingStatusExpectation(
	t *testing.T,
	relName string,
	expectation trafficShiftingStatusTestExpectation,
	trafficStatus trafficShiftingStatus,
) {
	actual := trafficShiftingStatusTestExpectation{
		Release:               expectation.Release,
		Ready:                 trafficStatus.ready,
		AchievedTrafficWeight: trafficStatus.achievedTrafficWeight,
		PodsReady:             trafficStatus.podsReady,
		PodsLabeled:           trafficStatus.podsLabeled,
		PodsToShift: podsToShift{
			Enabled:  len(trafficStatus.podsToShift[shipper.Enabled]),
			Disabled: len(trafficStatus.podsToShift[shipper.Disabled]),
		},
	}

	eq, diff := shippertesting.DeepEqualDiff(expectation, actual)
	if !eq {
		t.Errorf(
			"release %q got a different traffic shifting status than expected:\n%s",
			relName, diff)
	}
}
