package strategy

import (
	"fmt"
	"time"

	"github.com/golang/glog"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"

	"github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	"github.com/bookingcom/shipper/pkg/conditions"
	"github.com/bookingcom/shipper/pkg/controller"
)

type releaseInfo struct {
	release            *v1.Release
	installationTarget *v1.InstallationTarget
	trafficTarget      *v1.TrafficTarget
	capacityTarget     *v1.CapacityTarget
}

type Executor struct {
	contender *releaseInfo
	incumbent *releaseInfo
	recorder  record.EventRecorder
	strategy  v1.RolloutStrategy
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

// execute executes the strategy. It returns an ExecutorResult, if a patch should
// be performed into some of the associated Release objects and an error if an error
// has happened. Currently if both values are nil it means that the operation was
// successful but no modifications are required.
func (s *Executor) execute() ([]ExecutorResult, error) {
	targetStep := s.contender.release.Spec.TargetStep

	if targetStep >= int32(len(s.strategy.Steps)) {
		return nil, fmt.Errorf("no step %d in strategy for Release %q", targetStep,
			controller.MetaKey(s.contender.release))
	}
	strategyStep := s.strategy.Steps[targetStep]

	lastStepIndex := int32(len(s.strategy.Steps) - 1)
	if lastStepIndex < 0 {
		lastStepIndex = 0
	}

	isLastStep := targetStep == lastStepIndex

	var releaseStrategyConditions []v1.ReleaseStrategyCondition

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
				v1.StrategyConditionContenderAchievedInstallation,
				conditions.StrategyConditionsUpdate{
					Step:               targetStep,
					LastTransitionTime: lastTransitionTime,
				})
		} else {
			// Contender installation is not ready yet, so we update conditions
			// accordingly.
			strategyConditions.SetFalse(
				v1.StrategyConditionContenderAchievedInstallation,
				conditions.StrategyConditionsUpdate{
					Reason:             conditions.ClustersNotReady,
					Message:            fmt.Sprintf("clusters pending installation: %v", clusters),
					Step:               targetStep,
					LastTransitionTime: lastTransitionTime,
				})
		}

		return []ExecutorResult{s.buildContenderStrategyConditionsPatch(strategyConditions, targetStep, isLastStep)},
			nil

	} else {
		s.info("installation finished")

		strategyConditions.SetTrue(
			v1.StrategyConditionContenderAchievedInstallation,
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

		if achieved, newSpec, clustersNotReady := checkCapacity(s.contender.capacityTarget, uint(capacityWeight), contenderCapacityComparison); !achieved {
			s.info("contender %q hasn't achieved capacity yet", s.contender.release.Name)

			var patches []ExecutorResult

			strategyConditions.SetFalse(
				v1.StrategyConditionContenderAchievedCapacity,
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

			return patches, nil
		} else {
			s.info("contender %q has achieved capacity", s.contender.release.Name)

			strategyConditions.SetTrue(
				v1.StrategyConditionContenderAchievedCapacity,
				conditions.StrategyConditionsUpdate{
					Step:               targetStep,
					LastTransitionTime: lastTransitionTime,
				})
		}

		//////////////////////////////////////////////////////////////////////////
		// Contender Traffic
		//
		trafficWeight := strategyStep.Traffic.Contender

		if achieved, newSpec, clustersNotReady := checkTraffic(s.contender.trafficTarget, uint(trafficWeight), contenderTrafficComparison); !achieved {
			s.info("contender %q hasn't achieved traffic yet", s.contender.release.Name)

			var patches []ExecutorResult

			strategyConditions.SetFalse(
				v1.StrategyConditionContenderAchievedTraffic,
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

			return patches, nil
		} else {
			s.info("contender %q has achieved traffic", s.contender.release.Name)

			strategyConditions.SetTrue(
				v1.StrategyConditionContenderAchievedTraffic,
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

		if achieved, newSpec, clustersNotReady := checkTraffic(s.incumbent.trafficTarget, uint(trafficWeight), incumbentTrafficComparison); !achieved {
			s.info("incumbent %q hasn't achieved traffic yet", s.incumbent.release.Name)

			var patches []ExecutorResult

			strategyConditions.SetFalse(
				v1.StrategyConditionIncumbentAchievedTraffic,
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

			return patches, nil
		} else {
			s.info("incumbent %q has achieved traffic", s.incumbent.release.Name)

			strategyConditions.SetTrue(
				v1.StrategyConditionIncumbentAchievedTraffic,
				conditions.StrategyConditionsUpdate{
					Step:               targetStep,
					LastTransitionTime: lastTransitionTime,
				})
		}

		//////////////////////////////////////////////////////////////////////////
		// Incumbent Capacity
		//
		capacityWeight := strategyStep.Capacity.Incumbent

		if achieved, newSpec, clustersNotReady := checkCapacity(s.incumbent.capacityTarget, uint(capacityWeight), incumbentCapacityComparison); !achieved {
			s.info("incumbent %q hasn't achieved capacity yet", s.incumbent.release.Name)

			var patches []ExecutorResult

			strategyConditions.SetFalse(
				v1.StrategyConditionIncumbentAchievedCapacity,
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

			return patches, nil
		} else {
			s.info("incumbent %q has achieved capacity", s.incumbent.release.Name)

			strategyConditions.SetTrue(
				v1.StrategyConditionIncumbentAchievedCapacity,
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
		var contenderPhase string

		if isLastStep {
			contenderPhase = v1.ReleasePhaseInstalled
		} else {
			contenderPhase = v1.ReleasePhaseWaitingForCommand
		}

		var releasePatches []ExecutorResult

		reportedStep := s.contender.release.Status.AchievedStep
		reportedPhase := s.contender.release.Status.Phase

		contenderStatus := s.contender.release.Status.DeepCopy()
		contenderStatus.Strategy = &v1.ReleaseStrategyStatus{
			Conditions: strategyConditions.AsReleaseStrategyConditions(),
			State: strategyConditions.AsReleaseStrategyState(
				s.contender.release.Spec.TargetStep,
				s.incumbent != nil,
				isLastStep),
		}

		if targetStep != reportedStep || contenderPhase != reportedPhase {
			contenderStatus.AchievedStep = targetStep
			contenderStatus.Phase = contenderPhase
		}

		releasePatches = append(releasePatches, &ReleaseUpdateResult{
			NewStatus: contenderStatus,
			Name:      s.contender.release.Name,
		})

		if s.incumbent != nil {
			incumbentPhase := v1.ReleasePhaseInstalled
			if isLastStep {
				incumbentPhase = v1.ReleasePhaseSuperseded
			}

			if incumbentPhase != s.incumbent.release.Status.Phase {
				incumbentStatus := &v1.ReleaseStatus{
					Phase:        incumbentPhase,
					AchievedStep: s.incumbent.release.Status.AchievedStep,
				}
				releasePatches = append(releasePatches, &ReleaseUpdateResult{
					NewStatus: incumbentStatus,
					Name:      s.incumbent.release.Name,
				})
			}
		}

		s.event(s.contender.release, "step %d finished", targetStep)
		return releasePatches, nil
	}
}

func (s *Executor) buildContenderStrategyConditionsPatch(
	c conditions.StrategyConditionsMap,
	step int32,
	isLastStep bool,
) *ReleaseUpdateResult {
	newStatus := s.contender.release.Status.DeepCopy()
	//newStatus.AchievedStep = uint(step)
	newStatus.Strategy = &v1.ReleaseStrategyStatus{
		Conditions: c.AsReleaseStrategyConditions(),
		State:      c.AsReleaseStrategyState(step, s.incumbent != nil, isLastStep),
	}
	return &ReleaseUpdateResult{
		NewStatus: newStatus,
		Name:      s.contender.release.Name,
	}
}
