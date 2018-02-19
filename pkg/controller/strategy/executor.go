package strategy

import (
	"github.com/golang/glog"
	"github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	"strconv"
)

type releaseInfo struct {
	release            *v1.Release
	installationTarget *v1.InstallationTarget
	trafficTarget      *v1.TrafficTarget
	capacityTarget     *v1.CapacityTarget
}

type Executor struct {
	contenderRelease *releaseInfo
	incumbentRelease *releaseInfo
}

// execute executes the strategy. It returns an ExecutorResult, if a patch should
// be performed into some of the associated Release objects and an error if an error
// has happened. Currently if both values are nil it means that the operation was
// successful but no modifications are required.
func (s *Executor) execute() ([]interface{}, error) {

	strategy := getStrategy(string(s.contenderRelease.release.Environment.ShipmentOrder.Strategy))
	targetStep := uint(s.contenderRelease.release.Spec.TargetStep)
	achievedStep := s.contenderRelease.release.Status.AchievedStep

	if achievedStep == targetStep {
		glog.Infof("it seems that achievedStep (%d) is the same as targetStep (%d)", achievedStep, targetStep)
		return nil, nil
	}

	strategyStep, err := strategy.GetStep(targetStep)
	if err != nil {
		return nil, err
	}

	//////////////////////////////////////////////////////////////////////////
	// Installation
	//
	if canContinue := checkInstallation(s.contenderRelease); !canContinue {
		glog.Infof("Installation pending for release %q", s.contenderRelease.release.Name)
		return nil, nil
	} else {
		glog.Infof("installation target has been achieved for release %q", s.contenderRelease.release.Name)
	}

	//////////////////////////////////////////////////////////////////////////
	// Capacity
	//
	if capacityAchieved, capacityPatches, err := s.checkCapacity(strategyStep); err != nil {
		return nil, err
	} else if !capacityAchieved {
		glog.Infof("capacity target hasn't been achieved yet for step %d", targetStep)
		return capacityPatches, nil
	} else {
		glog.Infof("capacity target has been achieved for step %d", targetStep)
	}

	//////////////////////////////////////////////////////////////////////////
	// Traffic
	//
	if trafficAchieved, trafficPatches, err := s.checkTraffic(strategyStep); err != nil {
		return nil, err
	} else if !trafficAchieved {
		glog.Infof("traffic target hasn't been achieved yet for step %d", targetStep)
		return trafficPatches, nil
	} else {
		glog.Infof("traffic target has been achieved for step %d", targetStep)
	}

	//////////////////////////////////////////////////////////////////////////
	// Release
	//
	lastStepIndex := len(strategy.Steps) - 1
	if lastStepIndex < 0 {
		lastStepIndex = 0
	}

	isLastStep := targetStep == uint(lastStepIndex)

	if releasePatches, err := s.finalizeRelease(targetStep, strategyStep, isLastStep); err != nil {
		return nil, err
	} else {
		glog.Infof("release %q has been finished", s.contenderRelease.release.Name)
		return releasePatches, nil
	}
}

func (s *Executor) finalizeRelease(targetStep uint, strategyStep v1.StrategyStep, isLastStep bool) ([]interface{}, error) {
	var contenderPhase string
	var incumbentPhase string

	if isLastStep {
		contenderPhase = v1.ReleasePhaseInstalled
		incumbentPhase = v1.ReleasePhaseSuperseded
	} else {
		contenderPhase = v1.ReleasePhaseWaitingForCommand
	}

	var releasePatches []interface{}

	newReleaseStatus := &v1.ReleaseStatus{
		AchievedStep: targetStep,
		Phase:        contenderPhase,
	}
	releasePatches = append(releasePatches, &ReleaseUpdateResult{
		NewStatus: newReleaseStatus,
		Name:      s.contenderRelease.release.Name,
	})

	if s.incumbentRelease != nil {
		newReleaseStatus := &v1.ReleaseStatus{
			Phase: incumbentPhase,
		}
		releasePatches = append(releasePatches, &ReleaseUpdateResult{
			NewStatus: newReleaseStatus,
			Name:      s.incumbentRelease.release.Name,
		})
	}

	return releasePatches, nil

}

func (s *Executor) checkTraffic(strategyStep v1.StrategyStep) (bool, []interface{}, error) {
	var trafficPatches []interface{}

	contenderTrafficWeight, err := strconv.Atoi(strategyStep.ContenderTraffic)
	if err != nil {
		return false, nil, err
	}

	canContinue := true
	trafficAchieved, newSpec := checkTraffic(s.contenderRelease.trafficTarget, uint(contenderTrafficWeight), contenderTrafficComparison)
	if !trafficAchieved {
		canContinue = false
		if newSpec != nil {
			trafficPatches = append(trafficPatches, &TrafficTargetOutdatedResult{
				NewSpec: newSpec,
				Name:    s.contenderRelease.release.Name,
			})
		}
	}

	if s.incumbentRelease != nil {
		incumbentTrafficWeight, err := strconv.Atoi(strategyStep.IncumbentTraffic)
		if err != nil {
			return false, nil, err
		}

		trafficAchieved, newSpec := checkTraffic(s.incumbentRelease.trafficTarget, uint(incumbentTrafficWeight), incumbentTrafficComparison)
		if !trafficAchieved {
			canContinue = false
			if newSpec != nil {
				trafficPatches = append(trafficPatches, &TrafficTargetOutdatedResult{
					NewSpec: newSpec,
					Name:    s.incumbentRelease.release.Name,
				})
			}
		}
	}

	if len(trafficPatches) > 0 {
		return false, trafficPatches, nil
	} else {
		return canContinue, nil, nil
	}
}

func (s *Executor) checkCapacity(strategyStep v1.StrategyStep) (bool, []interface{}, error) {
	var capacityPatches []interface{}

	contenderCapacity, err := strconv.Atoi(strategyStep.ContenderCapacity)
	if err != nil {
		return false, nil, err
	}

	canContinue := true
	capacityAchieved, newSpec := checkCapacity(s.contenderRelease.capacityTarget, uint(contenderCapacity), contenderCapacityComparison)
	if !capacityAchieved {
		canContinue = false
		if newSpec != nil {
			capacityPatches = append(capacityPatches, &CapacityTargetOutdatedResult{
				NewSpec: newSpec,
				Name:    s.contenderRelease.release.Name,
			})
		}
	}

	if s.incumbentRelease != nil {
		glog.Infof("Found incumbent release: %s", s.incumbentRelease.release.Name)
		incumbentCapacity, err := strconv.Atoi(strategyStep.IncumbentCapacity)
		if err != nil {
			return false, nil, err
		}

		capacityAchieved, newSpec := checkCapacity(s.incumbentRelease.capacityTarget, uint(incumbentCapacity), incumbentCapacityComparison)
		if !capacityAchieved {
			canContinue = false
			if newSpec != nil {
				capacityPatches = append(capacityPatches, &CapacityTargetOutdatedResult{
					NewSpec: newSpec,
					Name:    s.incumbentRelease.release.Name,
				})
			}
		}
	}

	if len(capacityPatches) > 0 {
		return false, capacityPatches, nil
	} else {
		return canContinue, nil, nil
	}
}
