package release

import (
	"math"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

func HasEmptyEnvironment(rel *shipper.Release) bool {
	return rel.Spec.Environment.Chart == shipper.Chart{} &&
		rel.Spec.Environment.Values == nil &&
		rel.Spec.Environment.Strategy == nil &&
		len(rel.Spec.Environment.ClusterRequirements.Regions) == 0 &&
		len(rel.Spec.Environment.ClusterRequirements.Capabilities) == 0
}

func HasReleaseAchievedTargetStep(rel *shipper.Release) bool {
	if rel == nil || rel.Status.AchievedStep == nil {
		return false
	}
	return rel.Status.AchievedStep.Step == rel.Spec.TargetStep
}

func IsLastStrategyStep(rel *shipper.Release) bool {
	targetStep := rel.Spec.TargetStep
	numSteps := len(rel.Spec.Environment.Strategy.Steps)
	return targetStep == int32(numSteps-1)
}

func IsReleaseProgressing(achievedStep *shipper.AchievedStep, targetStep int32) bool {
	return achievedStep == nil || achievedStep.Step <= targetStep
}

func StepsFromAchievedStep(achievedStep *shipper.AchievedStep, targetStep int32) int {
	if achievedStep == nil {
		return int(targetStep)
	}
	return int(math.Abs(float64(achievedStep.Step - targetStep)))
}

func GetTargetStep(rel, succ *shipper.Release, strategy *shipper.RolloutStrategy, stepsFromAchievedStep int, progressing, achievedTargetStep bool) int32 {
	isHead := succ == nil
	var targetStep int32
	if isHead {
		targetStep = rel.Spec.TargetStep
	} else {
		targetStep = succ.Spec.TargetStep
	}

	if stepsFromAchievedStep <= len(strategy.Steps) {
		// we would like to move one step at a time
		if progressing && !achievedTargetStep && stepsFromAchievedStep > 1 {
			targetStep -= int32(stepsFromAchievedStep) - 1
		} else if !progressing && !achievedTargetStep && stepsFromAchievedStep > 1 {
			targetStep += int32(stepsFromAchievedStep) - 1
		}
	}

	return targetStep
}

func GetProcessedTargetStep(virtualStrategy *shipper.RolloutVirtualStrategy, targetStep int32, progressing, achievedTargetStep bool) int32 {
	processTargetStep := targetStep
	if !progressing && !achievedTargetStep {
		processTargetStep += 1
	}
	if processTargetStep >= int32(len(virtualStrategy.Steps)) {
		processTargetStep = int32(len(virtualStrategy.Steps) - 1)
	}

	return processTargetStep
}

func GetVirtualTargetStep(
	rel,
	succ *shipper.Release,
	virtualStrategy *shipper.RolloutVirtualStrategy,
	targetStep,
	processTargetStep int32,
	progressing,
	achievedTargetStep bool,
) int32 {
	isHead := succ == nil
	var targetVirtualStep int32
	if isHead {
		targetVirtualStep = rel.Spec.TargetVirtualStep
	} else {
		targetVirtualStep = succ.Spec.TargetVirtualStep
	}

	virtualSteps := virtualStrategy.Steps[processTargetStep].VirtualSteps
	if achievedTargetStep {
		targetVirtualStep = int32(len(virtualSteps)) - 1
	}

	prevVirtualStep := rel.Status.AchievedVirtualStep
	targetStepChanged := isHead && ((prevVirtualStep != nil && prevVirtualStep.Step != targetStep) || prevVirtualStep == nil)
	if targetStepChanged {
		// we should restart virtual process
		targetVirtualStep = 0
		if !progressing {
			// this is stepping backwards in the strategy
			// we should move backwards in the virtual strategy as well (from last virtual step to first)
			targetVirtualStep = int32(len(virtualSteps)) - 1
		}
	}

	targetVirtualStepOutOfRange := targetVirtualStep >= int32(len(virtualSteps)) || targetVirtualStep < 0
	if targetVirtualStepOutOfRange {
		if targetVirtualStep >= int32(len(virtualSteps)) {
			targetVirtualStep = int32(len(virtualSteps)) - 1
		} else if targetVirtualStep < 0 {
			targetVirtualStep = 0
		}
	}

	return targetVirtualStep
}
