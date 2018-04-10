package strategy

import "github.com/bookingcom/shipper/pkg/apis/shipper/v1"

func checkInstallation(contenderRelease *releaseInfo) (bool, []string) {
	clustersFromStatus := contenderRelease.installationTarget.Status.Clusters
	clustersFromSpec := contenderRelease.installationTarget.Spec.Clusters
	clusterStatuses := clustersFromStatus
	clustersFromSpecMap := make(map[string]struct{})
	clustersFromStatusMap := make(map[string]struct{})
	clustersNotReady := make(map[string]struct{})

	for _, e := range clustersFromSpec {
		clustersFromSpecMap[e] = struct{}{}
	}

	for _, e := range clustersFromStatus {
		clustersFromStatusMap[e.Name] = struct{}{}
	}

	// NOTE(btyler) not comparing against 0 because 'uninstall' looks like
	// a 0-len Clusters in the Spec, which is correct.
	if len(clustersFromStatusMap) != len(clustersFromSpecMap) {
		for k := range clustersFromSpecMap {
			if _, ok := clustersFromStatusMap[k]; !ok {
				clustersNotReady[k] = struct{}{}
			}
		}
	}

	for _, clusterStatus := range clusterStatuses {
		if clusterStatus.Status != v1.ReleasePhaseInstalled {
			clustersNotReady[clusterStatus.Name] = struct{}{}
		}
	}

	if len(clustersNotReady) > 0 {
		clusters := make([]string, 0, len(clustersNotReady))
		for k := range clustersNotReady {
			clusters = append(clusters, k)
		}
		return false, clusters
	}
	return true, nil
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
func checkCapacity(
	capacityTarget *v1.CapacityTarget,
	stepCapacity uint,
	compFn func(achieved uint, desired uint) bool,
) (
	bool,
	*v1.CapacityTargetSpec,
	[]string,
) {

	// capacityState holds the capacity data collected for the release the executor is
	// processing.
	clusterCapacityData := make(map[string]capacityState)

	specs := capacityTarget.Spec.Clusters
	for _, spec := range specs {
		clusterCapacityData[spec.Name] = capacityState{
			stepCapacity:    stepCapacity,
			desiredCapacity: uint(spec.Percent),
		}
	}

	statuses := capacityTarget.Status.Clusters
	if len(statuses) != len(specs) {
		return false, nil, nil
	}

	for _, status := range statuses {
		cd, ok := clusterCapacityData[status.Name]
		// this means that we have a status for a cluster which is not present
		// in the spec. suspicious, sketchy, and probably fixed by the responsible
		// controller by the next time we look
		if !ok {
			return false, nil, nil
		}
		cd.achievedCapacity = uint(status.AchievedPercent)
		clusterCapacityData[status.Name] = cd
	}

	clustersNotReady := make([]string, 0)
	canProceed := true
	newSpec := &v1.CapacityTargetSpec{}

	for clusterName, v := range clusterCapacityData {
		// Now we can check whether or not the desired target step replicas have
		// been achieved. If this isn't the case, it means that we need to update
		// this cluster's desired capacity.
		if v.desiredCapacity != v.stepCapacity {
			// Patch capacityTarget .spec to attempt to achieve the desired state.
			r := v1.ClusterCapacityTarget{Name: clusterName, Percent: int32(v.stepCapacity)}
			newSpec.Clusters = append(newSpec.Clusters, r)
			canProceed = false
			clustersNotReady = append(clustersNotReady, clusterName)
		} else if !compFn(v.achievedCapacity, v.desiredCapacity) {
			canProceed = false
			clustersNotReady = append(clustersNotReady, clusterName)
		}
	}

	if len(newSpec.Clusters) > 0 {
		return canProceed, newSpec, clustersNotReady
	} else {
		return canProceed, nil, clustersNotReady
	}
}

type trafficState struct {
	achievedTrafficWeight uint
	desiredTrafficWeight  uint
	stepTrafficWeight     uint
}

func checkTraffic(
	trafficTarget *v1.TrafficTarget,
	stepTrafficWeight uint,
	compFn func(achieved uint, desired uint) bool,
) (
	bool,
	*v1.TrafficTargetSpec,
	[]string,
) {

	clusterTrafficData := make(map[string]trafficState)

	specs := trafficTarget.Spec.Clusters
	for _, spec := range specs {
		clusterTrafficData[spec.Name] = trafficState{
			desiredTrafficWeight: spec.TargetTraffic,
			stepTrafficWeight:    stepTrafficWeight,
		}
	}

	statuses := trafficTarget.Status.Clusters
	if len(statuses) != len(specs) {
		return false, nil, nil
	}

	for _, status := range statuses {
		td, ok := clusterTrafficData[status.Name]
		// this means that we have a status for a cluster which is not present
		// in the spec. suspicious, sketchy, and probably fixed by the responsible
		// controller by the next time we look
		if !ok {
			return false, nil, nil
		}

		td.achievedTrafficWeight = status.AchievedTraffic
		clusterTrafficData[status.Name] = td
	}

	clustersNotReady := make([]string, 0)
	canProceed := true
	newSpec := &v1.TrafficTargetSpec{}

	for clusterName, trafficData := range clusterTrafficData {
		if trafficData.desiredTrafficWeight != trafficData.stepTrafficWeight {
			t := v1.ClusterTrafficTarget{Name: clusterName, TargetTraffic: trafficData.stepTrafficWeight}
			newSpec.Clusters = append(newSpec.Clusters, t)
			canProceed = false
			clustersNotReady = append(clustersNotReady, clusterName)
		} else if !compFn(trafficData.achievedTrafficWeight, trafficData.desiredTrafficWeight) {
			canProceed = false
			clustersNotReady = append(clustersNotReady, clusterName)
		}
	}

	if len(newSpec.Clusters) > 0 {
		return canProceed, newSpec, clustersNotReady
	} else {
		return canProceed, nil, clustersNotReady
	}
}
