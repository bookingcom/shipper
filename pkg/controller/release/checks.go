package release

import (
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	installationutil "github.com/bookingcom/shipper/pkg/util/installation"
	targetutil "github.com/bookingcom/shipper/pkg/util/target"
)

func checkInstallation(relInfo *releaseInfo) (bool, []string) {
	clustersFromStatus := relInfo.installationTarget.Status.Clusters
	clustersFromSpec := relInfo.installationTarget.Spec.Clusters
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

func checkCapacity(
	ct *shipper.CapacityTarget,
	stepCapacity int32,
) (
	bool,
	*shipper.CapacityTargetSpec,
	string,
) {
	canProceed := true
	newSpec := &shipper.CapacityTargetSpec{}
	reason := ""

	clustersNotReadyMap := make(map[string]struct{})
	for _, spec := range ct.Spec.Clusters {
		if spec.Percent != stepCapacity {
			t := shipper.ClusterCapacityTarget{
				Name:              spec.Name,
				Percent:           stepCapacity,
				TotalReplicaCount: spec.TotalReplicaCount,
			}
			newSpec.Clusters = append(newSpec.Clusters, t)

			clustersNotReadyMap[spec.Name] = struct{}{}
			canProceed = false
		}
	}

	if canProceed {
		if ct.Status.ObservedGeneration >= ct.Generation {
			canProceed, reason = targetutil.IsReady(ct.Status.Conditions)
		} else {
			canProceed = false

			clustersNotReady := make([]string, 0)
			for _, spec := range ct.Spec.Clusters {
				clustersNotReady = append(clustersNotReady, spec.Name)
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
