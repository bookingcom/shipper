package release

import (
	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

func IsEmpty(rel *shipper.Release) bool {
	return rel.Spec.Environment.Chart == shipper.Chart{} &&
		rel.Spec.Environment.Values == nil &&
		rel.Spec.Environment.Strategy == nil &&
		len(rel.Spec.Environment.ClusterRequirements.Regions) == 0 &&
		len(rel.Spec.Environment.ClusterRequirements.Capabilities) == 0
}
