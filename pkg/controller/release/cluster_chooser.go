package release

import (
	"sort"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

// computeTargetClusters picks out the clusters from the given list which match
// the release's clusterRequirements.
func computeTargetClusters(rel *shipper.Release, clusterList []*shipper.Cluster) ([]*shipper.Cluster, error) {
	regionSpecs := rel.Spec.Environment.ClusterRequirements.Regions
	requiredCapabilities := rel.Spec.Environment.ClusterRequirements.Capabilities
	capableClustersByRegion := map[string][]*shipper.Cluster{}
	regionReplicas := map[string]int{}

	if len(regionSpecs) == 0 {
		return nil, shippererrors.NewNoRegionsSpecifiedError()
	}

	app, err := releaseutil.ApplicationNameForRelease(rel)
	if err != nil {
		return nil, err
	}

	err = validateClusterRequirements(rel.Spec.Environment.ClusterRequirements)
	if err != nil {
		return nil, err
	}

	prefList := buildPrefList(app, clusterList)
	// This algo could probably build up hashes instead of doing linear searches,
	// but these data sets are so tiny (1-20 items) that it'd only be useful for
	// readability.
	for _, region := range regionSpecs {
		capableClustersByRegion[region.Name] = []*shipper.Cluster{}
		if region.Replicas == nil {
			regionReplicas[region.Name] = 1
		} else {
			regionReplicas[region.Name] = int(*region.Replicas)
		}

		matchedRegion := 0
		for _, cluster := range prefList {
			if cluster.Spec.Scheduler.Unschedulable {
				continue
			}

			if cluster.Spec.Region == region.Name {
				matchedRegion++
				capabilityMatch := 0
				for _, requiredCapability := range requiredCapabilities {
					for _, providedCapability := range cluster.Spec.Capabilities {
						if requiredCapability == providedCapability {
							capabilityMatch++
							break
						}
					}
				}

				if capabilityMatch == len(requiredCapabilities) {
					capableClustersByRegion[region.Name] = append(capableClustersByRegion[region.Name], cluster)
				}
			}
		}
		if regionReplicas[region.Name] > matchedRegion {
			return nil, shippererrors.NewNotEnoughClustersInRegionError(region.Name, regionReplicas[region.Name], matchedRegion)
		}
	}

	resClusters := make([]*shipper.Cluster, 0)
	for region, clusters := range capableClustersByRegion {
		if regionReplicas[region] > len(clusters) {
			return nil, shippererrors.NewNotEnoughCapableClustersInRegionError(
				region,
				requiredCapabilities,
				regionReplicas[region],
				len(clusters),
			)
		}

		//NOTE(btyler): this assumes we do not have duplicate cluster names. For the
		//moment cluster objects are cluster scoped; if they become namespace scoped
		//and releases can somehow be scheduled to clusters from multiple namespaces,
		//this assumption will be wrong.
		for _, cluster := range clusters {
			if regionReplicas[region] > 0 {
				regionReplicas[region]--
				resClusters = append(resClusters, cluster)
			}
		}
	}

	sort.Slice(resClusters, func(i, j int) bool {
		return resClusters[i].Name < resClusters[j].Name
	})

	return resClusters, nil
}

func validateClusterRequirements(requirements shipper.ClusterRequirements) error {
	// Ensure capability uniqueness. Erroring instead of de-duping in order to
	// avoid second-guessing by operators about how Shipper might treat repeated
	// listings of the same capability.
	seenCapabilities := map[string]struct{}{}
	for _, capability := range requirements.Capabilities {
		_, ok := seenCapabilities[capability]
		if ok {
			return shippererrors.NewDuplicateCapabilityRequirementError(capability)
		}
		seenCapabilities[capability] = struct{}{}
	}

	return nil
}
