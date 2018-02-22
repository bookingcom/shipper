package strategy

import "github.com/bookingcom/shipper/pkg/apis/shipper/v1"

func checkInstallation(contenderRelease *releaseInfo) bool {
	clusterStatuses := contenderRelease.installationTarget.Status.Clusters

	// NOTE(btyler) not comparing against 0 because 'uninstall' looks like
	// a 0-len Clusters in the Spec, which is correct.
	if len(clusterStatuses) != len(contenderRelease.installationTarget.Spec.Clusters) {
		return false
	}

	for _, clusterStatus := range clusterStatuses {
		if clusterStatus.Status != v1.ReleasePhaseInstalled {
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
func checkCapacity(capacityTarget *v1.CapacityTarget, stepCapacity uint, compFn func(achieved uint, desired uint) bool) (bool, *v1.CapacityTargetSpec) {

	// capacityState holds the capacity data collected for the release the executor is
	// processing.
	capacityData := make(map[string]capacityState)

	specs := capacityTarget.Spec.Clusters
	for _, spec := range specs {
		capacityData[spec.Name] = capacityState{
			stepCapacity:    stepCapacity,
			desiredCapacity: uint(spec.Percent),
		}
	}

	statuses := capacityTarget.Status.Clusters
	if len(statuses) != len(specs) {
		return false, nil
	}

	for _, status := range statuses {
		cd, ok := capacityData[status.Name]
		// this means that we have a status for a cluster which is not present
		// in the spec. suspicious, sketchy, and probably fixed by the responsible
		// controller by the next time we look
		if !ok {
			return false, nil
		}
		cd.achievedCapacity = uint(status.AchievedPercent)
		capacityData[status.Name] = cd
	}

	canProceed := false
	newSpec := &v1.CapacityTargetSpec{}

	for k, v := range capacityData {

		// Now we can check whether or not the desired target step replicas have
		// been achieved. If this isn't the case, it means that we need to update
		// this cluster's desired capacity.
		if v.desiredCapacity != v.stepCapacity {
			// Patch capacityTarget .spec to attempt to achieve the desired state.
			r := v1.ClusterCapacityTarget{Name: k, Percent: int32(v.stepCapacity)}
			newSpec.Clusters = append(newSpec.Clusters, r)
		} else if compFn(v.achievedCapacity, v.desiredCapacity) {
			canProceed = true
		}
	}

	if len(newSpec.Clusters) > 0 {
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

func checkTraffic(trafficTarget *v1.TrafficTarget, stepTrafficWeight uint, compFn func(achieved uint, desired uint) bool) (bool, *v1.TrafficTargetSpec) {

	trafficData := make(map[string]trafficState)

	specs := trafficTarget.Spec.Clusters
	for _, spec := range specs {
		trafficData[spec.Name] = trafficState{
			desiredTrafficWeight: spec.TargetTraffic,
			stepTrafficWeight:    stepTrafficWeight,
		}
	}

	statuses := trafficTarget.Status.Clusters
	if len(statuses) != len(specs) {
		return false, nil
	}

	for _, status := range statuses {
		td, ok := trafficData[status.Name]
		// this means that we have a status for a cluster which is not present
		// in the spec. suspicious, sketchy, and probably fixed by the responsible
		// controller by the next time we look
		if !ok {
			return false, nil
		}

		td.achievedTrafficWeight = status.AchievedTraffic
		trafficData[status.Name] = td
	}

	canContinue := false
	newSpec := &v1.TrafficTargetSpec{}

	for k, v := range trafficData {
		if v.desiredTrafficWeight != v.stepTrafficWeight {
			t := v1.ClusterTrafficTarget{Name: k, TargetTraffic: v.stepTrafficWeight}
			newSpec.Clusters = append(newSpec.Clusters, t)
		} else if compFn(v.achievedTrafficWeight, v.desiredTrafficWeight) {
			canContinue = true
		}
	}

	if len(newSpec.Clusters) > 0 {
		return canContinue, newSpec
	} else {
		return canContinue, nil
	}
}
