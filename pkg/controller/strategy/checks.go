package strategy

import "github.com/bookingcom/shipper/pkg/apis/shipper/v1"

func checkInstallation(contenderRelease *releaseInfo) bool {
	clusterStatuses := contenderRelease.installationTarget.Status.Clusters
	for _, clusterStatus := range clusterStatuses {
		if clusterStatus.Status != "Installed" {
			return false
		}
	}
	return true
}

type capacityState struct {
	achievedCapacity uint
	desiredCapacity  uint
	stepCapacity     uint
}

// outdated     -> false, newSpec, nil
// pending      -> false, nil, nil
// capacity met -> true, nil, nil
// error        -> nil, err
func checkCapacity(capacityTarget *v1.CapacityTarget, stepCapacity uint) (bool, *v1.CapacityTargetSpec) {

	// capacityState holds the capacity data collected for the release the executor is
	// processing.
	capacityData := make(map[string]capacityState)

	specs := capacityTarget.Spec.Clusters
	for _, spec := range specs {
		capacityData[spec.Name] = capacityState{
			stepCapacity:    stepCapacity,
			desiredCapacity: spec.Percent,
		}
	}

	statuses := capacityTarget.Status.Clusters
	for _, status := range statuses {
		cd := capacityData[status.Name]
		cd.achievedCapacity = status.AchievedPercent
		capacityData[status.Name] = cd
	}

	canProceed := true
	newSpec := &v1.CapacityTargetSpec{}

	for k, v := range capacityData {

		// Now we can check whether or not the desired target step replicas have
		// been achieved. If this isn't the case, it means that we need to update
		// this cluster's desired capacity.
		if v.desiredCapacity != v.stepCapacity {
			// Patch capacityTarget .spec to attempt to achieve the desired state.
			r := v1.ClusterCapacityTarget{Name: k, Percent: v.stepCapacity}
			newSpec.Clusters = append(newSpec.Clusters, r)
			canProceed = false
		} else if v.achievedCapacity < v.desiredCapacity {

			// In the case this branch has been reached, we assume that the capacity
			// for this step hasn't been met by setting up isFinished to false. It
			// means that isFinished will be false if at least one cluster hasn't
			// achieved the desired capacity.
			canProceed = false
		} else {

			// Noop, meaning that desired capacity and current step's capacity match,
			// and k cluster's desired capacity for the current step has been achieved.
			// Hopefully Go's compiler will remove this branch out.
		}
	}

	if len(newSpec.Clusters) > 0 {
		// In practice isFinished here will be always false, since there's at least one
		// ClusterCapacityTarget item in newSpec.Clusters.
		return canProceed, newSpec
	} else {
		return canProceed, nil
	}
}

type trafficState struct {
	achievedTrafficWeight uint
	desiredTrafficWeight  uint
	stepTrafficWeight     uint
}

func checkTraffic(trafficTarget *v1.TrafficTarget, stepTrafficWeight uint) (bool, *v1.TrafficTargetSpec) {

	trafficData := make(map[string]trafficState)

	specs := trafficTarget.Spec.Clusters
	for _, spec := range specs {
		trafficData[spec.Name] = trafficState{
			desiredTrafficWeight: spec.TargetTraffic,
			stepTrafficWeight:    stepTrafficWeight,
		}
	}

	statuses := trafficTarget.Status.Clusters
	for _, status := range statuses {
		td := trafficData[status.Name]
		td.achievedTrafficWeight = status.AchievedTraffic
		trafficData[status.Name] = td
	}

	canContinue := true
	newSpec := &v1.TrafficTargetSpec{}

	for k, v := range trafficData {
		if v.desiredTrafficWeight != v.stepTrafficWeight {
			t := v1.ClusterTrafficTarget{Name: k, TargetTraffic: v.stepTrafficWeight}
			newSpec.Clusters = append(newSpec.Clusters, t)
			canContinue = false
		} else if v.achievedTrafficWeight < v.desiredTrafficWeight {
			canContinue = false
		} else {
			// Noop.
		}
	}

	if len(newSpec.Clusters) > 0 {
		return canContinue, newSpec
	} else {
		return canContinue, nil
	}
}
