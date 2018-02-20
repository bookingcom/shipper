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
	contender *releaseInfo
	incumbent *releaseInfo
}

// execute executes the strategy. It returns an ExecutorResult, if a patch should
// be performed into some of the associated Release objects and an error if an error
// has happened. Currently if both values are nil it means that the operation was
// successful but no modifications are required.
func (s *Executor) execute() ([]interface{}, error) {

	strategy := getStrategy(string(s.contender.release.Environment.ShipmentOrder.Strategy))
	targetStep := uint(s.contender.release.Spec.TargetStep)
	achievedStep := s.contender.release.Status.AchievedStep

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
	if canContinue := checkInstallation(s.contender); !canContinue {
		glog.Infof("Installation pending for release %q", s.contender.release.Name)
		return nil, nil
	} else {
		glog.Infof("installation target has been achieved for release %q", s.contender.release.Name)
	}

	// Contender
	if contenderReady, contenderPatches, err := s.checkContender(strategyStep); err != nil {
		return nil, err
	} else if !contenderReady {
		glog.Infof("contender is not yet ready for release %q, step %d", s.contender.release.Name, targetStep)
		return contenderPatches, nil
	} else {
		glog.Infof("contender is ready for release %q, step %d", s.contender.release.Name, targetStep)
	}

	// Incumbent
	if incumbentReady, incumbentPatches, err := s.checkIncumbent(strategyStep); err != nil {
		return nil, err
	} else if !incumbentReady {
		glog.Infof("incumbent is not yet ready for release %q, step %d", s.incumbent.release.Name, targetStep)
		return incumbentPatches, nil
	} else {
		glog.Infof("incumbent is ready for step release %q, step %d", s.incumbent.release.Name, targetStep)
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
		glog.Infof("release %q has been finished", s.contender.release.Name)
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
		Name:      s.contender.release.Name,
	})

	if s.incumbent != nil {
		newReleaseStatus := &v1.ReleaseStatus{
			Phase: incumbentPhase,
		}
		releasePatches = append(releasePatches, &ReleaseUpdateResult{
			NewStatus: newReleaseStatus,
			Name:      s.incumbent.release.Name,
		})
	}

	return releasePatches, nil

}

func (s *Executor) checkContender(strategyStep v1.StrategyStep) (bool, []interface{}, error) {
	var patches []interface{}

	capacity, err := strconv.Atoi(strategyStep.ContenderCapacity)
	if err != nil {
		return false, nil, err
	}

	trafficWeight, err := strconv.Atoi(strategyStep.ContenderTraffic)
	if err != nil {
		return false, nil, err
	}

	if achieved, newSpec := checkCapacity(s.contender.capacityTarget, uint(capacity), contenderCapacityComparison); !achieved {
		glog.Infof("contender %q hasn't achieved capacity yet", s.contender.release.Name)
		if newSpec != nil {
			patches = append(patches, &CapacityTargetOutdatedResult{
				NewSpec: newSpec,
				Name:    s.contender.release.Name,
			})
			return false, patches, nil
		} else {
			return false, []interface{}{}, nil
		}
	} else {
		glog.Infof("contender %q has achieved capacity", s.contender.release.Name)
	}

	if achieved, newSpec := checkTraffic(s.contender.trafficTarget, uint(trafficWeight), contenderTrafficComparison); !achieved {
		glog.Infof("contender %q hasn't achieved traffic yet", s.contender.release.Name)
		if newSpec != nil {
			patches = append(patches, &TrafficTargetOutdatedResult{
				NewSpec: newSpec,
				Name:    s.contender.release.Name,
			})
			return false, patches, nil
		} else {
			return false, []interface{}{}, nil
		}
	} else {
		glog.Infof("contender %q has achieved traffic", s.contender.release.Name)
	}

	return true, []interface{}{}, nil
}

func (s *Executor) checkIncumbent(strategyStep v1.StrategyStep) (bool, []interface{}, error) {
	var patches []interface{}

	capacity, err := strconv.Atoi(strategyStep.IncumbentCapacity)
	if err != nil {
		return false, nil, err
	}

	trafficWeight, err := strconv.Atoi(strategyStep.IncumbentTraffic)
	if err != nil {
		return false, nil, err
	}

	if achieved, newSpec := checkTraffic(s.incumbent.trafficTarget, uint(trafficWeight), incumbentTrafficComparison); !achieved {
		glog.Infof("incumbent %q hasn't achieved traffic yet", s.incumbent.release.Name)
		if newSpec != nil {
			patches = append(patches, &TrafficTargetOutdatedResult{
				NewSpec: newSpec,
				Name:    s.incumbent.release.Name,
			})
			return false, patches, nil
		} else {
			return false, []interface{}{}, nil
		}
	} else {
		glog.Infof("incumbent %q has achieved traffic", s.incumbent.release.Name)
	}

	if achieved, newSpec := checkCapacity(s.incumbent.capacityTarget, uint(capacity), incumbentCapacityComparison); !achieved {
		glog.Infof("incumbent %q hasn't achieved capacity yet", s.incumbent.release.Name)
		if newSpec != nil {
			patches = append(patches, &CapacityTargetOutdatedResult{
				NewSpec: newSpec,
				Name:    s.incumbent.release.Name,
			})
			return false, patches, nil
		} else {
			return false, []interface{}{}, nil
		}
	} else {
		glog.Infof("incumbent %q has achieved capacity", s.incumbent.release.Name)
	}

	return true, []interface{}{}, nil
}
