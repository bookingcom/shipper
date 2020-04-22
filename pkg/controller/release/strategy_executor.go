package release

import (
	"fmt"
	"time"

	"k8s.io/klog"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	"github.com/bookingcom/shipper/pkg/util/conditions"
	objectutil "github.com/bookingcom/shipper/pkg/util/object"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

type PipelineContinuation bool

const (
	PipelineBreak    PipelineContinuation = false
	PipelineContinue                      = true
)

type context struct {
	release *shipper.Release
	step    int32
	isHead  bool
}

func (ctx *context) Copy() *context {
	return &context{
		release: ctx.release,
		step:    ctx.step,
		isHead:  ctx.isHead,
	}
}

type PipelineStep func(shipper.RolloutStrategyStep, conditions.StrategyConditionsMap) (PipelineContinuation, []StrategyPatch)

type Pipeline []PipelineStep

func NewPipeline() *Pipeline {
	return new(Pipeline)
}

func (p *Pipeline) Enqueue(step PipelineStep) {
	*p = append(*p, step)
}

func (p *Pipeline) Process(strategyStep shipper.RolloutStrategyStep, cond conditions.StrategyConditionsMap) (conditions.StrategyConditionsMap, []StrategyPatch) {
	var patches []StrategyPatch
	for _, stage := range *p {
		cont, steppatches := stage(strategyStep, cond)
		patches = append(patches, steppatches...)
		if cont == PipelineBreak {
			break
		}
	}

	return cond, patches
}

type StrategyExecutor struct {
	strategy *shipper.RolloutStrategy
	step     int32
}

func NewStrategyExecutor(strategy *shipper.RolloutStrategy, step int32) (*StrategyExecutor, error) {
	if step >= int32(len(strategy.Steps)) {
		return nil, shippererrors.NewUnrecoverableError(
			fmt.Errorf("no step %d in strategy", step))
	}

	return &StrategyExecutor{
		strategy: strategy,
		step:     step,
	}, nil
}

func (e *StrategyExecutor) Execute(prev, curr, succ *releaseInfo) (conditions.StrategyConditionsMap, []StrategyPatch) {
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
			return nil, nil
		}
	}

	ctx := &context{
		release: curr.release,
		step:    e.step,
		isHead:  isHead,
	}

	pipeline := NewPipeline()
	pipeline.Enqueue(genInstallationEnforcer(ctx, curr, succ))

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

	var releaseStrategyConditions []shipper.ReleaseStrategyCondition
	cond := conditions.NewStrategyConditions(releaseStrategyConditions...)
	strategyStep := e.strategy.Steps[e.step]

	return pipeline.Process(strategyStep, cond)
}

func genInstallationEnforcer(ctx *context, curr, succ *releaseInfo) PipelineStep {
	return func(strategyStep shipper.RolloutStrategyStep, cond conditions.StrategyConditionsMap) (PipelineContinuation, []StrategyPatch) {
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

			return PipelineBreak, nil
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

		return PipelineContinue, nil
	}
}

func genCapacityEnforcer(ctx *context, curr, succ *releaseInfo) PipelineStep {
	return func(strategyStep shipper.RolloutStrategyStep, cond conditions.StrategyConditionsMap) (PipelineContinuation, []StrategyPatch) {
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
			klog.Infof("Release %q %s", objectutil.MetaKey(curr.release), "hasn't achieved capacity yet")

			cond.SetFalse(
				condType,
				conditions.StrategyConditionsUpdate{
					Reason:             ClustersNotReady,
					Message:            fmt.Sprintf("release %q hasn't achieved capacity in clusters: %v. for more details try `kubectl describe ct %s`", curr.release.GetName(), clustersNotReady, curr.capacityTarget.GetName()),
					Step:               ctx.step,
					LastTransitionTime: time.Now(),
				},
			)

			patches := make([]StrategyPatch, 0, 1)
			ctPatch := &CapacityTargetSpecPatch{
				NewSpec: newSpec,
				Name:    curr.release.GetName(),
			}
			if ctPatch.Alters(curr.capacityTarget) {
				patches = append(patches, ctPatch)
			}

			return PipelineBreak, patches
		}

		klog.Infof("Release %q %s", objectutil.MetaKey(curr.release), "has achieved capacity")

		cond.SetTrue(
			condType,
			conditions.StrategyConditionsUpdate{
				Step:               ctx.step,
				LastTransitionTime: time.Now(),
				Message:            "",
				Reason:             "",
			},
		)

		return PipelineContinue, nil
	}
}

func genTrafficEnforcer(ctx *context, curr, succ *releaseInfo) PipelineStep {
	return func(strategyStep shipper.RolloutStrategyStep, cond conditions.StrategyConditionsMap) (PipelineContinuation, []StrategyPatch) {
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
			klog.Infof("Release %q %s", objectutil.MetaKey(curr.release), "hasn't achieved traffic yet")

			cond.SetFalse(
				condType,
				conditions.StrategyConditionsUpdate{
					Reason:             ClustersNotReady,
					Message:            fmt.Sprintf("release %q hasn't achieved traffic in clusters: %s. for more details try `kubectl describe tt %s`", curr.release.GetName(), reason, curr.trafficTarget.GetName()),
					Step:               ctx.step,
					LastTransitionTime: time.Now(),
				},
			)

			patches := make([]StrategyPatch, 0, 1)
			ttPatch := &TrafficTargetSpecPatch{
				NewSpec: newSpec,
				Name:    curr.release.GetName(),
			}
			if ttPatch.Alters(curr.trafficTarget) {
				patches = append(patches, ttPatch)
			}

			return PipelineBreak, patches
		}

		klog.Infof("Release %q %s", objectutil.MetaKey(curr.release), "has achieved traffic")

		cond.SetTrue(
			condType,
			conditions.StrategyConditionsUpdate{
				Step:               ctx.step,
				LastTransitionTime: time.Now(),
				Message:            "",
				Reason:             "",
			},
		)

		return PipelineContinue, nil
	}
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
