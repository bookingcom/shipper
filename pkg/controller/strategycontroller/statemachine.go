package strategycontroller

import "github.com/bookingcom/shipper/pkg/apis/shipper/v1"

type StrategyExecutor struct {
	release            *v1.Release
	installationTarget *v1.InstallationTarget
	trafficTarget      *v1.TrafficTarget
	capacityTarget     *v1.CapacityTarget
}

type CapacityTargetOutdatedError struct {
	NewSpec *v1.CapacityTargetSpec
}

func (c *CapacityTargetOutdatedError) Error() string {
	return "CapacityTargetOutdatedError"
}

type TrafficTargetOutdatedError struct {
	NewSpec *v1.TrafficTargetSpec
}

func (c *TrafficTargetOutdatedError) Error() string {
	return "TrafficTargetOutdatedError"
}

func (s *StrategyExecutor) execute() error {
	// Order: installation -> capacity -> target -> wait

	if state := s.InstallationState(); state == TargetStatePending {
		return nil
	}

	if state := s.CapacityState(); state == TargetStatePending {
		return nil
	} else if state == TargetStateOutdated {
		return &CapacityTargetOutdatedError{}
	}

	if state := s.TrafficState(); state == TargetStatePending {
		return nil
	} else if state == TargetStateOutdated {
		return &TrafficTargetOutdatedError{}
	}

	// Update internal Release copy
	s.release.Status.AchievedStep = 1
	s.release.Status.Phase = v1.ReleasePhaseWaitingForCommand

	return nil
}

func (s *StrategyExecutor) InstallationState() TargetState {
	clusterStatuses := s.installationTarget.Status.Clusters
	for _, clusterStatus := range clusterStatuses {
		if clusterStatus.Status != "Installed" {
			return TargetStatePending
		}
	}
	return TargetStateAchieved
}

type capacityData struct {
	availableReplicas  uint
	desiredReplicas    uint
	targetStepReplicas uint
}

type TargetState int

const (
	TargetStateAchieved TargetState = iota
	TargetStatePending
	TargetStateOutdated
)

func (s *StrategyExecutor) CapacityState() TargetState {

	// targetStep can currently be either 0 or 1, so we adjust our expected replicas
	// accordingly. This should stay here until I have rebased master with @asurikov's
	// changes.
	// targetStep := s.release.Spec.TargetStep
	targetStepReplicas := uint(*s.release.Environment.Replicas)

	// capacityData holds the capacity data collected for the release the executor is
	// processing.
	capacityData := make(map[string]capacityData)

	specs := s.capacityTarget.Spec.Clusters
	for _, spec := range specs {
		capacityData[spec.Name] = capacityData{
			targetStepReplicas: targetStepReplicas,
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
			return TargetStatePending
		}

		// Now we can check whether or not the desired target step replicas have
		// been achieved. If this isn't the case, it means that we need to update
		// the spec and bail out.
		if v.availableReplicas != v.targetStepReplicas {
			// Patch capacityTarget .spec to attempt to achieve the desired state.
			return TargetStateOutdated
		}
	}
	return TargetStateAchieved
}

type trafficData struct {
	achievedTraffic   uint
	targetTraffic     uint
	targetStepTraffic uint
}

func (s *StrategyExecutor) TrafficState() TargetState {

	// targetStep can currently be either 0 or 1, so we adjust our expected replicas
	// accordingly. This should stay here until I have rebased master with @asurikov's
	// changes.
	targetStep := s.release.Spec.TargetStep
	var targetStepTraffic uint
	if targetStep == 0 {
		targetStepTraffic = 0
	} else {
		targetStepTraffic = 100
	}

	// trafficData holds the traffic data collected for the release the executor is
	// processing.
	trafficData := make(map[string]trafficData)

	specs := s.trafficTarget.Spec.Clusters
	for _, spec := range specs {
		trafficData[spec.Name] = trafficData{
			achievedTraffic:   spec.TargetTraffic,
			targetStepTraffic: targetStepTraffic,
		}
	}

	statuses := s.trafficTarget.Status.Clusters
	for _, status := range statuses {
		(trafficData[status.Name]).achievedTraffic = status.AchievedTraffic
	}

	for _, v := range trafficData {

		// If the number of achieved and target traffic are different in here,
		// it means that even if this was the state the strategy is expecting the
		// strategy should not proceed.
		if v.achievedTraffic != v.targetTraffic {
			return TargetStatePending
		}

		// Now we can check whether or not the desired target step traffic have
		// been achieved. If this isn't the case, it means that we need to update
		// the spec and bail out.
		if v.achievedTraffic != v.targetStepTraffic {
			// Patch trafficTarget .spec to attempt to achieve the desired state.
			return TargetStateOutdated
		}
	}

	return TargetStateAchieved
}
