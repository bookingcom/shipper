package release

import (
	"fmt"
	"github.com/bookingcom/shipper/pkg/util/replicas"
	targetutil "github.com/bookingcom/shipper/pkg/util/target"
	"math"
	"time"

	intstrutil "k8s.io/apimachinery/pkg/util/intstr"
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

type PipelineStep func(*shipper.RolloutStrategy, int32, int32, Extra, conditions.StrategyConditionsMap) (PipelineContinuation, []StrategyPatch, []ReleaseStrategyStateTransition)

type Pipeline []PipelineStep

func NewPipeline() *Pipeline {
	return new(Pipeline)
}

func (p *Pipeline) Enqueue(step PipelineStep) {
	*p = append(*p, step)
}

type Extra struct {
	IsLastStep bool
	HasTail    bool
	Initiator  *shipper.Release
}

func (p *Pipeline) Process(strategy *shipper.RolloutStrategy, step int32, interStep int32, extra Extra, cond conditions.StrategyConditionsMap) (bool, []StrategyPatch, []ReleaseStrategyStateTransition) {
	var patches []StrategyPatch
	var trans []ReleaseStrategyStateTransition
	complete := true
	for _, stage := range *p {
		cont, steppatches, steptrans := stage(strategy, step, 0, extra, cond)
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
		Initiator:  curr.release,
		IsLastStep: isLastStep,
		HasTail:    hasTail,
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

	return pipeline.Process(e.strategy, e.step, 0, extra, cond)
}

func genInstallationEnforcer(curr, succ *releaseInfo) PipelineStep {
	return func(strategy *shipper.RolloutStrategy, targetStep int32, interStep int32, extra Extra, cond conditions.StrategyConditionsMap) (PipelineContinuation, []StrategyPatch, []ReleaseStrategyStateTransition) {
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
	return func(strategy *shipper.RolloutStrategy, targetStep int32, interStep int32, extra Extra, cond conditions.StrategyConditionsMap) (PipelineContinuation, []StrategyPatch, []ReleaseStrategyStateTransition) {
		var condType shipper.StrategyConditionType
		var capacityWeight int32
		isHead := succ == nil
		isInitiator := releasesIdentical(extra.Initiator, curr.release)

		if isInitiator {
			condType = shipper.StrategyConditionContenderAchievedCapacity
		} else {
			condType = shipper.StrategyConditionIncumbentAchievedCapacity
		}
		if isHead {
			capacityWeight = strategy.Steps[targetStep].Capacity.Contender
		} else {
			capacityWeight = strategy.Steps[targetStep].Capacity.Incumbent
		}

		//klog.Infof("HILLA want to achieve this weight %d", capacityWeight)
		klog.Infof("HILLA initiator is %s", extra.Initiator.Name)
		totalReplicaCount := getTotalDesiredReplicaCount(curr, succ)
		ruPointer := strategy.RollingUpdate
		var maxSurgeValue intstrutil.IntOrString
		if ruPointer != nil {
			maxSurgeValue = ruPointer.MaxSurge
		} else {
			maxSurgeValue = intstrutil.FromString("100%")
		}

		surge, err := intstrutil.GetValueFromIntOrPercent(intstrutil.ValueOrDefault(&maxSurgeValue, intstrutil.FromString("100%")), int(totalReplicaCount), true)
		if err != nil {
			klog.Infof("HILLA got this error %s", err)
		}
		// compare target and achieved step to know which direction to go
		var achievedStep int32 = 0
		if curr.release.Status.AchievedStep != nil {
			achievedStep = curr.release.Status.AchievedStep.Step
		} else {
			// did not achieve any step
			achievedStep = curr.release.Spec.TargetStep
		}
		var progress bool
		if isHead {
			if targetStep > achievedStep || targetStep == achievedStep {
				// progressing a release, new capacity should be old capacity + surge
				progress = true
			} else {
				// reverting a release, new capacity should be old capacity - surge
				progress = false
			}
		} else {
			if succ.release.Status.AchievedStep == nil {
				// did not achieve any step
				progress = false
			} else {
				achievedStep = succ.release.Status.AchievedStep.Step
				succTargetStep := succ.release.Spec.TargetStep
				if succTargetStep > achievedStep {
					// successor is progressing, current should not
					progress = false
				} else if succTargetStep == achievedStep {
					// successor is not head, current should not progress
					progress = false
				} else {
					// successor is stepping backward, current should step forward
					progress = true
				}
			}
		}

		//klog.Infof("HILLA isHead %s, target step %d achievedStep %d, progressing? %s", isHead, targetStep, achievedStep, progress)
		stepCapacity := make(map[string]int32)
		ct := curr.capacityTarget
		for _, spec := range ct.Spec.Clusters {
			var status shipper.ClusterCapacityStatus // TODO HILLA: will the clusters be sorted in the same order in spec and in status?
			for _, s := range ct.Status.Clusters {
				if s.Name == spec.Name {
					status = s
					break
				}
			}
			// surge is amount of pods. now we need to calculate the amount of pods we currently have
			// ct.spec.clusters[*].percent is how much we should have by now.
			// this value can be different for each cluster :facepalm: (though it shouldn't)
			capacity := replicas.CalculateDesiredReplicaCount(uint(spec.TotalReplicaCount), float64(spec.Percent)) //int32(math.Ceil(float64(spec.TotalReplicaCount) * (float64(spec.Percent) / 100.0)))
			var newCapacity float64

			if progress {
				newCapacity = float64(capacity + uint(surge))
			} else {
				// Here we need to decreed surge pods from CURRENT capacity, not from desired capacity
				availableReplicas := status.AvailableReplicas
				//klog.Infof("HILLA available replicas %d", availableReplicas)
				newCapacity = float64(availableReplicas - int32(surge))
				// don't drop too low!
				newCapacityWight := int32(newCapacity / float64(getReleaseReplicaCount(curr)) * 100)
				//klog.Infof("HILLA newCapacityWight is %d, desired capacity weight is %d", newCapacityWight, capacityWeight)
				if newCapacityWight < capacityWeight {
					//newCapacity = math.Ceil(float64(spec.TotalReplicaCount) * (float64(capacityWeight) / 100.0))
					newCapacity = float64(replicas.CalculateDesiredReplicaCount(uint(spec.TotalReplicaCount), float64(capacityWeight)))
				}
			}
			if newCapacity > float64(spec.TotalReplicaCount) {
				newCapacity = float64(spec.TotalReplicaCount)
			}
			if newCapacity < 0.0 {
				newCapacity = 0.0
			}
			//klog.Infof("HILLA old capacity %d, surge %s, new capacity is %.1f pods, total replica count is %d pods", capacity, surge, newCapacity, spec.TotalReplicaCount)
			// newCapacity is number of pods, now we translate it to percent
			newPercent := int32(math.Ceil(newCapacity / float64(getReleaseReplicaCount(curr)) * 100.0))
			if progress && newPercent > capacityWeight {
				newPercent = capacityWeight
			}
			stepCapacity[spec.Name] = newPercent
			//klog.Infof("HILLA new capacity is %d percent", stepCapacity[spec.Name])
		}

		// check if ct is ready! If it's not ready - leave it alone and let it get ready!!!
		if !isRelReady(curr) {
			return PipelineContinue, nil, nil
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

			return PipelineBreak, patches, nil
		}

		klog.Infof("Release %q %s", controller.MetaKey(curr.release), "has achieved capacity")

		cond.SetTrue(
			condType,
			conditions.StrategyConditionsUpdate{
				Step:               targetStep,
				LastTransitionTime: time.Now(),
				Reason:             "",
				Message:            "",
			},
		)

		return PipelineContinue, nil, nil
	}
}

func genTrafficEnforcer(curr, succ *releaseInfo) PipelineStep {
	return func(strategy *shipper.RolloutStrategy, targetStep int32, interStep int32, extra Extra, cond conditions.StrategyConditionsMap) (PipelineContinuation, []StrategyPatch, []ReleaseStrategyStateTransition) {
		var condType shipper.StrategyConditionType
		var trafficWeight int32
		isHead := succ == nil
		isInitiator := releasesIdentical(extra.Initiator, curr.release)

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
	return func(strategy *shipper.RolloutStrategy, targetStep int32, interStep int32, extra Extra, cond conditions.StrategyConditionsMap) (PipelineContinuation, []StrategyPatch, []ReleaseStrategyStateTransition) {
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

	klog.Infof("HILLA ready? %v why? %s", ready, why)
	return ready
}
