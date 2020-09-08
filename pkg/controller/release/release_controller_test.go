package release

import (
	"fmt"
	"github.com/bookingcom/shipper/pkg/metrics/prometheus"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
	"github.com/bookingcom/shipper/pkg/util/conditions"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

const (
	StepStaging int32 = iota
	StepVanguard
	StepFullOn

	GenerationInactive  string = "1"
	GenerationIncumbent string = "2"
	GenerationContender string = "3"
)

var (
	ReleaseConditionUnblocked = shipper.ReleaseCondition{
		Type:   shipper.ReleaseConditionTypeBlocked,
		Status: corev1.ConditionFalse,
	}
	ReleaseConditionStrategyExecuted = shipper.ReleaseCondition{
		Type:   shipper.ReleaseConditionTypeStrategyExecuted,
		Status: corev1.ConditionTrue,
	}
	ReleaseConditionComplete = shipper.ReleaseCondition{
		Type:   shipper.ReleaseConditionTypeComplete,
		Status: corev1.ConditionTrue,
	}

	StateWaitingForCommand = shipper.ReleaseStrategyState{
		WaitingForInstallation: shipper.StrategyStateFalse,
		WaitingForCapacity:     shipper.StrategyStateFalse,
		WaitingForTraffic:      shipper.StrategyStateFalse,
		WaitingForCommand:      shipper.StrategyStateTrue,
	}
	StateWaitingForNone = shipper.ReleaseStrategyState{
		WaitingForInstallation: shipper.StrategyStateFalse,
		WaitingForCapacity:     shipper.StrategyStateFalse,
		WaitingForTraffic:      shipper.StrategyStateFalse,
		WaitingForCommand:      shipper.StrategyStateFalse,
	}

	StrategyConditionContenderAchievedInstallation = shipper.ReleaseStrategyCondition{
		Type:   shipper.StrategyConditionContenderAchievedInstallation,
		Status: corev1.ConditionTrue,
	}
	StrategyConditionContenderAchievedCapacity = shipper.ReleaseStrategyCondition{
		Type:   shipper.StrategyConditionContenderAchievedCapacity,
		Status: corev1.ConditionTrue,
	}
	StrategyConditionContenderAchievedTraffic = shipper.ReleaseStrategyCondition{
		Type:   shipper.StrategyConditionContenderAchievedTraffic,
		Status: corev1.ConditionTrue,
	}
)

func ReleaseConditionClustersChosen(clusters []string) shipper.ReleaseCondition {
	return shipper.ReleaseCondition{
		Type:    shipper.ReleaseConditionTypeClustersChosen,
		Status:  corev1.ConditionTrue,
		Reason:  ClustersChosen,
		Message: strings.Join(clusters, "'"),
	}
}

func init() {
	releaseutil.ConditionsShouldDiscardTimestamps = true
	conditions.StrategyConditionsShouldDiscardTimestamps = true
}

type releaseControllerTestExpectation struct {
	release  *shipper.Release
	status   shipper.ReleaseStatus
	clusters []string
}

// TestRolloutsBlocked tests that a Release will not progress whenever there's
// a relevant RolloutBlock present, and that it will have a Blocked condition
// set to True.
func TestRolloutsBlocked(t *testing.T) {
	rel := buildRelease(
		shippertesting.TestNamespace,
		shippertesting.TestApp,
		"blocked",
		0,
	)
	rb := buildRolloutBlock(shippertesting.TestNamespace, "block-rollouts")

	mgmtClusterObjects := []runtime.Object{rel, rb}
	appClusterObjects := map[string][]runtime.Object{}

	expectedStatus := shipper.ReleaseStatus{
		Conditions: []shipper.ReleaseCondition{
			{
				Type:   shipper.ReleaseConditionTypeBlocked,
				Status: corev1.ConditionTrue,
				Reason: shipper.RolloutBlockReason,
				Message: fmt.Sprintf(
					"rollout block(s) with name(s) %s/%s exist",
					shippertesting.TestNamespace, rb.Name,
				),
			},
		},
	}

	runReleaseControllerTest(t, mgmtClusterObjects, appClusterObjects,
		[]releaseControllerTestExpectation{
			{
				release: rel,
				status:  expectedStatus,
			},
		})
}

// TestUnschedulable tests that a Release will not progress when it can't
// choose clusters based on the its requirements.
func TestUnschedulable(t *testing.T) {
	var replicas int32 = 1
	rel := buildRelease(
		shippertesting.TestNamespace,
		shippertesting.TestApp,
		"unschedulable",
		replicas,
	)

	mgmtClusterObjects := []runtime.Object{rel}
	appClusterObjects := map[string][]runtime.Object{}

	// Our release requires one cluster, but we have none available in
	// mgmtClusterObjects

	expectedStatus := shipper.ReleaseStatus{
		Conditions: []shipper.ReleaseCondition{
			ReleaseConditionUnblocked,
			{
				Type:   shipper.ReleaseConditionTypeClustersChosen,
				Status: corev1.ConditionFalse,
				Reason: "NotEnoughClustersInRegion",
				Message: fmt.Sprintf(
					"Not enough clusters in region %q. Required: %d / Available: 0",
					shippertesting.TestRegion, replicas,
				),
			},
		},
	}

	runReleaseControllerTest(t, mgmtClusterObjects, appClusterObjects,
		[]releaseControllerTestExpectation{
			{
				release: rel,
				status:  expectedStatus,
			},
		})
}

// TestClusterUnreachable tests that a Release will not progress when it
// already has clusters chosen, but they are unreachable.
func TestClusterUnreachable(t *testing.T) {
	rel := buildRelease(
		shippertesting.TestNamespace,
		shippertesting.TestApp,
		"unreachable",
		1,
	)

	// We give the release a cluster that won't exist, making the
	// clusterclientstore consider it unreachable.
	clusterName := "cluster-a"
	rel.Annotations[shipper.ReleaseClustersAnnotation] = clusterName

	mgmtClusterObjects := []runtime.Object{rel}
	appClusterObjects := map[string][]runtime.Object{}

	expectedStatus := shipper.ReleaseStatus{
		Conditions: []shipper.ReleaseCondition{
			ReleaseConditionUnblocked,
			ReleaseConditionClustersChosen([]string{clusterName}),
			{
				Type:   shipper.ReleaseConditionTypeStrategyExecuted,
				Status: corev1.ConditionFalse,
				Reason: StrategyExecutionFailed,
				Message: fmt.Sprintf(
					"no client for cluster %q",
					clusterName,
				),
			},
		},
	}

	runReleaseControllerTest(t, mgmtClusterObjects, appClusterObjects,
		[]releaseControllerTestExpectation{
			{
				release:  rel,
				status:   expectedStatus,
				clusters: []string{clusterName},
			},
		})
}

// TestInvalidStrategy tests that a Release will not progress when it
// can't execute its strategy.
func TestInvalidStrategy(t *testing.T) {
	rel := buildRelease(
		shippertesting.TestNamespace,
		shippertesting.TestApp,
		"invalid-strategy",
		1,
	)

	// we set the Release's TargetStep to something that's very obviously
	// invalid, making its strategy not executable
	rel.Spec.TargetStep = 999

	cluster := buildCluster("cluster-a")
	mgmtClusterObjects := []runtime.Object{rel, cluster}

	// we need to set the cluster as a key here so it gets its own client,
	// otherwise the release won't be executed properly.
	appClusterObjects := map[string][]runtime.Object{
		cluster.Name: []runtime.Object{},
	}

	expectedStatus := shipper.ReleaseStatus{
		Conditions: []shipper.ReleaseCondition{
			ReleaseConditionUnblocked,
			ReleaseConditionClustersChosen([]string{cluster.Name}),
			{
				Type:   shipper.ReleaseConditionTypeStrategyExecuted,
				Status: corev1.ConditionFalse,
				Reason: StrategyExecutionFailed,
				Message: fmt.Sprintf(
					"no step %d in strategy",
					rel.Spec.TargetStep,
				),
			},
		},
	}

	runReleaseControllerTest(t, mgmtClusterObjects, appClusterObjects,
		[]releaseControllerTestExpectation{
			{
				release:  rel,
				status:   expectedStatus,
				clusters: []string{cluster.Name},
			},
		})
}

// TestIntermediateStep tests that a Release will have all of its conditions
// set appropriately for an intermediate achieved step (as in, not a final
// step). This expects most conditions to be true, except for Blocked and
// Complete.
func TestIntermediateStep(t *testing.T) {
	rel := buildRelease(
		shippertesting.TestNamespace,
		shippertesting.TestApp,
		"intermediate-step",
		1,
	)

	targetStep := StepVanguard
	achievedStep := StepVanguard

	// rel has a three-step strategy, so StepVanguard should be an
	// intermediate step
	rel.Spec.TargetStep = targetStep

	cluster := buildCluster("cluster-a")
	it, tt, ct := buildAssociatedObjectsWithStatus(rel, []*shipper.Cluster{cluster}, &achievedStep)

	mgmtClusterObjects := []runtime.Object{rel, cluster}
	appClusterObjects := map[string][]runtime.Object{
		cluster.Name: []runtime.Object{it, ct, tt},
	}

	expectedStatus := shipper.ReleaseStatus{
		AchievedStep: &shipper.AchievedStep{
			Step: achievedStep,
			Name: rel.Spec.Environment.Strategy.Steps[achievedStep].Name,
		},
		Conditions: []shipper.ReleaseCondition{
			ReleaseConditionUnblocked,
			ReleaseConditionClustersChosen([]string{cluster.Name}),
			ReleaseConditionStrategyExecuted,
		},
		Strategy: &shipper.ReleaseStrategyStatus{
			Clusters: []shipper.ClusterStrategyStatus{
				{
					Name: cluster.Name,
					Conditions: stepify(achievedStep, []shipper.ReleaseStrategyCondition{
						StrategyConditionContenderAchievedCapacity,
						StrategyConditionContenderAchievedInstallation,
						StrategyConditionContenderAchievedTraffic,
					}),
				},
			},
			State: StateWaitingForCommand,
		},
	}

	runReleaseControllerTest(t, mgmtClusterObjects, appClusterObjects,
		[]releaseControllerTestExpectation{
			{
				release:  rel,
				status:   expectedStatus,
				clusters: []string{cluster.Name},
			},
		})
}

// TestLastStep tests that a Release will have all of its conditions set
// appropriately for a final achieved step. This expects most conditions to be
// true (except for Blocked) as in TestIntermediateStep, but now also Complete.
func TestLastStep(t *testing.T) {
	rel := buildRelease(
		shippertesting.TestNamespace,
		shippertesting.TestApp,
		"last-step",
		1,
	)

	targetStep := StepFullOn
	achievedStep := StepFullOn

	// rel has a three-step strategy, so StepVanguard should be an
	// intermediate step
	rel.Spec.TargetStep = targetStep

	cluster := buildCluster("cluster-a")
	it, tt, ct := buildAssociatedObjectsWithStatus(rel, []*shipper.Cluster{cluster}, &achievedStep)

	mgmtClusterObjects := []runtime.Object{rel, cluster}
	appClusterObjects := map[string][]runtime.Object{
		cluster.Name: []runtime.Object{it, ct, tt},
	}

	expectedStatus := shipper.ReleaseStatus{
		AchievedStep: &shipper.AchievedStep{
			Step: achievedStep,
			Name: rel.Spec.Environment.Strategy.Steps[achievedStep].Name,
		},
		Conditions: []shipper.ReleaseCondition{
			ReleaseConditionUnblocked,
			ReleaseConditionClustersChosen([]string{cluster.Name}),
			ReleaseConditionComplete,
			ReleaseConditionStrategyExecuted,
		},
		Strategy: &shipper.ReleaseStrategyStatus{
			Clusters: []shipper.ClusterStrategyStatus{
				{
					Name: cluster.Name,
					Conditions: stepify(achievedStep, []shipper.ReleaseStrategyCondition{
						StrategyConditionContenderAchievedCapacity,
						StrategyConditionContenderAchievedInstallation,
						StrategyConditionContenderAchievedTraffic,
					}),
				},
			},
			State: StateWaitingForNone,
		},
	}

	runReleaseControllerTest(t, mgmtClusterObjects, appClusterObjects,
		[]releaseControllerTestExpectation{
			{
				release:  rel,
				status:   expectedStatus,
				clusters: []string{cluster.Name},
			},
		})
}

// TestIncumbentNotOnLastStep tests that a release that has a contender (as in,
// not a head release) won't have its strategy executed if its target step is
// not the final step in the strategy, to prevent historical releases from
// being woken up by user changes.
func TestIncumbentNotOnLastStep(t *testing.T) {
	incumbent := buildRelease(
		shippertesting.TestNamespace,
		shippertesting.TestApp,
		"incumbent-not-on-last-step-incumbent",
		1,
	)

	contender := buildRelease(
		shippertesting.TestNamespace,
		shippertesting.TestApp,
		"incumbent-not-on-last-step-contender",
		1,
	)

	contender.Annotations[shipper.ReleaseGenerationAnnotation] = GenerationContender
	incumbent.Annotations[shipper.ReleaseGenerationAnnotation] = GenerationIncumbent
	incumbent.Spec.TargetStep = StepVanguard

	cluster := buildCluster("cluster-a")
	mgmtClusterObjects := []runtime.Object{incumbent, contender, cluster}

	// we need to set the cluster as a key here so it gets its own client,
	// otherwise the release won't be executed properly.
	appClusterObjects := map[string][]runtime.Object{
		cluster.Name: []runtime.Object{},
	}

	expectedStatus := shipper.ReleaseStatus{
		Conditions: []shipper.ReleaseCondition{
			ReleaseConditionUnblocked,
			ReleaseConditionClustersChosen([]string{cluster.Name}),
			{
				Type:   shipper.ReleaseConditionTypeStrategyExecuted,
				Status: corev1.ConditionFalse,
				Reason: StrategyExecutionFailed,
				Message: fmt.Sprintf(
					"Release %q target step is inconsistent: unexpected value %d (expected: %d)",
					fmt.Sprintf("%s/%s", incumbent.Namespace, incumbent.Name),
					incumbent.Spec.TargetStep, StepFullOn,
				),
			},
		},
	}

	// NOTE(jgreff): although we include the contender in
	// mgmtClusterObjects, we don't check its status, as we actually don't
	// care about it in this particular test case
	runReleaseControllerTest(t, mgmtClusterObjects, appClusterObjects,
		[]releaseControllerTestExpectation{
			{
				release:  incumbent,
				status:   expectedStatus,
				clusters: []string{cluster.Name},
			},
		})
}

func stepify(step int32, conditions []shipper.ReleaseStrategyCondition) []shipper.ReleaseStrategyCondition {
	for i, _ := range conditions {
		conditions[i].Step = step
	}

	return conditions
}

func runReleaseControllerTest(
	t *testing.T,
	mgmtClusterObjects []runtime.Object,
	appClusterObjects map[string][]runtime.Object,
	expectations []releaseControllerTestExpectation,
) {
	f := shippertesting.NewManagementControllerTestFixture(
		mgmtClusterObjects, appClusterObjects)

	runController(f)

	relGVR := shipper.SchemeGroupVersion.WithResource("releases")
	for _, expectation := range expectations {
		initialRel := expectation.release
		relKey := fmt.Sprintf("%s/%s", initialRel.Namespace, initialRel.Name)
		object, err := f.ShipperClient.Tracker().Get(relGVR, initialRel.Namespace, initialRel.Name)
		if err != nil {
			t.Errorf("could not Get Release %q: %s", relKey, err)
			continue
		}

		rel := object.(*shipper.Release)

		expectedSelectedClusters := strings.Join(expectation.clusters, ",")
		selectedClusters, ok := rel.Annotations[shipper.ReleaseClustersAnnotation]
		if !ok && len(expectation.clusters) > 0 {
			t.Errorf("expected release %q to have selected clusters, but got none", relKey)
		}

		if expectedSelectedClusters != selectedClusters {
			t.Errorf("expected release %q to have clusters %q but got %q",
				relKey, expectedSelectedClusters, selectedClusters)
		}

		actualStatus := rel.Status
		eq, diff := shippertesting.DeepEqualDiff(expectation.status, actualStatus)
		if !eq {
			t.Errorf(
				"Release %q has Status different from expected:\n%s",
				relKey, diff)
			continue
		}
	}
}

func runController(f *shippertesting.ControllerTestFixture) {
	controller := NewController(
		f.ShipperClient,
		f.ClusterClientStore,
		f.ShipperInformerFactory,
		shippertesting.LocalFetchChart,
		f.Recorder,
		*prometheus.NewEnqueueMetric(),
	)

	stopCh := make(chan struct{})
	defer close(stopCh)

	f.Run(stopCh)

	for controller.processNextWorkItem() {
		if controller.workqueue.Len() == 0 {
			time.Sleep(200 * time.Millisecond)
		}
		if controller.workqueue.Len() == 0 {
			return
		}
	}
}
