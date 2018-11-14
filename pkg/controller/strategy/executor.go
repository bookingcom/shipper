package strategy

import (
	"fmt"
	"sort"
	"time"

	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/conditions"
	"github.com/bookingcom/shipper/pkg/controller"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

type releaseInfo struct {
	release            *shipper.Release
	installationTarget *shipper.InstallationTarget
	trafficTarget      *shipper.TrafficTarget
	capacityTarget     *shipper.CapacityTarget
}

type Executor struct {
	contender *releaseInfo
	incumbent *releaseInfo
	recorder  record.EventRecorder
	strategy  shipper.RolloutStrategy
}

func (s *Executor) info(format string, args ...interface{}) {
	glog.Infof("Release %q: %s", controller.MetaKey(s.contender.release), fmt.Sprintf(format, args...))
}

func (s *Executor) event(obj runtime.Object, format string, args ...interface{}) {
	s.recorder.Eventf(
		obj,
		corev1.EventTypeNormal,
		"StrategyApplied",
		format,
		args,
	)
}

type ReleaseStrategyStateTransition struct {
	State    string
	Previous shipper.StrategyState
	New      shipper.StrategyState
}

// execute executes the strategy. It returns an ExecutorResult, if a patch should
// be performed into some of the associated Release objects and an error if an error
// has happened. Currently if both values are nil it means that the operation was
// successful but no modifications are required.
func (s *Executor) execute() ([]ExecutorResult, []ReleaseStrategyStateTransition, error) {
	targetStep := s.contender.release.Spec.TargetStep

	if targetStep >= int32(len(s.strategy.Steps)) {
		return nil, nil, fmt.Errorf("no step %d in strategy for Release %q", targetStep,
			controller.MetaKey(s.contender.release))
	}
	strategyStep := s.strategy.Steps[targetStep]

	lastStepIndex := int32(len(s.strategy.Steps) - 1)
	if lastStepIndex < 0 {
		lastStepIndex = 0
	}

	isLastStep := targetStep == lastStepIndex

	var releaseStrategyConditions []shipper.ReleaseStrategyCondition

	if s.contender.release.Status.Strategy != nil {
		releaseStrategyConditions = s.contender.release.Status.Strategy.Conditions
	}

	strategyConditions := conditions.NewStrategyConditions(releaseStrategyConditions...)

	lastTransitionTime := time.Now()

	//////////////////////////////////////////////////////////////////////////
	// Installation
	//
	if contenderReady, clusters := checkInstallation(s.contender); !contenderReady {
		s.info("installation pending")

		if len(s.contender.installationTarget.Spec.Clusters) != len(s.contender.installationTarget.Status.Clusters) {
			strategyConditions.SetUnknown(
				shipper.StrategyConditionContenderAchievedInstallation,
				conditions.StrategyConditionsUpdate{
					Step:               targetStep,
					LastTransitionTime: lastTransitionTime,
				})
		} else {
			// Contender installation is not ready yet, so we update conditions
			// accordingly.
			strategyConditions.SetFalse(
				shipper.StrategyConditionContenderAchievedInstallation,
				conditions.StrategyConditionsUpdate{
					Reason:             conditions.ClustersNotReady,
					Message:            fmt.Sprintf("clusters pending installation: %v", clusters),
					Step:               targetStep,
					LastTransitionTime: lastTransitionTime,
				})
		}

		return []ExecutorResult{s.buildContenderStrategyConditionsPatch(strategyConditions, targetStep, isLastStep)},
			nil,
			nil

	} else {
		s.info("installation finished")

		strategyConditions.SetTrue(
			shipper.StrategyConditionContenderAchievedInstallation,
			conditions.StrategyConditionsUpdate{
				LastTransitionTime: lastTransitionTime,
				Step:               targetStep,
			})
	}

	//////////////////////////////////////////////////////////////////////////
	// Contender
	//
	{

		//////////////////////////////////////////////////////////////////////////
		// Contender Capacity
		//
		capacityWeight := strategyStep.Capacity.Contender

		if achieved, newSpec, clustersNotReady := checkCapacity(s.contender.capacityTarget, uint(capacityWeight)); !achieved {
			s.info("contender %q hasn't achieved capacity yet", s.contender.release.Name)

			var patches []ExecutorResult

			strategyConditions.SetFalse(
				shipper.StrategyConditionContenderAchievedCapacity,
				conditions.StrategyConditionsUpdate{
					Reason:             conditions.ClustersNotReady,
					Message:            fmt.Sprintf("clusters pending capacity adjustments: %v", clustersNotReady),
					Step:               targetStep,
					LastTransitionTime: lastTransitionTime,
				})

			if newSpec != nil {
				patches = append(patches, &CapacityTargetOutdatedResult{
					NewSpec: newSpec,
					Name:    s.contender.release.Name,
				})
			}

			patches = append(patches, s.buildContenderStrategyConditionsPatch(strategyConditions, targetStep, isLastStep))

			return patches, nil, nil
		} else {
			s.info("contender %q has achieved capacity", s.contender.release.Name)

			strategyConditions.SetTrue(
				shipper.StrategyConditionContenderAchievedCapacity,
				conditions.StrategyConditionsUpdate{
					Step:               targetStep,
					LastTransitionTime: lastTransitionTime,
				})
		}

		//////////////////////////////////////////////////////////////////////////
		// Contender Traffic
		//
		trafficWeight := strategyStep.Traffic.Contender

		if achieved, newSpec, clustersNotReady := checkTraffic(s.contender.trafficTarget, uint32(trafficWeight), contenderTrafficComparison); !achieved {
			s.info("contender %q hasn't achieved traffic yet", s.contender.release.Name)

			var patches []ExecutorResult

			strategyConditions.SetFalse(
				shipper.StrategyConditionContenderAchievedTraffic,
				conditions.StrategyConditionsUpdate{
					Reason:             conditions.ClustersNotReady,
					Message:            fmt.Sprintf("clusters pending traffic adjustments: %v", clustersNotReady),
					Step:               targetStep,
					LastTransitionTime: lastTransitionTime,
				})

			if newSpec != nil {
				patches = append(patches, &TrafficTargetOutdatedResult{
					NewSpec: newSpec,
					Name:    s.contender.release.Name,
				})
			}

			patches = append(patches, s.buildContenderStrategyConditionsPatch(strategyConditions, targetStep, isLastStep))

			return patches, nil, nil
		} else {
			s.info("contender %q has achieved traffic", s.contender.release.Name)

			strategyConditions.SetTrue(
				shipper.StrategyConditionContenderAchievedTraffic,
				conditions.StrategyConditionsUpdate{
					Step:               targetStep,
					LastTransitionTime: lastTransitionTime,
				})
		}
	}

	//////////////////////////////////////////////////////////////////////////
	// Incumbent
	//
	if s.incumbent != nil {

		//////////////////////////////////////////////////////////////////////////
		// Incumbent Traffic
		//
		trafficWeight := strategyStep.Traffic.Incumbent

		if achieved, newSpec, clustersNotReady := checkTraffic(s.incumbent.trafficTarget, uint32(trafficWeight), incumbentTrafficComparison); !achieved {
			s.info("incumbent %q hasn't achieved traffic yet", s.incumbent.release.Name)

			var patches []ExecutorResult

			strategyConditions.SetFalse(
				shipper.StrategyConditionIncumbentAchievedTraffic,
				conditions.StrategyConditionsUpdate{
					Reason:             conditions.ClustersNotReady,
					Message:            fmt.Sprintf("clusters pending traffic adjustments: %v", clustersNotReady),
					Step:               targetStep,
					LastTransitionTime: lastTransitionTime,
				})

			if newSpec != nil {
				patches = append(patches, &TrafficTargetOutdatedResult{
					NewSpec: newSpec,
					Name:    s.incumbent.release.Name,
				})
			}

			patches = append(patches, s.buildContenderStrategyConditionsPatch(strategyConditions, targetStep, isLastStep))

			return patches, nil, nil
		} else {
			s.info("incumbent %q has achieved traffic", s.incumbent.release.Name)

			strategyConditions.SetTrue(
				shipper.StrategyConditionIncumbentAchievedTraffic,
				conditions.StrategyConditionsUpdate{
					Step:               targetStep,
					LastTransitionTime: lastTransitionTime,
				})
		}

		//////////////////////////////////////////////////////////////////////////
		// Incumbent Capacity
		//
		capacityWeight := strategyStep.Capacity.Incumbent

		if achieved, newSpec, clustersNotReady := checkCapacity(s.incumbent.capacityTarget, uint(capacityWeight)); !achieved {
			s.info("incumbent %q hasn't achieved capacity yet", s.incumbent.release.Name)

			var patches []ExecutorResult

			strategyConditions.SetFalse(
				shipper.StrategyConditionIncumbentAchievedCapacity,
				conditions.StrategyConditionsUpdate{
					Reason:             conditions.ClustersNotReady,
					Message:            fmt.Sprintf("clusters pending capacity adjustments: %v", clustersNotReady),
					Step:               targetStep,
					LastTransitionTime: lastTransitionTime,
				})

			if newSpec != nil {
				patches = append(patches, &CapacityTargetOutdatedResult{
					NewSpec: newSpec,
					Name:    s.incumbent.release.Name,
				})
			}

			patches = append(patches, s.buildContenderStrategyConditionsPatch(strategyConditions, targetStep, isLastStep))

			return patches, nil, nil
		} else {
			s.info("incumbent %q has achieved capacity", s.incumbent.release.Name)

			strategyConditions.SetTrue(
				shipper.StrategyConditionIncumbentAchievedCapacity,
				conditions.StrategyConditionsUpdate{
					Step:               targetStep,
					LastTransitionTime: lastTransitionTime,
				})
		}
	} else {
		s.info("no incumbent, must be a new app")
	}

	//////////////////////////////////////////////////////////////////////////
	// Step wrap up
	//
	{
		var releasePatches []ExecutorResult
		var releaseStrategyStateTransitions []ReleaseStrategyStateTransition

		contenderStatus := s.contender.release.Status.DeepCopy()

		newReleaseStrategyState := strategyConditions.AsReleaseStrategyState(
			s.contender.release.Spec.TargetStep,
			s.incumbent != nil,
			isLastStep)

		oldReleaseStrategyState := shipper.ReleaseStrategyState{}
		if contenderStatus.Strategy != nil {
			oldReleaseStrategyState = contenderStatus.Strategy.State
		}

		sort.Slice(contenderStatus.Conditions, func(i, j int) bool {
			return contenderStatus.Conditions[i].Type < contenderStatus.Conditions[j].Type
		})

		releaseStrategyStateTransitions =
			getReleaseStrategyStateTransitions(
				oldReleaseStrategyState,
				newReleaseStrategyState,
				releaseStrategyStateTransitions)

		contenderStatus.Strategy = &shipper.ReleaseStrategyStatus{
			Conditions: strategyConditions.AsReleaseStrategyConditions(),
			State:      newReleaseStrategyState,
		}

		previouslyAchievedStep := s.contender.release.Status.AchievedStep
		if previouslyAchievedStep == nil || targetStep != previouslyAchievedStep.Step {
			// we validate that it fits in the len() of Strategy.Steps early in the process
			targetStepName := s.contender.release.Spec.Environment.Strategy.Steps[targetStep].Name
			contenderStatus.AchievedStep = &shipper.AchievedStep{
				Step: targetStep,
				Name: targetStepName,
			}
		}

		if targetStep == lastStepIndex {
			condition := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeComplete, corev1.ConditionTrue, "", "")
			releaseutil.SetReleaseCondition(contenderStatus, *condition)
		}

		releasePatches = append(releasePatches, &ReleaseUpdateResult{
			NewStatus: contenderStatus,
			Name:      s.contender.release.Name,
		})

		s.event(s.contender.release, "step %d finished", targetStep)
		return releasePatches, releaseStrategyStateTransitions, nil
	}
}

func getReleaseStrategyStateTransitions(
	oldState shipper.ReleaseStrategyState,
	newState shipper.ReleaseStrategyState,
	stateTransitions []ReleaseStrategyStateTransition,
) []ReleaseStrategyStateTransition {
	if oldState.WaitingForCapacity != newState.WaitingForCapacity {
		stateTransitions = append(stateTransitions, ReleaseStrategyStateTransition{State: "WaitingForCapacity", New: newState.WaitingForCapacity, Previous: valueOrUnknown(oldState.WaitingForCapacity)})
	}
	if oldState.WaitingForCommand != newState.WaitingForCommand {
		stateTransitions = append(stateTransitions, ReleaseStrategyStateTransition{State: "WaitingForCommand", New: newState.WaitingForCommand, Previous: valueOrUnknown(oldState.WaitingForCapacity)})
	}
	if oldState.WaitingForInstallation != newState.WaitingForInstallation {
		stateTransitions = append(stateTransitions, ReleaseStrategyStateTransition{State: "WaitingForInstallation", New: newState.WaitingForInstallation, Previous: valueOrUnknown(oldState.WaitingForCapacity)})
	}
	if oldState.WaitingForTraffic != newState.WaitingForTraffic {
		stateTransitions = append(stateTransitions, ReleaseStrategyStateTransition{State: "WaitingForTraffic", New: newState.WaitingForTraffic, Previous: valueOrUnknown(oldState.WaitingForCapacity)})
	}
	return stateTransitions
}

func valueOrUnknown(v shipper.StrategyState) shipper.StrategyState {
	if len(v) < 1 {
		v = shipper.StrategyStateUnknown
	}
	return v
}

func (s *Executor) buildContenderStrategyConditionsPatch(
	c conditions.StrategyConditionsMap,
	step int32,
	isLastStep bool,
) *ReleaseUpdateResult {
	newStatus := s.contender.release.Status.DeepCopy()
	newStatus.Strategy = &shipper.ReleaseStrategyStatus{
		Conditions: c.AsReleaseStrategyConditions(),
		State:      c.AsReleaseStrategyState(step, s.incumbent != nil, isLastStep),
	}
	return &ReleaseUpdateResult{
		NewStatus: newStatus,
		Name:      s.contender.release.Name,
	}
}
