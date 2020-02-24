package release

import (
	"fmt"
	"k8s.io/klog"
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
	canProceed := true
	newSpec := &shipper.CapacityTargetSpec{}
	reason := ""

	clustersNotReadyMap := make(map[string]struct{})
	for _, spec := range ct.Spec.Clusters {
		t := spec
		if spec.Percent != stepCapacity {
			t = shipper.ClusterCapacityTarget{
				Name:              spec.Name,
				Percent:           stepCapacity,
				TotalReplicaCount: spec.TotalReplicaCount,
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

	return canProceed, newSpec, reason
}

func checkCapacityHilla(
	ct *shipper.CapacityTarget,
	stepCapacitys map[string]int32,
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
		stepCapacity := stepCapacitys[spec.Name]
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

	klog.Infof("HILLA can proceed?? %s", canProceed)
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
