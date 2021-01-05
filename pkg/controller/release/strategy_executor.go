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

type context struct {
	release    *shipper.Release
	step       int32
	isHead     bool
	isLastStep bool
	hasTail    bool
}

func (ctx *context) Copy() *context {
	return &context{
		release:    ctx.release,
		step:       ctx.step,
		isHead:     ctx.isHead,
		isLastStep: ctx.isLastStep,
		hasTail:    ctx.hasTail,
	}
}

type PipelineStep func(shipper.RolloutStrategyStep, conditions.StrategyConditionsMap) (PipelineContinuation, []StrategyPatch, []ReleaseStrategyStateTransition)

type Pipeline []PipelineStep

func NewPipeline() *Pipeline {
	return new(Pipeline)
}

func (p *Pipeline) Enqueue(step PipelineStep) {
	*p = append(*p, step)
}

func (p *Pipeline) Process(strategyStep shipper.RolloutStrategyStep, cond conditions.StrategyConditionsMap) (bool, []StrategyPatch, []ReleaseStrategyStateTransition) {
	var patches []StrategyPatch
	var trans []ReleaseStrategyStateTransition
	complete := true
	for _, stage := range *p {
		cont, steppatches, steptrans := stage(strategyStep, cond)
		patches = append(patches, steppatches...)
		trans = append(trans, steptrans...)
		if cont == PipelineBreak {
			complete = false
			break
		}
	}

	return complete, patches, trans
}

type StrategyExecutor struct {
	strategy    *shipper.RolloutStrategy
	step        int32
	progressing bool
}

func NewStrategyExecutor(strategy *shipper.RolloutStrategy, step int32, progressing bool) *StrategyExecutor {
	return &StrategyExecutor{
		strategy:    strategy,
		step:        step,
		progressing: progressing,
	}
}

// copyStrategyConditions makes a shallow copy of the original condition
// collection and based on the value of the keepIncumbent flag either keeps or
// filters out conditions that descibe incumbent's state.
func copyStrategyConditions(conditions []shipper.ReleaseStrategyCondition, keepIncumbent bool) []shipper.ReleaseStrategyCondition {
	res := make([]shipper.ReleaseStrategyCondition, 0, len(conditions))
	for _, cond := range conditions {
		if t := cond.Type; !keepIncumbent &&
			(t == shipper.StrategyConditionIncumbentAchievedTraffic ||
				t == shipper.StrategyConditionIncumbentAchievedCapacity) {
			continue
		}
		res = append(res, cond)
	}
	return res
}

func (e *StrategyExecutor) Execute(prev, curr, succ *releaseInfo) (bool, []StrategyPatch, []ReleaseStrategyStateTransition) {
	isHead := succ == nil

	// hasTail is a flag indicating that the executor should look behind. The
	// deal is that the executor normaly looks ahead. In the case of contender,
	// it's completeness state depends on the incumbent state, tehrefore it's
	// the only case when we look behind.
	hasTail := isHead && prev != nil

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
		// As it's been mentioned before, we only look behind if it's the
		// contender. StrategyExecutor should not state a fact if it has never
		// observed an evidence of this statement, therefore for non-contender
		// cases we can't really say anything about incumbent's state (in fact,
		// we are not  interested in it at all). Therefore, we're dropping
		// incumbent-related conditions from the initial condition collection so
		// to never report an unchecked state.
		releaseStrategyConditions = copyStrategyConditions(
			curr.release.Status.Strategy.Conditions,
			hasTail,
		)
	}
	cond := conditions.NewStrategyConditions(releaseStrategyConditions...)

	// the last step is slightly special from others: at this moment shipper
	// is no longer waiting for a command but marks a release as complete.
	isLastStep := int(e.step) == len(e.strategy.Steps)-1

	ctx := &context{
		release:    curr.release,
		hasTail:    hasTail,
		isLastStep: isLastStep,
		step:       e.step,
		isHead:     isHead,
	}

	pipeline := NewPipeline()
	pipeline.Enqueue(genInstallationEnforcer(ctx, curr, succ))

	if e.progressing {
		// release is progressing (achieved step <= target step):
			// increase capacity for current release
			// increase traffic for current release
			// reduce traffic for previous release
			// reduce capacity for previous release
		if isHead {
			pipeline.Enqueue(genCapacityEnforcer(ctx, curr, succ))
			pipeline.Enqueue(genTrafficEnforcer(ctx, curr, succ))
			if hasTail {
				// This is the moment where a contender is performing a look-behind.
				// Incumbent's context is completely identical to it's successor
				// except that it's not the head of the chain anymore.
				prevctx := ctx.Copy()
				prevctx.isHead = false
				pipeline.Enqueue(genTrafficEnforcer(prevctx, prev, curr))
				pipeline.Enqueue(genCapacityEnforcer(prevctx, prev, curr))
			}
		} else {
			pipeline.Enqueue(genTrafficEnforcer(ctx, curr, succ))
			pipeline.Enqueue(genCapacityEnforcer(ctx, curr, succ))
		}
	} else {
		// release is not progressing (achieved step > target step):
			// increase capacity for previous release
			// increase traffic for previous release
			// reduce traffic for current release
			// reduce capacity for current release
		if isHead {
			if hasTail {
				prevctx := ctx.Copy()
				prevctx.isHead = false
				pipeline.Enqueue(genCapacityEnforcer(prevctx, prev, curr))
				pipeline.Enqueue(genTrafficEnforcer(prevctx, prev, curr))
			}
			pipeline.Enqueue(genTrafficEnforcer(ctx, curr, succ))
			pipeline.Enqueue(genCapacityEnforcer(ctx, curr, succ))
		} else {
			pipeline.Enqueue(genTrafficEnforcer(ctx, curr, succ))
			pipeline.Enqueue(genCapacityEnforcer(ctx, curr, succ))
		}
	}

	pipeline.Enqueue(genReleaseStrategyStateEnforcer(ctx, curr, succ))

	strategyStep := e.strategy.Steps[e.step]

	return pipeline.Process(strategyStep, cond)
}

func genInstallationEnforcer(ctx *context, curr, succ *releaseInfo) PipelineStep {
	return func(strategyStep shipper.RolloutStrategyStep, cond conditions.StrategyConditionsMap) (PipelineContinuation, []StrategyPatch, []ReleaseStrategyStateTransition) {
		if ready, clusters := checkInstallation(curr.installationTarget); !ready {
			cond.SetFalse(
				shipper.StrategyConditionContenderAchievedInstallation,
				conditions.StrategyConditionsUpdate{
					Reason:             ClustersNotReady,
					Step:               ctx.step,
					Message:            fmt.Sprintf("clusters pending installation: %v. for more details try `kubectl describe it %s`", clusters, curr.installationTarget.GetName()),
					LastTransitionTime: time.Now(),
				},
			)

			patches := make([]StrategyPatch, 0, 1)
			relPatch := buildContenderStrategyConditionsPatch(ctx, cond)
			if relPatch.Alters(ctx.release) {
				patches = append(patches, relPatch)
			}

			return PipelineBreak, patches, nil
		}

		cond.SetTrue(
			shipper.StrategyConditionContenderAchievedInstallation,
			conditions.StrategyConditionsUpdate{
				LastTransitionTime: time.Now(),
				Step:               ctx.step,
				Message:            "",
				Reason:             "",
			},
		)

		return PipelineContinue, nil, nil
	}
}

func genCapacityEnforcer(ctx *context, curr, succ *releaseInfo) PipelineStep {
	return func(strategyStep shipper.RolloutStrategyStep, cond conditions.StrategyConditionsMap) (PipelineContinuation, []StrategyPatch, []ReleaseStrategyStateTransition) {
		var condType shipper.StrategyConditionType
		var capacityWeight int32
		isHead := succ == nil
		isInitiator := releasesIdentical(ctx.release, curr.release)

		if isInitiator {
			condType = shipper.StrategyConditionContenderAchievedCapacity
		} else {
			condType = shipper.StrategyConditionIncumbentAchievedCapacity
		}
		if isHead {
			capacityWeight = strategyStep.Capacity.Contender
		} else {
			capacityWeight = strategyStep.Capacity.Incumbent
		}

		if achieved, newSpec, clustersNotReady := checkCapacity(curr.capacityTarget, capacityWeight); !achieved {
			klog.Infof("Release %q %s", controller.MetaKey(curr.release), "hasn't achieved capacity yet")

			patches := make([]StrategyPatch, 0, 2)

			cond.SetFalse(
				condType,
				conditions.StrategyConditionsUpdate{
					Reason:             ClustersNotReady,
					Message:            fmt.Sprintf("release %q hasn't achieved capacity in clusters: %v. for more details try `kubectl describe ct %s`", curr.release.GetName(), clustersNotReady, curr.capacityTarget.GetName()),
					Step:               ctx.step,
					LastTransitionTime: time.Now(),
				},
			)

			ctPatch := &CapacityTargetSpecPatch{
				NewSpec: newSpec,
				Name:    curr.release.GetName(),
			}
			if ctPatch.Alters(curr.capacityTarget) {
				patches = append(patches, ctPatch)
			}

			relPatch := buildContenderStrategyConditionsPatch(ctx, cond)
			if relPatch.Alters(ctx.release) {
				patches = append(patches, relPatch)
			}

			return PipelineBreak, patches, nil
		}

		klog.Infof("Release %q %s", controller.MetaKey(curr.release), "has achieved capacity")

		cond.SetTrue(
			condType,
			conditions.StrategyConditionsUpdate{
				Step:               ctx.step,
				LastTransitionTime: time.Now(),
				Message:            "",
				Reason:             "",
			},
		)

		return PipelineContinue, nil, nil
	}
}

func genTrafficEnforcer(ctx *context, curr, succ *releaseInfo) PipelineStep {
	return func(strategyStep shipper.RolloutStrategyStep, cond conditions.StrategyConditionsMap) (PipelineContinuation, []StrategyPatch, []ReleaseStrategyStateTransition) {
		var condType shipper.StrategyConditionType
		var trafficWeight int32
		isHead := succ == nil
		isInitiator := releasesIdentical(ctx.release, curr.release)

		if isInitiator {
			condType = shipper.StrategyConditionContenderAchievedTraffic
		} else {
			condType = shipper.StrategyConditionIncumbentAchievedTraffic
		}
		if isHead {
			trafficWeight = strategyStep.Traffic.Contender
		} else {
			trafficWeight = strategyStep.Traffic.Incumbent
		}

		if achieved, newSpec, reason := checkTraffic(curr.trafficTarget, uint32(trafficWeight)); !achieved {
			klog.Infof("Release %q %s", controller.MetaKey(curr.release), "hasn't achieved traffic yet")

			patches := make([]StrategyPatch, 0, 2)

			cond.SetFalse(
				condType,
				conditions.StrategyConditionsUpdate{
					Reason:             ClustersNotReady,
					Message:            fmt.Sprintf("release %q hasn't achieved traffic in clusters: %s. for more details try `kubectl describe tt %s`", curr.release.GetName(), reason, curr.trafficTarget.GetName()),
					Step:               ctx.step,
					LastTransitionTime: time.Now(),
				},
			)

			ttPatch := &TrafficTargetSpecPatch{
				NewSpec: newSpec,
				Name:    curr.release.GetName(),
			}
			if ttPatch.Alters(curr.trafficTarget) {
				patches = append(patches, ttPatch)
			}

			relPatch := buildContenderStrategyConditionsPatch(ctx, cond)
			if relPatch.Alters(ctx.release) {
				patches = append(patches, relPatch)
			}

			return PipelineBreak, patches, nil
		}

		klog.Infof("Release %q %s", controller.MetaKey(curr.release), "has achieved traffic")

		cond.SetTrue(
			condType,
			conditions.StrategyConditionsUpdate{
				Step:               ctx.step,
				LastTransitionTime: time.Now(),
				Message:            "",
				Reason:             "",
			},
		)

		return PipelineContinue, nil, nil
	}
}

func genReleaseStrategyStateEnforcer(ctx *context, curr, succ *releaseInfo) PipelineStep {
	return func(strategyStep shipper.RolloutStrategyStep, cond conditions.StrategyConditionsMap) (PipelineContinuation, []StrategyPatch, []ReleaseStrategyStateTransition) {
		var releaseStrategyStateTransitions []ReleaseStrategyStateTransition
		patches := make([]StrategyPatch, 0, 1)

		relStatus := curr.release.Status.DeepCopy()

		newReleaseStrategyState := cond.AsReleaseStrategyState(
			ctx.step,
			ctx.hasTail,
			ctx.isLastStep,
			ctx.isHead,
		)

		var oldReleaseStrategyState shipper.ReleaseStrategyState
		if relStatus.Strategy != nil {
			oldReleaseStrategyState = relStatus.Strategy.State
		}

		relPatch := buildContenderStrategyConditionsPatch(ctx, cond)
		if relPatch.Alters(ctx.release) {
			patches = append(patches, relPatch)
			releaseStrategyStateTransitions =
				getReleaseStrategyStateTransitions(
					oldReleaseStrategyState,
					newReleaseStrategyState,
					releaseStrategyStateTransitions)
		}

		return PipelineContinue, patches, releaseStrategyStateTransitions
	}
}

func buildContenderStrategyConditionsPatch(
	ctx *context,
	cond conditions.StrategyConditionsMap,
) StrategyPatch {
	newStrategyStatus := &shipper.ReleaseStrategyStatus{
		Conditions: cond.AsReleaseStrategyConditions(),
		State: cond.AsReleaseStrategyState(
			ctx.step,
			ctx.hasTail,
			ctx.isLastStep,
			ctx.isHead,
		),
	}
	return &ReleaseStrategyStatusPatch{
		NewStrategyStatus: newStrategyStatus,
		Name:              ctx.release.GetName(),
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
		stateTransitions = append(stateTransitions, ReleaseStrategyStateTransition{State: "WaitingForTraffic", New: newState.WaitingForTraffic, Previous: valueOrUnknown(oldState.WaitingForTraffic)})
	}
	return stateTransitions
}

func valueOrUnknown(v shipper.StrategyState) shipper.StrategyState {
	if len(v) < 1 {
		v = shipper.StrategyStateUnknown
	}
	return v
}

// This method is a super basic helper and should not be re-used as a reliable
// release similarity checker
func releasesIdentical(r1, r2 *shipper.Release) bool {
	if r1 == nil || r2 == nil {
		return r1 == r2
	}
	return r1.GetNamespace() == r2.GetNamespace() &&
		r1.GetName() == r2.GetName()
}
