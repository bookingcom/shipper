package strategy

import (
	"fmt"
	"testing"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	"github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	"github.com/bookingcom/shipper/pkg/conditions"
)

const (
	clusterName   = "minikube"
	namespace     = "test-namespace"
	incumbentName = "0.0.1"
	contenderName = "0.0.2"
)

func init() {
	conditions.StrategyConditionsShouldDiscardTimestamps = true
}

var app = &v1.Application{
	ObjectMeta: metaV1.ObjectMeta{
		Name:      "test-app",
		Namespace: namespace,
		UID:       "foobarbaz",
	},
	Status: v1.ApplicationStatus{
		History: []string{incumbentName, contenderName},
	},
}

var vanguard = v1.RolloutStrategy{
	Steps: []v1.RolloutStrategyStep{
		{
			Name:     "staging",
			Capacity: v1.RolloutStrategyStepValue{Incumbent: 100, Contender: 1},
			Traffic:  v1.RolloutStrategyStepValue{Incumbent: 100, Contender: 0},
		},
		{
			Name:     "50/50",
			Capacity: v1.RolloutStrategyStepValue{Incumbent: 50, Contender: 50},
			Traffic:  v1.RolloutStrategyStepValue{Incumbent: 50, Contender: 50},
		},
		{
			Name:     "full on",
			Capacity: v1.RolloutStrategyStepValue{Incumbent: 0, Contender: 100},
			Traffic:  v1.RolloutStrategyStepValue{Incumbent: 0, Contender: 100},
		},
	},
}

// TestCompleteStrategy tests the complete "vanguard" strategy, end-to-end.
// This test exercises only the Executor.execute() method, using hard coded
// incumbent and contender releases, checking if the generated patches were
// the expected for the strategy at a given moment.
func TestCompleteStrategy(t *testing.T) {
	executor := &Executor{
		contender: buildContender(),
		incumbent: buildIncumbent(),
		recorder:  record.NewFakeRecorder(42),
		strategy:  vanguard,
	}

	// Mimic patch to .spec.targetStep
	executor.contender.release.Spec.TargetStep = 1

	// Execute first part of strategy's first step
	if newSpec, err := ensureCapacityPatch(executor, contenderName, Contender); err != nil {
		t.Fatal(err)
	} else {
		executor.contender.capacityTarget.Spec = *newSpec
	}

	// Mimic Capacity Controller patch to contender's .status.clusters.*.achievedPercent
	for i := range executor.contender.capacityTarget.Status.Clusters {
		executor.contender.capacityTarget.Status.Clusters[i].AchievedPercent = 50
	}

	// Execute second part of strategy's first step
	if newSpec, err := ensureTrafficPatch(executor, contenderName, Contender); err != nil {
		t.Fatal(err)
	} else {
		executor.contender.trafficTarget.Spec = *newSpec
	}

	// Mimic Traffic Controller patch to contender's .status.clusters.*.achievedTraffic
	for i := range executor.contender.trafficTarget.Status.Clusters {
		executor.contender.trafficTarget.Status.Clusters[i].AchievedTraffic = 50
	}

	// Execute third part of strategy's first step
	if newSpec, err := ensureTrafficPatch(executor, incumbentName, Incumbent); err != nil {
		t.Fatal(err)
	} else {
		executor.incumbent.trafficTarget.Spec = *newSpec
	}

	// Mimic Traffic Controller patch to incumbent's .status.clusters.*.achievedTraffic
	for i := range executor.incumbent.trafficTarget.Status.Clusters {
		executor.incumbent.trafficTarget.Status.Clusters[i].AchievedTraffic = 50
	}

	// Execute fourth part of strategy's first step
	if newSpec, err := ensureCapacityPatch(executor, incumbentName, Incumbent); err != nil {
		t.Fatal(err)
	} else {
		executor.incumbent.capacityTarget.Spec = *newSpec
	}

	// Mimic Capacity Controller patch to incumbent's .status.clusters.*.achievedPercent
	for i := range executor.incumbent.capacityTarget.Status.Clusters {
		executor.incumbent.capacityTarget.Status.Clusters[i].AchievedPercent = 50
	}

	// Execute fifth part of strategy's first step
	if newStatus, err := ensureReleasePatch(executor, contenderName); err != nil {
		t.Fatal(err)
	} else {
		executor.contender.release.Status = *newStatus
	}

	// Mimic patch to contender's .spec.targetStep
	executor.contender.release.Spec.TargetStep = 2

	// Execute first part of strategy's second step
	if newSpec, err := ensureCapacityPatch(executor, contenderName, Contender); err != nil {
		t.Fatal(err)
	} else {
		executor.contender.capacityTarget.Spec = *newSpec
	}

	// Mimic Capacity Controller patch to contender's .status.clusters.*.achievedPercent
	for i := range executor.contender.capacityTarget.Status.Clusters {
		executor.contender.capacityTarget.Status.Clusters[i].AchievedPercent = 100
	}

	// Execute second part of strategy's second step
	if newSpec, err := ensureTrafficPatch(executor, contenderName, Contender); err != nil {
		t.Fatal(err)
	} else {
		executor.contender.trafficTarget.Spec = *newSpec
	}

	// Mimic Traffic Controller patch to contender's .status.clusters.*.achievedTraffic
	for i := range executor.contender.trafficTarget.Status.Clusters {
		executor.contender.trafficTarget.Status.Clusters[i].AchievedTraffic = 100
	}

	// Execute third part of strategy's second step
	if newSpec, err := ensureTrafficPatch(executor, incumbentName, Incumbent); err != nil {
		t.Fatal(err)
	} else {
		executor.incumbent.trafficTarget.Spec = *newSpec
	}

	// Mimic Traffic Controller patch to incumbent's .status.clusters.*.achievedTraffic
	for i := range executor.incumbent.trafficTarget.Status.Clusters {
		executor.incumbent.trafficTarget.Status.Clusters[i].AchievedTraffic = 0
	}

	// Execute fourth part of strategy's second step
	if newSpec, err := ensureCapacityPatch(executor, incumbentName, Incumbent); err != nil {
		t.Fatal(err)
	} else {
		executor.incumbent.capacityTarget.Spec = *newSpec
	}

	// Mimic Capacity Controller patch to incumbent's .status.clusters.*.achievedPercent
	for i := range executor.incumbent.capacityTarget.Status.Clusters {
		executor.incumbent.capacityTarget.Status.Clusters[i].AchievedPercent = 0
	}

	// Execute fifth part of strategy's second step, which is the last one
	if err := ensureFinalReleasePatches(executor); err != nil {
		t.Fatal(err)
	}
}

// buildIncumbent returns a releaseInfo with an incumbent release and
// associated objects.
func buildIncumbent() *releaseInfo {

	rel := &v1.Release{
		TypeMeta: metaV1.TypeMeta{
			APIVersion: "shipper.booking.com/v1",
			Kind:       "Release",
		},
		ReleaseMeta: v1.ReleaseMeta{
			ObjectMeta: metaV1.ObjectMeta{
				Name:      incumbentName,
				Namespace: namespace,
				OwnerReferences: []metaV1.OwnerReference{
					metaV1.OwnerReference{
						APIVersion: "shipper.booking.com/v1",
						Kind:       "Application",
						Name:       app.GetName(),
						UID:        app.GetUID(),
					},
				},
				Labels: map[string]string{
					v1.ReleaseLabel: incumbentName,
					v1.AppLabel:     app.GetName(),
				},
				Annotations: map[string]string{
					v1.ReleaseGenerationAnnotation: "0",
				},
			},
			Environment: v1.ReleaseEnvironment{
				Strategy: &vanguard,
			},
		},
		Status: v1.ReleaseStatus{
			Phase:        v1.ReleasePhaseInstalled,
			AchievedStep: 2,
		},
		Spec: v1.ReleaseSpec{
			TargetStep: 2,
		},
	}

	installationTarget := &v1.InstallationTarget{
		TypeMeta: metaV1.TypeMeta{
			APIVersion: "shipper.booking.com/v1",
			Kind:       "InstallationTarget",
		},
		ObjectMeta: metaV1.ObjectMeta{
			Name:      incumbentName,
			Namespace: namespace,
		},
		Status: v1.InstallationTargetStatus{
			Clusters: []*v1.ClusterInstallationStatus{
				{
					Name:   clusterName,
					Status: v1.InstallationStatusInstalled,
				},
			},
		},
		Spec: v1.InstallationTargetSpec{
			Clusters: []string{clusterName},
		},
	}

	capacityTarget := &v1.CapacityTarget{
		TypeMeta: metaV1.TypeMeta{
			APIVersion: "shipper.booking.com/v1",
			Kind:       "CapacityTarget",
		},
		ObjectMeta: metaV1.ObjectMeta{
			Namespace: namespace,
			Name:      incumbentName,
		},
		Status: v1.CapacityTargetStatus{
			Clusters: []v1.ClusterCapacityStatus{
				{
					Name:            clusterName,
					AchievedPercent: 100,
				},
			},
		},
		Spec: v1.CapacityTargetSpec{
			Clusters: []v1.ClusterCapacityTarget{
				{
					Name:    clusterName,
					Percent: 100,
				},
			},
		},
	}

	trafficTarget := &v1.TrafficTarget{
		TypeMeta: metaV1.TypeMeta{
			APIVersion: "shipper.booking.com/v1",
			Kind:       "TrafficTarget",
		},
		ObjectMeta: metaV1.ObjectMeta{
			Namespace: namespace,
			Name:      incumbentName,
		},
		Status: v1.TrafficTargetStatus{
			Clusters: []*v1.ClusterTrafficStatus{
				{
					Name:            clusterName,
					AchievedTraffic: 100,
				},
			},
		},
		Spec: v1.TrafficTargetSpec{
			Clusters: []v1.ClusterTrafficTarget{
				{
					Name:   clusterName,
					Weight: 100,
				},
			},
		},
	}

	return &releaseInfo{
		release:            rel,
		installationTarget: installationTarget,
		capacityTarget:     capacityTarget,
		trafficTarget:      trafficTarget,
	}
}

// buildContender returns a releaseInfo with a contender release and
// associated objects.
func buildContender() *releaseInfo {
	rel := &v1.Release{
		TypeMeta: metaV1.TypeMeta{
			APIVersion: "shipper.booking.com/v1",
			Kind:       "Release",
		},
		ReleaseMeta: v1.ReleaseMeta{
			ObjectMeta: metaV1.ObjectMeta{
				Name:      contenderName,
				Namespace: namespace,
				OwnerReferences: []metaV1.OwnerReference{
					metaV1.OwnerReference{
						APIVersion: "shipper.booking.com/v1",
						Kind:       "Application",
						Name:       app.GetName(),
						UID:        app.GetUID(),
					},
				},
				Labels: map[string]string{
					v1.ReleaseLabel: contenderName,
					v1.AppLabel:     app.GetName(),
				},
				Annotations: map[string]string{
					v1.ReleaseGenerationAnnotation: "1",
				},
			},

			Environment: v1.ReleaseEnvironment{
				Strategy: &vanguard,
			},
		},
		Status: v1.ReleaseStatus{
			Phase:        v1.ReleasePhaseWaitingForStrategy,
			AchievedStep: 0,
		},
		Spec: v1.ReleaseSpec{
			TargetStep: 0,
		},
	}

	installationTarget := &v1.InstallationTarget{
		TypeMeta: metaV1.TypeMeta{
			APIVersion: "shipper.booking.com/v1",
			Kind:       "InstallationTarget",
		},
		ObjectMeta: metaV1.ObjectMeta{
			Namespace: namespace,
			Name:      contenderName,
		},
		Status: v1.InstallationTargetStatus{
			Clusters: []*v1.ClusterInstallationStatus{
				{
					Name:   clusterName,
					Status: v1.InstallationStatusInstalled,
				},
			},
		},
		Spec: v1.InstallationTargetSpec{
			Clusters: []string{clusterName},
		},
	}

	capacityTarget := &v1.CapacityTarget{
		TypeMeta: metaV1.TypeMeta{
			APIVersion: "shipper.booking.com/v1",
			Kind:       "CapacityTarget",
		},
		ObjectMeta: metaV1.ObjectMeta{
			Namespace: namespace,
			Name:      contenderName,
		},
		Status: v1.CapacityTargetStatus{
			Clusters: []v1.ClusterCapacityStatus{
				{
					Name:            clusterName,
					AchievedPercent: 100,
				},
			},
		},
		Spec: v1.CapacityTargetSpec{
			Clusters: []v1.ClusterCapacityTarget{
				{
					Name:    clusterName,
					Percent: 0,
				},
			},
		},
	}

	trafficTarget := &v1.TrafficTarget{
		TypeMeta: metaV1.TypeMeta{
			APIVersion: "shipper.booking.com/v1",
			Kind:       "TrafficTarget",
		},
		ObjectMeta: metaV1.ObjectMeta{
			Namespace: namespace,
			Name:      contenderName,
		},
		Status: v1.TrafficTargetStatus{
			Clusters: []*v1.ClusterTrafficStatus{
				{
					Name:            clusterName,
					AchievedTraffic: 100,
				},
			},
		},
		Spec: v1.TrafficTargetSpec{
			Clusters: []v1.ClusterTrafficTarget{
				{
					Name:   clusterName,
					Weight: 0,
				},
			},
		},
	}

	return &releaseInfo{
		release:            rel,
		installationTarget: installationTarget,
		capacityTarget:     capacityTarget,
		trafficTarget:      trafficTarget,
	}
}

func addCluster(ri *releaseInfo, name string) {
	ri.installationTarget.Spec.Clusters = append(ri.installationTarget.Spec.Clusters, name)
	ri.installationTarget.Status.Clusters = append(ri.installationTarget.Status.Clusters,
		&v1.ClusterInstallationStatus{Name: name, Status: v1.InstallationStatusInstalled},
	)

	ri.capacityTarget.Status.Clusters = append(ri.capacityTarget.Status.Clusters,
		v1.ClusterCapacityStatus{Name: name, AchievedPercent: 100},
	)

	ri.capacityTarget.Spec.Clusters = append(ri.capacityTarget.Spec.Clusters,
		v1.ClusterCapacityTarget{Name: name, Percent: 0},
	)

	ri.trafficTarget.Spec.Clusters = append(ri.trafficTarget.Spec.Clusters,
		v1.ClusterTrafficTarget{Name: name, Weight: 0},
	)
	ri.trafficTarget.Status.Clusters = append(ri.trafficTarget.Status.Clusters,
		&v1.ClusterTrafficStatus{Name: name, AchievedTraffic: 100},
	)
}

// ensureCapacityPatch executes the strategy and returns the content of the
// patch it produced, if any. Returns an error if the number of patches is
// wrong, the patch doesn't implement the CapacityTargetOutdatedResult
// interface or the name is different than the given expectedName.
func ensureCapacityPatch(e *Executor, expectedName string, role role) (*v1.CapacityTargetSpec, error) {
	if patches, err := e.execute(); err != nil {
		return nil, err
	} else {

		aType := v1.StrategyConditionContenderAchievedCapacity
		releaseRole := "contender"

		if role == Incumbent {
			aType = v1.StrategyConditionIncumbentAchievedCapacity
			releaseRole = "incumbent"
		}

		strategyConditions := firstConditionsFromPatches(patches)

		if strategyConditions == nil {
			return nil, fmt.Errorf("could not find a ReleaseUpdateResult patch")
		}

		if s, ok := strategyConditions.GetStatus(aType); ok && s != coreV1.ConditionFalse {
			return nil, fmt.Errorf("%s shouldn't have achieved traffic", releaseRole)
		}

		if len(patches) < 2 || len(patches) > 3 {
			return nil, fmt.Errorf("expected two or three patches, got %d patch(es) instead", len(patches))
		} else {
			if p, ok := patches[0].(*CapacityTargetOutdatedResult); !ok {
				return nil, fmt.Errorf("expected a CapacityTargetOutdatedResult, got something else")
			} else {
				if p.Name != expectedName {
					return nil, fmt.Errorf("expected %q, got %q", expectedName, p.Name)
				} else {
					return p.NewSpec, nil
				}
			}
		}
	}
}

// ensureTrafficPatch executes the strategy and returns the content of the
// patch it produced, if any. Returns an error if the number of patches is
// wrong, the patch doesn't implement the TrafficTargetOutdatedResult
// interface or the name is different than the given expectedName.
func ensureTrafficPatch(e *Executor, expectedName string, role role) (*v1.TrafficTargetSpec, error) {
	if patches, err := e.execute(); err != nil {
		return nil, err
	} else {

		aType := v1.StrategyConditionContenderAchievedTraffic
		releaseRole := "contender"

		if role == Incumbent {
			aType = v1.StrategyConditionIncumbentAchievedTraffic
			releaseRole = "incumbent"
		}

		strategyConditions := firstConditionsFromPatches(patches)

		if s, ok := strategyConditions.GetStatus(aType); ok && s != coreV1.ConditionFalse {
			return nil, fmt.Errorf("%s shouldn't have achieved traffic", releaseRole)
		}

		if len(patches) < 2 || len(patches) > 3 {
			return nil, fmt.Errorf("expected two patches, got %d patch(es) instead", len(patches))
		} else {
			if p, ok := patches[0].(*TrafficTargetOutdatedResult); !ok {
				return nil, fmt.Errorf("expected a TrafficTargetOutdatedResult, got something else")
			} else {
				if p.Name != expectedName {
					return nil, fmt.Errorf("expected %q, got %q", expectedName, p.Name)
				} else {
					return p.NewSpec, nil
				}
			}
		}
	}
}

// ensureReleasePatch executes the strategy and returns the content of the
// patch it produced, if any. Returns an error if the number of patches is
// wrong, the patch doesn't implement the ReleaseUpdateResult interface or
// the name is different than the given expectedName.
func ensureReleasePatch(e *Executor, expectedName string) (*v1.ReleaseStatus, error) {
	if patches, err := e.execute(); err != nil {
		return nil, err
	} else {

		strategyConditions := firstConditionsFromPatches(patches)

		if !strategyConditions.AllTrue(e.contender.release.Spec.TargetStep) {
			return nil, fmt.Errorf("all conditions should have been met")
		}

		if len(patches) != 1 {
			return nil, fmt.Errorf("expected one patch, got %d patches instead", len(patches))
		} else {
			if p, ok := patches[0].(*ReleaseUpdateResult); !ok {
				return nil, fmt.Errorf("expected a ReleaseUpdateResult, got something else")
			} else {
				if p.Name != expectedName {
					return nil, fmt.Errorf("expected %q, got %q", expectedName, p.Name)
				} else {
					return p.NewStatus, nil
				}
			}
		}
	}
}

// ensureFinalReleasePatches executes the strategy and returns an error if
// the number of patches is wrong, the patches' phases are wrong for either
// incumbent or contender.
func ensureFinalReleasePatches(e *Executor) error {
	if patches, err := e.execute(); err != nil {
		return err
	} else {

		strategyConditions := firstConditionsFromPatches(patches)

		if !strategyConditions.AllTrue(int32(e.contender.release.Spec.TargetStep)) {
			return fmt.Errorf("all conditions should be true")
		}

		if len(patches) != 2 {
			return fmt.Errorf("expected two patches, got %d patches instead", len(patches))
		} else {
			for _, patch := range patches {
				if p, ok := patch.(*ReleaseUpdateResult); !ok {
					return fmt.Errorf("expected a ReleaseUpdateResult, got something else")
				} else {
					if p.Name == contenderName {
						if p.NewStatus.Phase != v1.ReleasePhaseInstalled {
							return fmt.Errorf(
								"expected contender phase %q, got %q",
								v1.ReleasePhaseInstalled, p.NewStatus.Phase)
						}
						if p.NewStatus.AchievedStep != 2 {
							return fmt.Errorf(
								"expected contender achievedSteps %d, got %d",
								2, p.NewStatus.AchievedStep)
						}
					} else if p.Name == incumbentName {
						if p.NewStatus.Phase != v1.ReleasePhaseSuperseded {
							return fmt.Errorf(
								"expected incumbent phase %q, got %q",
								v1.ReleasePhaseSuperseded, p.NewStatus.Phase)
						}
						if p.NewStatus.AchievedStep != e.incumbent.release.Status.AchievedStep {
							return fmt.Errorf(
								"expected incumbent achievedSteps %d, got %d",
								e.incumbent.release.Status.AchievedStep, p.NewStatus.AchievedStep)
						}
					}
				}
			}
		}
	}
	return nil
}

func firstConditionsFromPatches(patches []ExecutorResult) conditions.StrategyConditionsMap {
	var strategyConditions conditions.StrategyConditionsMap
	for p := range patches {
		if patch, ok := patches[p].(*ReleaseUpdateResult); ok {
			if patch.NewStatus.Strategy != nil {
				strategyConditions = conditions.NewStrategyConditions(patch.NewStatus.Strategy.Conditions...)
			}
		}
	}
	return strategyConditions
}
