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
	if capacityPatches, err := s.checkCapacity(strategyStep); err != nil {
		return nil, err
	} else if len(capacityPatches) > 0 {
		glog.Infof("capacity target hasn't been achieved yet for step %d", targetStep)
		return capacityPatches, nil
	} else {
		glog.Infof("capacity target has been achieved for step %d", targetStep)
	}

	//////////////////////////////////////////////////////////////////////////
	// Traffic
	//
	if trafficPatches, err := s.checkTraffic(strategyStep); err != nil {
		return nil, err
	} else if len(trafficPatches) > 0 {
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
		incumbentPhase = v1.ReleasePhaseDecommissioned
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

func (s *Executor) checkTraffic(strategyStep v1.StrategyStep) ([]interface{}, error) {
	var trafficPatches []interface{}

	contenderTrafficWeight, err := strconv.Atoi(strategyStep.ContenderTraffic)
	if err != nil {
		return nil, err
	}

	if canContinue, newSpec := checkTraffic(s.contenderRelease.trafficTarget, uint(contenderTrafficWeight)); !canContinue {
		if newSpec != nil {
			trafficPatches = append(trafficPatches, &TrafficTargetOutdatedResult{
				NewSpec: newSpec,
				Name:    s.contenderRelease.release.Name,
			})
		}
	}

	if s.incumbentRelease != nil {
		incumbentTrafficWeight, err := strconv.Atoi(strategyStep.ContenderCapacity)
		if err != nil {
			return nil, err
		}

		if canContinue, newSpec := checkTraffic(s.incumbentRelease.trafficTarget, uint(incumbentTrafficWeight)); !canContinue {
			if newSpec != nil {
				trafficPatches = append(trafficPatches, &TrafficTargetOutdatedResult{
					NewSpec: newSpec,
					Name:    s.incumbentRelease.release.Name,
				})
			}
			return nil, nil
		}
	}

	if len(trafficPatches) > 0 {
		return trafficPatches, nil
	} else {
		return nil, nil
	}
}

func (s *Executor) checkCapacity(strategyStep v1.StrategyStep) ([]interface{}, error) {
	var capacityPatches []interface{}

	contenderCapacity, err := strconv.Atoi(strategyStep.ContenderCapacity)
	if err != nil {
		return nil, err
	}

	if canContinue, newSpec := checkCapacity(s.contenderRelease.capacityTarget, uint(contenderCapacity)); !canContinue {
		if newSpec != nil {
			capacityPatches = append(capacityPatches, &CapacityTargetOutdatedResult{
				NewSpec: newSpec,
				Name:    s.contenderRelease.release.Name,
			})
		}
	}

	if s.incumbentRelease != nil {
		incumbentCapacity, err := strconv.Atoi(strategyStep.IncumbentCapacity)
		if err != nil {
			return nil, err
		}

		if canContinue, newSpec := checkCapacity(s.incumbentRelease.capacityTarget, uint(incumbentCapacity)); !canContinue {
			if newSpec != nil {
				capacityPatches = append(capacityPatches, &CapacityTargetOutdatedResult{
					NewSpec: newSpec,
					Name:    s.incumbentRelease.release.Name,
				})
			}
		}
	}

	if len(capacityPatches) > 0 {
		return capacityPatches, nil
	} else {
		return nil, nil
	}
}
