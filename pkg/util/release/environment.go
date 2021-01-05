package release

import (
	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

func HasEmptyEnvironment(rel *shipper.Release) bool {
	return rel.Spec.Environment.Chart == shipper.Chart{} &&
		rel.Spec.Environment.Values == nil &&
		rel.Spec.Environment.Strategy == nil &&
		len(rel.Spec.Environment.ClusterRequirements.Regions) == 0 &&
		len(rel.Spec.Environment.ClusterRequirements.Capabilities) == 0
}

func ReleaseAchievedTargetStep(rel *shipper.Release) bool {
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
