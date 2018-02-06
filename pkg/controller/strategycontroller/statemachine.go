package strategycontroller

import "github.com/bookingcom/shipper/pkg/apis/shipper/v1"

type StrategyExecutor struct {
	release            *v1.Release
	installationTarget *v1.InstallationTarget
	trafficTarget      *v1.TrafficTarget
	capacityTarget     *v1.CapacityTarget
}

func (s *StrategyExecutor) execute() error {
	// Order: installation -> capacity -> target -> wait

	if !s.isInstallationFinished() {
		return nil
	}

	if !s.isCapacityFinished() {
		return nil
	}

	if !s.isTrafficFinished() {
		return nil
	}

	s.release.Status.Phase = v1.ReleasePhaseWaitingForCommand

	return nil
}

func (s *StrategyExecutor) isInstallationFinished() bool {
	clusterStatuses := s.installationTarget.Status.Clusters
	for _, clusterStatus := range clusterStatuses {
		if clusterStatus.Status == "Unknown" {
			return false
		}
	}
	return true
}

type capacityData struct {
	availableReplicas  uint
	desiredReplicas    uint
	targetStepReplicas uint
}

func (s *StrategyExecutor) isCapacityFinished() bool {

	// targetStep can currently be either 0 or 1, so we adjust our expected replicas
	// accordingly. This should stay here until I have rebased master with @asurikov's
	// changes.
	targetStep := s.release.Spec.TargetStep
	targetStepReplicas := targetStep

	// capacityData holds the capacity data collected for the release the executor is
	// processing.
	capacityData := make(map[string]capacityData)

	specs := s.capacityTarget.Spec.Clusters
	for _, spec := range specs {
		capacityData[spec.Name] = capacityData{
			targetStepReplicas: uint(targetStepReplicas),
			desiredReplicas:    spec.Replicas,
		}
	}

	statuses := s.capacityTarget.Status.Clusters
	for _, status := range statuses {
		(capacityData[status.Name]).availableReplicas = status.AvailableReplicas
	}

	for _, v := range capacityData {

		// If the number of desired and available replicas are different in here,
		// it means that even if this was the state the strategy is expecting the
		// strategy should not proceed.
		if v.availableReplicas != v.desiredReplicas {
			return false
		}

		// Now we can check whether or not the desired target step replicas have
		// been achieved. If this isn't the case, it means that we need to update
		// the spec and bail out.
		if v.availableReplicas != v.targetStepReplicas {
			// Patch capacityTarget .spec to attempt to achieve the desired state.
			return false
		}
	}
	return true
}

type trafficData struct {
	achievedTraffic uint
	targetTraffic   uint
}

func (s *StrategyExecutor) isTrafficFinished() bool {

	trafficData := make(map[string]trafficData)

	specs := s.trafficTarget.Spec.Clusters
	for _, spec := range specs {
		trafficData[spec.Name] = trafficData{achievedTraffic: spec.TargetTraffic}
	}

	statuses := s.trafficTarget.Status.Clusters
	for _, status := range statuses {
		(trafficData[status.Name]).achievedTraffic = status.AchievedTraffic
	}

	for _, v := range trafficData {
		if v.achievedTraffic != v.targetTraffic {
			return false
		}
	}

	return true
}
