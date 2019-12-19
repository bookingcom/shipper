package release

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/klog"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/controller"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	"github.com/bookingcom/shipper/pkg/util/conditions"
)

type StrategyExecutor struct {
	curr, prev, succ *releaseInfo
	hasIncumbent     bool
}

func NewStrategyExecutor(curr, prev, succ *releaseInfo, hasIncumbent bool) *StrategyExecutor {
	return &StrategyExecutor{
		curr:         curr,
		prev:         prev,
		succ:         succ,
		hasIncumbent: hasIncumbent,
	}
}

type PipelineContinuation bool

const (
	PipelineBreak    PipelineContinuation = false
	PipelineContinue                      = true
)

type PipelineStep func(*StrategyExecutor, conditions.StrategyConditionsMap) (PipelineContinuation, []ExecutorResult, []ReleaseStrategyStateTransition)

type Pipeline []PipelineStep

func NewPipeline() *Pipeline {
	return new(Pipeline)
}

func (p *Pipeline) Enqueue(step PipelineStep) {
	*p = append(*p, step)
}

func (p *Pipeline) Process(e *StrategyExecutor, cond conditions.StrategyConditionsMap) (bool, []ExecutorResult, []ReleaseStrategyStateTransition) {
	var res []ExecutorResult
	var trans []ReleaseStrategyStateTransition
	for _, step := range *p {
		cont, stepres, steptrans := step(e, cond)
		res = append(res, stepres...)
		trans = append(trans, steptrans...)
		if cont == PipelineBreak {
			return false, res, trans
		}
	}

	return true, res, trans
}

func genInstallationEnforcer(curr, succ *releaseInfo) PipelineStep {
	return func(e *StrategyExecutor, cond conditions.StrategyConditionsMap) (PipelineContinuation, []ExecutorResult, []ReleaseStrategyStateTransition) {
		strategy := curr.release.Spec.Environment.Strategy
		targetStep := curr.release.Spec.TargetStep
		isLastStep := int(targetStep) == len(strategy.Steps)-1

		if ready, clusters := checkInstallation(curr.installationTarget); !ready {
			cond.SetFalse(
				shipper.StrategyConditionContenderAchievedInstallation,
				conditions.StrategyConditionsUpdate{
					Reason:             ClustersNotReady,
					Message:            fmt.Sprintf("clusters pending installation: %v. for more details try `kubectl describe it %s`", clusters, curr.installationTarget.Name),
					Step:               targetStep,
					LastTransitionTime: time.Now(),
				},
			)

			return PipelineBreak, e.buildContenderStrategyConditionsPatch(cond, targetStep, isLastStep, e.hasIncumbent), nil
		}

		cond.SetTrue(
			shipper.StrategyConditionContenderAchievedInstallation,
			conditions.StrategyConditionsUpdate{
				LastTransitionTime: time.Now(),
				Step:               targetStep,
			},
		)

		return PipelineContinue, nil, nil
	}
}

func genCapacityEnforcer(curr, succ *releaseInfo) PipelineStep {
	return func(e *StrategyExecutor, cond conditions.StrategyConditionsMap) (PipelineContinuation, []ExecutorResult, []ReleaseStrategyStateTransition) {
		var targetStep, capacityWeight int32
		var strategy *shipper.RolloutStrategy
		var strategyStep shipper.RolloutStrategyStep
		var condType shipper.StrategyConditionType

		isHead := succ == nil

		if isHead {
			targetStep = curr.release.Spec.TargetStep
			strategy = curr.release.Spec.Environment.Strategy
			strategyStep = strategy.Steps[targetStep]
			capacityWeight = strategyStep.Capacity.Contender
			condType = shipper.StrategyConditionContenderAchievedCapacity
		} else {
			targetStep = succ.release.Spec.TargetStep
			strategy = succ.release.Spec.Environment.Strategy
			strategyStep = strategy.Steps[targetStep]
			capacityWeight = strategyStep.Capacity.Incumbent
			condType = shipper.StrategyConditionIncumbentAchievedCapacity
		}

		isLastStep := int(targetStep) == len(strategy.Steps)-1

		if achieved, newSpec, clustersNotReady := checkCapacity(curr.capacityTarget, capacityWeight); !achieved {
			e.info("release hasn't achieved capacity yet")

			var patches []ExecutorResult

			cond.SetFalse(
				condType,
				conditions.StrategyConditionsUpdate{
					Reason:             ClustersNotReady,
					Message:            fmt.Sprintf("release %q hasn't achieved capacity in clusters: %v. for more details try `kubectl describe ct %s`", curr.release.Name, clustersNotReady, curr.capacityTarget.Name),
					Step:               targetStep,
					LastTransitionTime: time.Now(),
				},
			)

			if newSpec != nil {
				patches = append(patches, &CapacityTargetOutdatedResult{
					NewSpec: newSpec,
					Name:    curr.release.Name,
				})
			}

			patches = append(patches, e.buildContenderStrategyConditionsPatch(cond, targetStep, isLastStep, e.hasIncumbent)...)

			return PipelineBreak, patches, nil
		}

		e.info("release has achieved capacity")

		cond.SetTrue(
			condType,
			conditions.StrategyConditionsUpdate{
				Step:               targetStep,
				LastTransitionTime: time.Now(),
			},
		)

		return PipelineContinue, nil, nil
	}
}

func genTrafficEnforcer(curr, succ *releaseInfo) PipelineStep {
	return func(e *StrategyExecutor, cond conditions.StrategyConditionsMap) (PipelineContinuation, []ExecutorResult, []ReleaseStrategyStateTransition) {
		var targetStep, trafficWeight int32
		var strategy *shipper.RolloutStrategy
		var strategyStep shipper.RolloutStrategyStep
		var condType shipper.StrategyConditionType

		// isHead is equivalent to the contender concept: it hjas no
		// successor and it defines the desired state purely based on
		// it's own spec. Any tail release will first look at the state
		// of the release in front of it in order to figure out the
		// realistic state of the world.
		isHead := succ == nil

		if isHead {
			targetStep = curr.release.Spec.TargetStep
			strategy = curr.release.Spec.Environment.Strategy
			strategyStep = strategy.Steps[targetStep]
			trafficWeight = strategyStep.Traffic.Contender
			condType = shipper.StrategyConditionContenderAchievedTraffic
		} else {
			targetStep = succ.release.Spec.TargetStep
			strategy = succ.release.Spec.Environment.Strategy
			strategyStep = strategy.Steps[targetStep]
			trafficWeight = strategyStep.Traffic.Incumbent
			condType = shipper.StrategyConditionIncumbentAchievedTraffic
		}

		isLastStep := int(targetStep) == len(strategy.Steps)-1

		if achieved, newSpec, reason := checkTraffic(curr.trafficTarget, uint32(trafficWeight)); !achieved {
			e.info("release hasn't achieved traffic yet")

			var patches []ExecutorResult

			cond.SetFalse(
				condType,
				conditions.StrategyConditionsUpdate{
					Reason:             ClustersNotReady,
					Message:            fmt.Sprintf("release %q hasn't achieved traffic in clusters: %s. for more details try `kubectl describe tt %s`", curr.release.Name, reason, curr.trafficTarget.Name),
					Step:               targetStep,
					LastTransitionTime: time.Now(),
				},
			)

			if newSpec != nil {
				patches = append(patches, &TrafficTargetOutdatedResult{
					NewSpec: newSpec,
					Name:    curr.release.Name,
				})
			}

			patches = append(patches, e.buildContenderStrategyConditionsPatch(cond, targetStep, isLastStep, e.hasIncumbent)...)

			return PipelineBreak, patches, nil
		}

		e.info("release has achieved traffic")

		cond.SetTrue(
			condType,
			conditions.StrategyConditionsUpdate{
				Step:               targetStep,
				LastTransitionTime: time.Now(),
			},
		)

		return PipelineContinue, nil, nil
	}
}

func genReleaseStrategyStateEnforcer(curr, succ *releaseInfo) PipelineStep {
	return func(e *StrategyExecutor, cond conditions.StrategyConditionsMap) (PipelineContinuation, []ExecutorResult, []ReleaseStrategyStateTransition) {
		var releasePatches []ExecutorResult
		var releaseStrategyStateTransitions []ReleaseStrategyStateTransition

		targetStep := curr.release.Spec.TargetStep
		strategy := curr.release.Spec.Environment.Strategy

		isLastStep := int(targetStep) == len(strategy.Steps)-1
		relStatus := curr.release.Status.DeepCopy()

		newReleaseStrategyState := cond.AsReleaseStrategyState(
			curr.release.Spec.TargetStep,
			e.hasIncumbent,
			isLastStep)

		oldReleaseStrategyState := shipper.ReleaseStrategyState{}
		if relStatus.Strategy != nil {
			oldReleaseStrategyState = relStatus.Strategy.State
		}

		releaseStrategyStateTransitions =
			getReleaseStrategyStateTransitions(
				oldReleaseStrategyState,
				newReleaseStrategyState,
				releaseStrategyStateTransitions)

		relStrategyStatus := &shipper.ReleaseStrategyStatus{
			Conditions: cond.AsReleaseStrategyConditions(),
			State:      newReleaseStrategyState,
		}

		if !equality.Semantic.DeepEqual(curr.release.Status.Strategy, relStrategyStatus) {
			releasePatches = append(releasePatches, &ReleaseUpdateResult{
				NewStrategyStatus: relStrategyStatus,
				Name:              curr.release.Name,
			})
		}

		return PipelineContinue, releasePatches, releaseStrategyStateTransitions
	}
}

/*
	For each release object:
	0. Ensure release scheduled.
	  0.1. Choose clusters.
	  0.2. Ensure target objects exist.
	    0.2.1. Compare chosen clusters and if different, update the spec.
	1. Find it's ancestor.
	2. For the head release, ensure installation.
	  2.1. Simply check installation targets.
	3. For the head release, ensure capacity.
	  3.1. Ensure the capacity corresponds to the strategy contender.
	4. For the head release, ensure traffic.
	  4.1. Ensure the traffic corresponds to the strategy contender.
	5. For a tail release, ensure traffic.
	  5.1. Look at the leader and check it's target traffic.
	  5.2. Look at the strategy and figure out the target traffic.
	6. For a tail release, ensure capacity.
	  6.1. Look at the leader and check it's target capacity.
	  6.2 Look at the strategy and figure out the target capacity.
	7. Make necessary adjustments to the release object.
*/

func (e *StrategyExecutor) Execute() (bool, []ExecutorResult, []ReleaseStrategyStateTransition, error) {
	strategy := e.curr.release.Spec.Environment.Strategy
	targetStep := e.curr.release.Spec.TargetStep
	if targetStep >= int32(len(strategy.Steps)) {
		err := fmt.Errorf("no step %d in strategy for Release %q",
			targetStep, controller.MetaKey(e.curr.release))
		return false, nil, nil, shippererrors.NewUnrecoverableError(err)
	}

	var releaseStrategyConditions []shipper.ReleaseStrategyCondition
	if e.curr.release.Status.Strategy != nil {
		releaseStrategyConditions = e.curr.release.Status.Strategy.Conditions
	}
	cond := conditions.NewStrategyConditions(releaseStrategyConditions...)

	isHead, hasTail := e.succ == nil, e.prev != nil

	pipeline := NewPipeline()
	if isHead {
		pipeline.Enqueue(genInstallationEnforcer(e.curr, nil))
	}
	pipeline.Enqueue(genCapacityEnforcer(e.curr, e.succ))
	pipeline.Enqueue(genTrafficEnforcer(e.curr, e.succ))

	if isHead {
		if hasTail {
			pipeline.Enqueue(genTrafficEnforcer(e.prev, e.curr))
			pipeline.Enqueue(genCapacityEnforcer(e.prev, e.curr))
		}
		pipeline.Enqueue(genReleaseStrategyStateEnforcer(e.curr, nil))
	}

	complete, patches, trans := pipeline.Process(e, cond)

	return complete, patches, trans, nil
}

func (e *StrategyExecutor) buildContenderStrategyConditionsPatch(
	cond conditions.StrategyConditionsMap,
	step int32,
	isLastStep bool,
	hasIncumbent bool,
) []ExecutorResult {
	newStrategyStatus := &shipper.ReleaseStrategyStatus{
		Conditions: cond.AsReleaseStrategyConditions(),
		State:      cond.AsReleaseStrategyState(step, hasIncumbent, isLastStep),
	}
	res := make([]ExecutorResult, 0, 1)
	if !equality.Semantic.DeepEqual(e.curr.release.Status.Strategy, newStrategyStatus) {
		res = append(res, &ReleaseUpdateResult{
			NewStrategyStatus: newStrategyStatus,
			Name:              e.curr.release.Name,
		})
	}
	return res
}

func (e *StrategyExecutor) info(format string, args ...interface{}) {
	klog.Infof("Release %q: %s", controller.MetaKey(e.curr.release), fmt.Sprintf(format, args...))
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
