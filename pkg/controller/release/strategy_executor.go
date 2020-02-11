package release

import (
	"fmt"
	"time"

	"k8s.io/klog"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/controller"
	"github.com/bookingcom/shipper/pkg/util/conditions"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

type PipelineContinuation bool

const (
	PipelineBreak    PipelineContinuation = false
	PipelineContinue                      = true
)

type PipelineStep func(*shipper.RolloutStrategy, int32, Extra, conditions.StrategyConditionsMap) (PipelineContinuation, []StrategyPatch, []ReleaseStrategyStateTransition)

type Pipeline []PipelineStep

func NewPipeline() *Pipeline {
	return new(Pipeline)
}

func (p *Pipeline) Enqueue(step PipelineStep) {
	*p = append(*p, step)
}

type Extra struct {
	IsLastStep   bool
	HasIncumbent bool
}

func (p *Pipeline) Process(strategy *shipper.RolloutStrategy, step int32, extra Extra, cond conditions.StrategyConditionsMap) (bool, []StrategyPatch, []ReleaseStrategyStateTransition) {
	var patches []StrategyPatch
	var trans []ReleaseStrategyStateTransition
	for _, stage := range *p {
		cont, steppatches, steptrans := stage(strategy, step, extra, cond)
		patches = append(patches, steppatches...)
		trans = append(trans, steptrans...)
		if cont == PipelineBreak {
			return false, patches, trans
		}
	}

	return true, patches, trans
}

type StrategyExecutor struct {
	strategy *shipper.RolloutStrategy
	step     int32
}

func NewStrategyExecutor(strategy *shipper.RolloutStrategy, step int32) *StrategyExecutor {
	return &StrategyExecutor{
		strategy: strategy,
		step:     step,
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

func (e *StrategyExecutor) Execute(prev, curr, succ *releaseInfo) (bool, []StrategyPatch, []ReleaseStrategyStateTransition) {
	isHead, hasTail := succ == nil, prev != nil
	hasIncumbent := prev != nil || succ != nil

	// There is no really a point in making any changes until the successor
	// has completed it's transition, therefore we're hoilding off and aborting
	// the pipeline execution. An alternative to this approach could be to make
	// an autonomous move purely based on the picture of the world. But due to
	// the limited visilibility of what's happening to the successor (as it
	// might be following it's successor) it could be that a preliminary action
	// would create more noise than help really.
	if !isHead {
		if !releaseutil.ReleaseAchievedTargetStep(succ.release) {
			return false, nil, nil
		}
	}

	var releaseStrategyConditions []shipper.ReleaseStrategyCondition
	if curr.release.Status.Strategy != nil {
		releaseStrategyConditions = curr.release.Status.Strategy.Conditions
	}
	cond := conditions.NewStrategyConditions(releaseStrategyConditions...)

	// the last step is slightly special from others: at this moment shipper
	// no longer waits for a command but marks a release as complete.
	isLastStep := int(e.step) == len(e.strategy.Steps)-1
	// The reason because isHead is not included in the extra set is mainly
	// because the pipeline is picking up 2 distinct tuples of releases
	// (curr+succ) and (prev+curr), therefore isHead is supposed to be
	// calculated by enforcers.
	extra := Extra{
		HasIncumbent: hasIncumbent,
		IsLastStep:   isLastStep,
	}

	pipeline := NewPipeline()
	if isHead {
		pipeline.Enqueue(genInstallationEnforcer(curr, nil))
	}
	pipeline.Enqueue(genCapacityEnforcer(curr, succ))
	pipeline.Enqueue(genTrafficEnforcer(curr, succ))

	if isHead {
		if hasTail {
			pipeline.Enqueue(genTrafficEnforcer(prev, curr))
			pipeline.Enqueue(genCapacityEnforcer(prev, curr))
		}
		pipeline.Enqueue(genReleaseStrategyStateEnforcer(curr, nil))
	}

	return pipeline.Process(e.strategy, e.step, extra, cond)
}

func genInstallationEnforcer(curr, succ *releaseInfo) PipelineStep {
	return func(strategy *shipper.RolloutStrategy, targetStep int32, extra Extra, cond conditions.StrategyConditionsMap) (PipelineContinuation, []StrategyPatch, []ReleaseStrategyStateTransition) {
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

			patches := make([]StrategyPatch, 0, 1)
			relPatch := buildContenderStrategyConditionsPatch(
				curr.release.Name,
				cond,
				targetStep,
				extra.IsLastStep,
				extra.HasIncumbent,
			)
			if relPatch.Alters(curr.release) {
				patches = append(patches, relPatch)
			}

			return PipelineBreak, patches, nil
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
	return func(strategy *shipper.RolloutStrategy, targetStep int32, extra Extra, cond conditions.StrategyConditionsMap) (PipelineContinuation, []StrategyPatch, []ReleaseStrategyStateTransition) {
		var condType shipper.StrategyConditionType
		var capacityWeight int32
		isHead := succ == nil

		if isHead {
			capacityWeight = strategy.Steps[targetStep].Capacity.Contender
			condType = shipper.StrategyConditionContenderAchievedCapacity
		} else {
			capacityWeight = strategy.Steps[targetStep].Capacity.Incumbent
			condType = shipper.StrategyConditionIncumbentAchievedCapacity
		}

		if achieved, newSpec, clustersNotReady := checkCapacity(curr.capacityTarget, capacityWeight); !achieved {
			klog.Infof("Release %q %s", controller.MetaKey(curr.release), "hasn't achieved capacity yet")

			var patches []StrategyPatch

			cond.SetFalse(
				condType,
				conditions.StrategyConditionsUpdate{
					Reason:             ClustersNotReady,
					Message:            fmt.Sprintf("release %q hasn't achieved capacity in clusters: %v. for more details try `kubectl describe ct %s`", curr.release.Name, clustersNotReady, curr.capacityTarget.Name),
					Step:               targetStep,
					LastTransitionTime: time.Now(),
				},
			)

			ctPatch := &CapacityTargetSpecPatch{
				NewSpec: newSpec,
				Name:    curr.release.Name,
			}
			if ctPatch.Alters(curr.capacityTarget) {
				patches = append(patches, ctPatch)
			}

			relPatch := buildContenderStrategyConditionsPatch(
				curr.release.Name,
				cond,
				targetStep,
				extra.IsLastStep,
				extra.HasIncumbent,
			)
			if relPatch.Alters(curr.release) {
				patches = append(patches, relPatch)
			}

			return PipelineBreak, patches, nil
		}

		klog.Infof("Release %q %s", controller.MetaKey(curr.release), "has achieved capacity")

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
	return func(strategy *shipper.RolloutStrategy, targetStep int32, extra Extra, cond conditions.StrategyConditionsMap) (PipelineContinuation, []StrategyPatch, []ReleaseStrategyStateTransition) {
		var condType shipper.StrategyConditionType
		var trafficWeight int32
		isHead := succ == nil

		if isHead {
			trafficWeight = strategy.Steps[targetStep].Traffic.Contender
			condType = shipper.StrategyConditionContenderAchievedTraffic
		} else {
			trafficWeight = strategy.Steps[targetStep].Traffic.Incumbent
			condType = shipper.StrategyConditionIncumbentAchievedTraffic
		}

		if achieved, newSpec, reason := checkTraffic(curr.trafficTarget, uint32(trafficWeight)); !achieved {
			klog.Infof("Release %q %s", controller.MetaKey(curr.release), "hasn't achieved traffic yet")

			var patches []StrategyPatch

			cond.SetFalse(
				condType,
				conditions.StrategyConditionsUpdate{
					Reason:             ClustersNotReady,
					Message:            fmt.Sprintf("release %q hasn't achieved traffic in clusters: %s. for more details try `kubectl describe tt %s`", curr.release.Name, reason, curr.trafficTarget.Name),
					Step:               targetStep,
					LastTransitionTime: time.Now(),
				},
			)

			ttPatch := &TrafficTargetSpecPatch{
				NewSpec: newSpec,
				Name:    curr.release.Name,
			}
			if ttPatch.Alters(curr.trafficTarget) {
				patches = append(patches, ttPatch)
			}

			relPatch := buildContenderStrategyConditionsPatch(
				curr.release.Name,
				cond,
				targetStep,
				extra.IsLastStep,
				extra.HasIncumbent,
			)
			if relPatch.Alters(curr.release) {
				patches = append(patches, relPatch)
			}

			return PipelineBreak, patches, nil
		}

		klog.Infof("Release %q %s", controller.MetaKey(curr.release), "has achieved traffic")

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
	return func(strategy *shipper.RolloutStrategy, targetStep int32, extra Extra, cond conditions.StrategyConditionsMap) (PipelineContinuation, []StrategyPatch, []ReleaseStrategyStateTransition) {
		var patches []StrategyPatch
		var releaseStrategyStateTransitions []ReleaseStrategyStateTransition

		relStatus := curr.release.Status.DeepCopy()

		newReleaseStrategyState := cond.AsReleaseStrategyState(
			targetStep,
			extra.HasIncumbent,
			extra.IsLastStep,
		)

		var oldReleaseStrategyState shipper.ReleaseStrategyState
		if relStatus.Strategy != nil {
			oldReleaseStrategyState = relStatus.Strategy.State
		}

		releaseStrategyStateTransitions =
			getReleaseStrategyStateTransitions(
				oldReleaseStrategyState,
				newReleaseStrategyState,
				releaseStrategyStateTransitions)

		relPatch := buildContenderStrategyConditionsPatch(
			curr.release.Name,
			cond,
			targetStep,
			extra.IsLastStep,
			extra.HasIncumbent,
		)

		if relPatch.Alters(curr.release) {
			patches = append(patches, relPatch)
		}

		return PipelineContinue, patches, releaseStrategyStateTransitions
	}
}

func buildContenderStrategyConditionsPatch(
	name string,
	cond conditions.StrategyConditionsMap,
	step int32,
	isLastStep bool,
	hasIncumbent bool,
) StrategyPatch {
	newStrategyStatus := &shipper.ReleaseStrategyStatus{
		Conditions: cond.AsReleaseStrategyConditions(),
		State:      cond.AsReleaseStrategyState(step, hasIncumbent, isLastStep),
	}
	return &ReleaseStrategyStatusPatch{
		NewStrategyStatus: newStrategyStatus,
		Name:              name,
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
