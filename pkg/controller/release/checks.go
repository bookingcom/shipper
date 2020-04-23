package release

import (
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
	if tt.Spec.Weight != stepTrafficWeight {
		newSpec := &shipper.TrafficTargetSpec{
			Weight: stepTrafficWeight,
		}

		return false, newSpec, "patches pending"
	}

	if tt.Status.ObservedGeneration >= tt.Generation {
		canProceed, reason := targetutil.IsReady(tt.Status.Conditions)
		return canProceed, nil, reason
	}

	return false, nil, "in progress"
}
