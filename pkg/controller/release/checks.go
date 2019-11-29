package release

import (
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	installationutil "github.com/bookingcom/shipper/pkg/util/installation"
	replicasutil "github.com/bookingcom/shipper/pkg/util/replicas"
	targetutil "github.com/bookingcom/shipper/pkg/util/target"
)

func checkInstallation(contenderRelease *releaseInfo) (bool, []string) {
	clustersFromStatus := contenderRelease.installationTarget.Status.Clusters
	clustersFromSpec := contenderRelease.installationTarget.Spec.Clusters
	clustersFromSpecMap := make(map[string]struct{})
	clustersFromStatusMap := make(map[string]struct{})
	clustersNotReady := make(map[string]struct{})

	for _, e := range clustersFromSpec {
		clustersFromSpecMap[e] = struct{}{}
	}

	for _, e := range clustersFromStatus {
		clustersFromStatusMap[e.Name] = struct{}{}
	}

	// NOTE(btyler): not comparing against 0 because 'uninstall' looks like a 0-len
	// Clusters in the Spec, which is correct.
	if len(clustersFromStatusMap) != len(clustersFromSpecMap) {
		for k := range clustersFromSpecMap {
			if _, ok := clustersFromStatusMap[k]; !ok {
				clustersNotReady[k] = struct{}{}
			}
		}
	}

	for _, clusterStatus := range clustersFromStatus {
		readyCond := installationutil.GetClusterInstallationCondition(*clusterStatus, shipper.ClusterConditionTypeReady)
		if readyCond == nil || readyCond.Status != corev1.ConditionTrue {
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
	achievedCapacity    uint
	desiredCapacity     uint
	stepCapacity        uint
	totalReplicaCount   int32
	currentReplicaCount int32
}

// outdated     -> false, newSpec, nil
// pending      -> false, nil, nil
// capacity met -> true, nil, nil
// error        -> nil, err
func checkCapacity(
	capacityTarget *shipper.CapacityTarget,
	stepCapacity uint,
) (
	bool,
	*shipper.CapacityTargetSpec,
	[]string,
) {

	// capacityState holds the capacity data collected for the release the executor
	// is processing.
	clusterCapacityData := make(map[string]capacityState)

	specs := capacityTarget.Spec.Clusters
	for _, spec := range specs {
		clusterCapacityData[spec.Name] = capacityState{
			stepCapacity:      stepCapacity,
			desiredCapacity:   uint(spec.Percent),
			totalReplicaCount: spec.TotalReplicaCount,
		}
	}

	statuses := capacityTarget.Status.Clusters
	if len(statuses) != len(specs) {
		return false, nil, nil
	}

	for _, status := range statuses {
		cd, ok := clusterCapacityData[status.Name]
		// This means that we have a status for a cluster which is not present in the
		// spec. Suspicious, sketchy, and probably fixed by the responsible controller
		// by the next time we look.
		if !ok {
			return false, nil, nil
		}
		cd.achievedCapacity = uint(status.AchievedPercent)
		cd.currentReplicaCount = status.AvailableReplicas
		clusterCapacityData[status.Name] = cd
	}

	clustersNotReady := make([]string, 0)
	canProceed := true
	newSpec := &shipper.CapacityTargetSpec{}

	for clusterName, v := range clusterCapacityData {
		// Now we can check whether or not the desired target step replicas have
		// been achieved. If this isn't the case, it means that we need to update
		// this cluster's desired capacity.
		if v.desiredCapacity != v.stepCapacity {
			// Patch capacityTarget .spec to attempt to achieve the desired state.
			r := shipper.ClusterCapacityTarget{Name: clusterName, Percent: int32(v.stepCapacity), TotalReplicaCount: v.totalReplicaCount}
			newSpec.Clusters = append(newSpec.Clusters, r)
			canProceed = false
			clustersNotReady = append(clustersNotReady, clusterName)
		} else if !replicasutil.AchievedDesiredReplicaPercentage(uint(v.totalReplicaCount), uint(v.currentReplicaCount), float64(v.desiredCapacity)) {
			canProceed = false
			clustersNotReady = append(clustersNotReady, clusterName)
		}
	}

	// We need a sorted order, otherwise it will trigger unnecessary etcd update operations
	sort.Strings(clustersNotReady)

	if len(newSpec.Clusters) > 0 {
		return canProceed, newSpec, clustersNotReady
	} else {
		return canProceed, nil, clustersNotReady
	}
}

func checkTraffic(
	tt *shipper.TrafficTarget,
	stepTrafficWeight uint32,
) (
	bool,
	*shipper.TrafficTargetSpec,
	string,
) {
	canProceed := true
	newSpec := &shipper.TrafficTargetSpec{}
	reason := ""

	clustersNotReadyMap := make(map[string]struct{})
	for _, spec := range tt.Spec.Clusters {
		if spec.Weight != stepTrafficWeight {
			t := shipper.ClusterTrafficTarget{
				Name:   spec.Name,
				Weight: stepTrafficWeight,
			}
			newSpec.Clusters = append(newSpec.Clusters, t)

			clustersNotReadyMap[spec.Name] = struct{}{}
			canProceed = false
		}
	}

	if canProceed {
		if tt.Status.ObservedGeneration >= tt.Generation {
			canProceed, reason = targetutil.IsReady(tt.Status.Conditions)
		} else {
			canProceed = false
			clustersNotReady := make([]string, 0)
			for _, c := range tt.Spec.Clusters {
				clustersNotReady = append(clustersNotReady, c.Name)
			}

			// We need a sorted order, otherwise it will trigger
			// unnecessary etcd update operations
			sort.Strings(clustersNotReady)

			reason = fmt.Sprintf("%v", clustersNotReady)
		}

	} else {
		clustersNotReady := make([]string, 0)
		for c, _ := range clustersNotReadyMap {
			clustersNotReady = append(clustersNotReady, c)
		}

		// We need a sorted order, otherwise it will trigger
		// unnecessary etcd update operations
		sort.Strings(clustersNotReady)

		reason = fmt.Sprintf("%v", clustersNotReady)
	}

	if len(newSpec.Clusters) > 0 {
		return canProceed, newSpec, reason
	} else {
		return canProceed, nil, reason
	}
}
