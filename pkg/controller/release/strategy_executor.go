package release

import (
	"fmt"
	"math"
	"time"

	"k8s.io/klog"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/controller"
	"github.com/bookingcom/shipper/pkg/util/conditions"
	"github.com/bookingcom/shipper/pkg/util/replicas"
	targetutil "github.com/bookingcom/shipper/pkg/util/target"
)

type PipelineContinuation bool

const (
	PipelineBreak    PipelineContinuation = false
	PipelineContinue                      = true
)

type PipelineStep func(*shipper.RolloutStrategy, int32, int32, Extra, conditions.StrategyConditionsMap) (PipelineContinuation, []StrategyPatch, []ReleaseStrategyStateTransition, bool)

type Pipeline []PipelineStep

func NewPipeline() *Pipeline {
	return new(Pipeline)
}

func (p *Pipeline) Enqueue(step PipelineStep) {
	*p = append(*p, step)
}

type Extra struct {
	IsLastStep  bool
	HasTail     bool
	Initiator   *shipper.Release
	Progressing bool
}

func (p *Pipeline) Process(strategy *shipper.RolloutStrategy, step int32, virtualStep int32, extra Extra, cond conditions.StrategyConditionsMap) (bool, []StrategyPatch, []ReleaseStrategyStateTransition, bool) {
	var patches []StrategyPatch
	var trans []ReleaseStrategyStateTransition
	complete := true
	completeVirtualSteps := false

	for _, stage := range *p {
		cont, steppatches, steptrans, isLastVirtualStep := stage(strategy, step, virtualStep, extra, cond)
		patches = append(patches, steppatches...)
		trans = append(trans, steptrans...)
		if isLastVirtualStep {
			klog.Infof("HILLA LAST STEPPPP!!! COMPLETE?!?! %v", complete)
			completeVirtualSteps = true
		}
		if cont == PipelineBreak {
			complete = false
			break
		}
	}

	return complete, patches, trans, completeVirtualSteps
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

func (e *StrategyExecutor) Execute(prev, curr, succ *releaseInfo, progressing bool) (bool, []StrategyPatch, []ReleaseStrategyStateTransition, bool) {
	isHead, hasTail := succ == nil, prev != nil

	// There is no really a point in making any changes until the successor
	// has completed it's transition, therefore we're hoilding off and aborting
	// the pipeline execution. An alternative to this approach could be to make
	// an autonomous move purely based on the picture of the world. But due to
	// the limited visilibility of what's happening to the successor (as it
	// might be following it's successor) it could be that a preliminary action
	// would create more noise than help really.
	if !isHead {
		if !isRelReady(succ) {
			//if !releaseutil.ReleaseAchievedTargetStep(succ.release) {
			klog.Infof("HILLA fuck")
			//curr.release.Status.AchievedSubStepp = 0
			return false, nil, nil, false
		}
	}

	// This pre-fill is super important, otherwise conditions will be sorted
	// in a wrong order and corresponding patches will override wrong
	// conditions.
	var releaseStrategyConditions []shipper.ReleaseStrategyCondition
	if curr.release.Status.Strategy != nil {
		releaseStrategyConditions = curr.release.Status.Strategy.Conditions
	}
	cond := conditions.NewStrategyConditions(releaseStrategyConditions...)

	// the last step is slightly special from others: at this moment shipper
	// is no longer waiting for a command but marks a release as complete.
	isLastStep := int(e.step) == len(e.strategy.Steps)-1
	// The reason because isHead is not included in the extra set is mainly
	// because the pipeline is picking up 2 distinct tuples of releases
	// (curr+succ) and (prev+curr), therefore isHead is supposed to be
	// calculated by enforcers.
	extra := Extra{
		Initiator:   curr.release,
		IsLastStep:  isLastStep,
		HasTail:     hasTail,
		Progressing: progressing,
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

	virtualStep := e.getNextSubStep(prev, curr, succ, progressing)
	return pipeline.Process(e.strategy, e.step, virtualStep, extra, cond)
}

func (e *StrategyExecutor) getNextSubStep(prev, curr, succ *releaseInfo, progressing bool) int32 {
	// TODO HILLA find a way for curr and prev to wait for each other
	isHead := succ == nil
	var virtualStep int32 = 0
	if curr.release.Status.AchievedStep != nil {
		if isHead {
			//ct := curr.capacityTarget
			//achievedStep := e.step
			currentStep, achieved := e.virtualStep(curr, progressing, isHead)
			incumbentStep, incumbentAchieved := e.virtualStep(prev, !progressing, !isHead)
			isReady := isRelReady(curr) && isRelReady(prev)
			if isReady && achieved && incumbentAchieved && currentStep == incumbentStep {
				virtualStep = currentStep + 1
			} else {
				virtualStep = currentStep
			}

			klog.Infof("HILLA CONTENDER NEXT Virtual STEP IS %d, incumbent is in step %d, did it achieve it? %v", virtualStep, incumbentStep, incumbentAchieved)
		} else {
			//must match successor!
			//virtualStep = succ.release.Status.AchievedSubStepp

			contenderStep, achieved := e.virtualStep(succ, progressing, isHead)
			incumbentStep, incumbentAchieved := e.virtualStep(curr, !progressing, !isHead)
			isReady := isRelReady(succ) && isRelReady(curr)
			if isReady && achieved && incumbentAchieved && contenderStep == incumbentStep {
				virtualStep = contenderStep + 1
			} else {
				virtualStep = contenderStep
			}

			klog.Infof("HILLA INCUMBENT NEXT Virtual STEP IS %d", virtualStep)
		}
	} else {
		klog.Infof("HILLA NO ACHIEVED STEEEPPPPP FFFUUUCCCKKKKK")
	}
	return virtualStep
}

func (e *StrategyExecutor) virtualStep(rel *releaseInfo, progressing, isHead bool) (int32, bool) {
	ct := rel.capacityTarget
	achievedStep := e.step
	achievedStep = rel.release.Status.AchievedStep.Step
	baseCapacity := e.strategy.Steps[achievedStep].Capacity.Incumbent
	if isHead {
		baseCapacity = e.strategy.Steps[achievedStep].Capacity.Contender
	}
	baseCapacityAchieved := replicas.CalculateDesiredReplicaCount(uint(getReleaseReplicaCount(rel)), float64(baseCapacity))
	surge, err := getMaxSurge(rel, e.strategy)
	if err != nil {
		klog.Infof("HILLA got this error %s", err.Error())
		//return false, nil, nil, false
	}
	currentCapacity := baseCapacityAchieved
	for _, spec := range ct.Spec.Clusters {
		capacity := replicas.CalculateDesiredReplicaCount(uint(spec.TotalReplicaCount), float64(spec.Percent))
		klog.Infof("HILLA rel %s, capacity in spec %d, capacity in pods %d", rel.release.Name, spec.Percent, capacity)
		// TODO HILLA This is only for progressing contender, make sure to check for not progressing contender
		if progressing && (capacity > currentCapacity) {
			currentCapacity = capacity
		} else if !progressing && (capacity < currentCapacity) {
			currentCapacity = capacity
		}
	}
	diffAchieved := math.Abs(float64(int(baseCapacityAchieved) - int(currentCapacity)))
	currentStep := int32(math.Ceil(diffAchieved / float64(surge)))
	klog.Infof("HILLA release %s, base %d pods, achieved %d pods, diff %.1f, current step is %d, division result is %.1f", rel.release.Name, baseCapacityAchieved, currentCapacity, diffAchieved, currentStep, diffAchieved/float64(surge))
	return currentStep, diffAchieved == 0
}

func genInstallationEnforcer(curr, succ *releaseInfo) PipelineStep {
	return func(strategy *shipper.RolloutStrategy, targetStep int32, virtualStep int32, extra Extra, cond conditions.StrategyConditionsMap) (PipelineContinuation, []StrategyPatch, []ReleaseStrategyStateTransition, bool) {
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
				extra.Initiator.Name,
				cond,
				targetStep,
				extra.IsLastStep,
				extra.HasTail,
			)
			if relPatch.Alters(extra.Initiator) {
				patches = append(patches, relPatch)
			}

			return PipelineBreak, patches, nil, false
		}

		cond.SetTrue(
			shipper.StrategyConditionContenderAchievedInstallation,
			conditions.StrategyConditionsUpdate{
				LastTransitionTime: time.Now(),
				Step:               targetStep,
			},
		)

		return PipelineContinue, nil, nil, false
	}
}

func genCapacityEnforcer(curr, succ *releaseInfo) PipelineStep {
	return func(strategy *shipper.RolloutStrategy, targetStep int32, virtualStep int32, extra Extra, cond conditions.StrategyConditionsMap) (PipelineContinuation, []StrategyPatch, []ReleaseStrategyStateTransition, bool) {
		var condType shipper.StrategyConditionType
		var capacityWeight int32
		var baseWeight int32
		var achievedStep int32
		isLastVirtualStep := false
		isHead := succ == nil
		isInitiator := releasesIdentical(extra.Initiator, curr.release)
		if curr.release.Status.AchievedStep != nil {
			// TODO HILLA fix this
			achievedStep = curr.release.Status.AchievedStep.Step
		} else {
			achievedStep = targetStep
		}
		//klog.Infof("HILLA achievedStep %d", achievedStep)
		if isInitiator {
			condType = shipper.StrategyConditionContenderAchievedCapacity
		} else {
			condType = shipper.StrategyConditionIncumbentAchievedCapacity
		}
		if isHead {
			capacityWeight = strategy.Steps[targetStep].Capacity.Contender
			baseWeight = strategy.Steps[achievedStep].Capacity.Contender
		} else {
			capacityWeight = strategy.Steps[targetStep].Capacity.Incumbent
			baseWeight = strategy.Steps[achievedStep].Capacity.Incumbent
		}

		//klog.Infof("HILLA processing release %s virtual step %d", curr.release.Name, virtualStep)
		surge, err := getMaxSurge(curr, strategy)
		if err != nil {
			klog.Infof("HILLA got this error %s", err)
		}
		progress := extra.Progressing

		//klog.Infof("HILLA is progressing %v, is initiator %v", progress, isInitiator)
		stepCapacity := make(map[string]int32)
		ct := curr.capacityTarget
		for _, spec := range ct.Spec.Clusters {
			desiredCapacity := replicas.CalculateDesiredReplicaCount(uint(spec.TotalReplicaCount), float64(capacityWeight))
			baseCapacity := replicas.CalculateDesiredReplicaCount(uint(spec.TotalReplicaCount), float64(baseWeight))
			if baseCapacity == desiredCapacity {
				stepCapacity[spec.Name] = capacityWeight
				continue

			}
			// surge is amount of pods. now we need to calculate the amount of pods we currently have
			// ct.spec.clusters[*].percent is how much we should have by now.
			// this value can be different for each cluster :facepalm: (though it shouldn't)
			//existingCapacity := replicas.CalculateDesiredReplicaCount(uint(spec.TotalReplicaCount), float64(spec.Percent))
			var newCapacity float64

			var possibleCapacity float64
			//if progress {
			if (progress && isHead) || (!progress && !isHead) {
				possibleCapacity = float64(int32(baseCapacity) + virtualStep*int32(surge))
				newCapacity = math.Min(possibleCapacity, float64(desiredCapacity))
				//newCapacity = math.Min(float64(existingCapacity), float64(desiredCapacity))
			} else {
				possibleCapacity = float64(int32(baseCapacity) - virtualStep*int32(surge))
				newCapacity = math.Max(possibleCapacity, float64(desiredCapacity))
				//newCapacity = math.Min(float64(existingCapacity), float64(desiredCapacity))
			}
			//if baseCapacity > desiredCapacity {
			//	possibleCapacity = float64(int32(baseCapacity) - virtualStep*int32(surge))
			//	newCapacity = math.Max(possibleCapacity, float64(desiredCapacity))
			//} else if baseCapacity < desiredCapacity {
			//	possibleCapacity = float64(int32(baseCapacity) + virtualStep*int32(surge))
			//	newCapacity = math.Min(possibleCapacity, float64(desiredCapacity))
			//} else {
			//	newCapacity = float64(desiredCapacity)
			//}

			if newCapacity > float64(spec.TotalReplicaCount) {
				newCapacity = float64(spec.TotalReplicaCount)
			}
			if newCapacity < 0.0 {
				newCapacity = 0.0
			}
			//klog.Infof("HILLA existingCapacity %d, surge %s, new capacity is %.1f pods, total replica count is %d pods", existingCapacity, surge, newCapacity, spec.TotalReplicaCount)
			// newCapacity is number of pods, now we translate it to percent
			newPercent := int32(math.Ceil(newCapacity / float64(getReleaseReplicaCount(curr)) * 100.0))
			// TODO HILLA fix this, this is wrong
			if ((progress && isHead) || (!progress && !isHead)) && newPercent > capacityWeight {
				klog.Infof("HILAAAAAAA WWHHHYYYY")
				newPercent = capacityWeight
			}
			klog.Infof("HILLA virtual step base %d,  capacity %d, desired Capacity %d, possible Capacity %.1f, new Capacity %.1f", virtualStep, baseCapacity, desiredCapacity, possibleCapacity, newCapacity)
			stepCapacity[spec.Name] = newPercent
			if isHead && newPercent == capacityWeight {
				isLastVirtualStep = true
			}
			//klog.Infof("HILLA new capacity is %d percent", stepCapacity[spec.Name])
		}

		// check if ct is ready! If it's not ready - leave it alone and let it get ready!!!
		if !isRelReady(curr) {
			return PipelineBreak, nil, nil, false
		}

		//if achieved, newSpec, clustersNotReady := checkCapacity(curr.capacityTarget, capacityWeight); !achieved {
		if achieved, newSpec, clustersNotReady := checkCapacity(curr.capacityTarget, stepCapacity); !achieved {
			klog.Infof("Release %q %s", controller.MetaKey(curr.release), "hasn't achieved capacity yet")

			patches := make([]StrategyPatch, 0, 2)

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
				extra.Initiator.Name,
				cond,
				targetStep,
				extra.IsLastStep,
				extra.HasTail,
			)
			if relPatch.Alters(extra.Initiator) {
				patches = append(patches, relPatch)
			}

			return PipelineBreak, patches, nil, false
			//} else if curr.release.Status.AchievedSubStepp.ForStep != targetStep {
		}

		klog.Infof("HILLA Release %q updating rel info virtual step %d step %d", curr.release.Name, virtualStep, targetStep)
		curr.release.Status.AchievedSubStepp = &shipper.AchievedSubStep{SubStep: virtualStep, Step: targetStep}
		//if curr.release.Status.AchievedSubStepp != nil {
		//	curr.release.Status.AchievedSubStepp.SubStep = virtualStep
		//} else {
		//}

		if !isLastVirtualStep {
			klog.Infof("HILLA Release %q did not finish virtual steps", curr.release.Name)

			return PipelineContinue, nil, nil, isLastVirtualStep
		}

		klog.Infof("HILLA Release %q %s", controller.MetaKey(curr.release), "has achieved capacity")

		cond.SetTrue(
			condType,
			conditions.StrategyConditionsUpdate{
				Step:               targetStep,
				LastTransitionTime: time.Now(),
				Reason:             "",
				Message:            "",
			},
		)

		return PipelineContinue, nil, nil, isLastVirtualStep
	}
}

func getMaxSurge(rel *releaseInfo, strategy *shipper.RolloutStrategy) (uint, error) {
	return 2, nil
	//totalReplicaCount := getReleaseReplicaCount(rel)
	//ruPointer := strategy.RollingUpdate
	//var maxSurgeValue intstrutil.IntOrString
	//if ruPointer != nil {
	//	maxSurgeValue = ruPointer.MaxSurge
	//} else {
	//	maxSurgeValue = intstrutil.FromString("100%")
	//}
	//
	//surge, err := intstrutil.GetValueFromIntOrPercent(intstrutil.ValueOrDefault(&maxSurgeValue, intstrutil.FromString("100%")), int(totalReplicaCount), true)
	//return uint(surge), err
}

func genTrafficEnforcer(curr, succ *releaseInfo) PipelineStep {
	return func(strategy *shipper.RolloutStrategy, targetStep int32, virtualStep int32, extra Extra, cond conditions.StrategyConditionsMap) (PipelineContinuation, []StrategyPatch, []ReleaseStrategyStateTransition, bool) {
		var condType shipper.StrategyConditionType
		var trafficWeight int32
		isHead := succ == nil
		isInitiator := releasesIdentical(extra.Initiator, curr.release)
		isLastVirtualStep := false

		if isInitiator {
			condType = shipper.StrategyConditionContenderAchievedTraffic
		} else {
			condType = shipper.StrategyConditionIncumbentAchievedTraffic
		}
		if isHead {
			trafficWeight = strategy.Steps[targetStep].Traffic.Contender
		} else {
			trafficWeight = strategy.Steps[targetStep].Traffic.Incumbent
		}

		if achieved, newSpec, reason := checkTraffic(curr.trafficTarget, uint32(trafficWeight)); !achieved {
			klog.Infof("Release %q %s", controller.MetaKey(curr.release), "hasn't achieved traffic yet")

			patches := make([]StrategyPatch, 0, 2)

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
				extra.Initiator.Name,
				cond,
				targetStep,
				extra.IsLastStep,
				extra.HasTail,
			)
			if relPatch.Alters(extra.Initiator) {
				patches = append(patches, relPatch)
			}

			return PipelineBreak, patches, nil, isLastVirtualStep
		}

		klog.Infof("Release %q %s", controller.MetaKey(curr.release), "has achieved traffic")

		cond.SetTrue(
			condType,
			conditions.StrategyConditionsUpdate{
				Step:               targetStep,
				LastTransitionTime: time.Now(),
			},
		)

		return PipelineContinue, nil, nil, isLastVirtualStep
	}
}

func getTotalDesiredReplicaCount(curr *releaseInfo, succ *releaseInfo) int64 {
	isHead := succ == nil
	totalReplicaCount := getReleaseReplicaCount(curr)
	//klog.Infof("HILLA got this curr replica count: ", totalReplicaCount)

	if !isHead {
		totalReplicaCount += getReleaseReplicaCount(succ)
	}
	return totalReplicaCount
}

func getReleaseReplicaCount(rel *releaseInfo) int64 {
	relValues := *rel.release.Spec.Environment.Values
	if relValues == nil {
		klog.Infof("HILLA no release spec env vals in rel %s", rel.release.Name)
		return 0
	}
	rcInterface := relValues["replicaCount"]
	if totalReplicaCount, ok := rcInterface.(int64); ok {
		return totalReplicaCount
	}
	klog.Infof("HILLA no replicaCount in rel %s", rel.release.Name)
	return 0
}

func genReleaseStrategyStateEnforcer(curr, succ *releaseInfo) PipelineStep {
	return func(strategy *shipper.RolloutStrategy, targetStep int32, virtualStep int32, extra Extra, cond conditions.StrategyConditionsMap) (PipelineContinuation, []StrategyPatch, []ReleaseStrategyStateTransition, bool) {
		var releaseStrategyStateTransitions []ReleaseStrategyStateTransition
		patches := make([]StrategyPatch, 0, 1)

		relStatus := curr.release.Status.DeepCopy()

		newReleaseStrategyState := cond.AsReleaseStrategyState(
			targetStep,
			extra.HasTail,
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
			extra.Initiator.Name,
			cond,
			targetStep,
			extra.IsLastStep,
			extra.HasTail,
		)

		if relPatch.Alters(extra.Initiator) {
			patches = append(patches, relPatch)
		}

		return PipelineContinue, patches, releaseStrategyStateTransitions, false
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

// This method is a super basic helper and should not be re-used as a reliable
// release similarity checker
func releasesIdentical(r1, r2 *shipper.Release) bool {
	if r1 == nil || r2 == nil {
		return r1 == r2
	}
	return r1.Namespace == r2.Namespace &&
		r1.Name == r2.Name
}

func isRelReady(rel *releaseInfo) bool {
	ready := true
	why := ""
	// check it
	it := rel.installationTarget
	ctReady, _ := targetutil.IsReady(it.Status.Conditions)
	if !ctReady {
		ready = false
		why += "installation not ready "
	}

	// check ct
	ct := rel.capacityTarget
	if ct.Status.ObservedGeneration >= ct.Generation {
		ctReady, _ := targetutil.IsReady(ct.Status.Conditions)
		if !ctReady {
			ready = false
			why += "capacity not ready "
		}
	} else {
		// definitally not ready!
		ready = false
		why += "capacity not ready "
	}

	// check tt
	tt := rel.trafficTarget
	if tt.Status.ObservedGeneration >= tt.Generation {
		ttReady, _ := targetutil.IsReady(tt.Status.Conditions)
		if !ttReady {
			ready = false
			why += "traffic not ready "
		}
	} else {
		// definitally not ready!
		ready = false
		why += "traffic not ready "
	}

	//klog.Infof("HILLA ready? %v why? %s", ready, why)
	return ready
}
