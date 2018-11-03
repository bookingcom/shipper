package release

import (
	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
)

func IsEmpty(rel *shipperv1.Release) bool {
	return rel.Spec.Environment.Chart == shipperv1.Chart{} &&
		rel.Spec.Environment.Values == nil &&
		rel.Spec.Environment.Strategy == nil &&
		len(rel.Spec.Environment.ClusterRequirements.Regions) == 0 &&
		len(rel.Spec.Environment.ClusterRequirements.Capabilities) == 0
}
