package release

import (
	"fmt"
	"sort"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	targetutil "github.com/bookingcom/shipper/pkg/util/target"
)

func checkInstallation(it *shipper.InstallationTarget) (bool, string) {
	return targetutil.IsReady(it.Status.Conditions)
}

func checkCapacity(
	ct *shipper.CapacityTarget,
	stepCapacity int32,
) (
	bool,
	*shipper.CapacityTargetSpec,
	string,
) {
	if ct.Spec.Percent != stepCapacity {
		newSpec := &shipper.CapacityTargetSpec{
			Percent:           stepCapacity,
			TotalReplicaCount: ct.Spec.TotalReplicaCount,
		}

		return false, newSpec, "patches pending"
	}

	if ct.Status.ObservedGeneration >= ct.Generation {
		canProceed, reason := targetutil.IsReady(ct.Status.Conditions)
		return canProceed, nil, reason
	}

	return false, nil, "in progress"
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
		t := spec
		if spec.Weight != stepTrafficWeight {
			t = shipper.ClusterTrafficTarget{
				Name:   spec.Name,
				Weight: stepTrafficWeight,
			}

			clustersNotReadyMap[spec.Name] = struct{}{}
			canProceed = false
		}
		newSpec.Clusters = append(newSpec.Clusters, t)
	}

	if canProceed {
		// We return an empty new spec if cluster spec check went fine
		// so far.
		newSpec = nil

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

	return canProceed, newSpec, reason
}
