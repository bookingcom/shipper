package strategycontroller

import (
	"encoding/json"
	"github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type Executor struct {
	release            *v1.Release
	installationTarget *v1.InstallationTarget
	trafficTarget      *v1.TrafficTarget
	capacityTarget     *v1.CapacityTarget
}

type ExecutorResult interface {
	Patch() (schema.GroupVersionKind, []byte)
}

type CapacityTargetOutdatedResult struct {
	NewSpec *v1.CapacityTargetSpec
}

type TrafficTargetOutdatedResult struct {
	NewSpec *v1.TrafficTargetSpec
}

type ReleaseUpdateResult struct {
	NewStatus *v1.ReleaseStatus
}

func (c *CapacityTargetOutdatedResult) Patch() (schema.GroupVersionKind, []byte) {
	b, _ := json.Marshal(c.NewSpec)
	return (&v1.CapacityTarget{}).GroupVersionKind(), b
}

func (c *TrafficTargetOutdatedResult) Patch() (schema.GroupVersionKind, []byte) {
	b, _ := json.Marshal(c.NewSpec)
	return (&v1.TrafficTarget{}).GroupVersionKind(), b
}

func (r *ReleaseUpdateResult) Patch() (schema.GroupVersionKind, []byte) {
	b, _ := json.Marshal(r.NewStatus)
	return (&v1.Release{}).GroupVersionKind(), b
}

// execute executes the strategy. It returns an ExecutorResult, if a patch should
// be performed into some of the associated Release objects and an error if an error
// has happened. Currently if both values are nil it means that the operation was
// successful but no modifications are required.
func (s *Executor) execute() (ExecutorResult, error) {

	if state := s.InstallationState(); state == TargetStatePending {
		return nil, nil
	}

	if state := s.CapacityState(); state == TargetStatePending {
		return nil, nil
	} else if state == TargetStateOutdated {
		// Compute desired capacity target for step
		return &CapacityTargetOutdatedResult{}, nil
	}

	if state := s.TrafficState(); state == TargetStatePending {
		return nil, nil
	} else if state == TargetStateOutdated {
		// Compute desired traffic target for step
		return &TrafficTargetOutdatedResult{}, nil
	}

	newReleaseStatus := s.release.Status.DeepCopy()
	newReleaseStatus.AchievedStep = 1
	newReleaseStatus.Phase = v1.ReleasePhaseWaitingForCommand
	return &ReleaseUpdateResult{NewStatus: newReleaseStatus}, nil
}

func (s *Executor) InstallationState() TargetState {
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

func (s *Executor) CapacityState() TargetState {

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

func (s *Executor) TrafficState() TargetState {

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
