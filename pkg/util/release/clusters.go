package release

import (
	"strings"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

func GetSelectedClusters(rel *shipper.Release) []string {
	clusterAnnotation, ok := rel.Annotations[shipper.ReleaseClustersAnnotation]
	if !ok {
		return nil
	}

	if len(clusterAnnotation) == 0 {
		return []string{}
	}

	return strings.Split(clusterAnnotation, ",")
}
