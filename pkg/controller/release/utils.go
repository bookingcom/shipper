package release

import (
	"fmt"
	"sort"
	"strings"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	"github.com/bookingcom/shipper/pkg/util/conditions"
)

func reasonForReleaseCondition(err error) string {
	switch err.(type) {
	case shippererrors.NoRegionsSpecifiedError:
		return "NoRegionsSpecified"
	case shippererrors.NotEnoughClustersInRegionError:
		return "NotEnoughClustersInRegion"
	case shippererrors.NotEnoughCapableClustersInRegionError:
		return "NotEnoughCapableClustersInRegion"

	case shippererrors.DuplicateCapabilityRequirementError:
		return "DuplicateCapabilityRequirement"

	case shippererrors.ChartFetchFailureError:
		return "ChartFetchFailure"
	case shippererrors.BrokenChartSpecError:
		return "BrokenChartSpec"
	case shippererrors.WrongChartDeploymentsError:
		return "WrongChartDeployments"
	case shippererrors.RolloutBlockError:
		return "RolloutBlock"
	case shippererrors.ChartRepoInternalError:
		return "ChartRepoInternal"
	case shippererrors.NoCachedChartRepoIndexError:
		return "NoCachedChartRepoIndex"
	}

	if shippererrors.IsKubeclientError(err) {
		return "FailedAPICall"
	}

	return fmt.Sprintf("unknown error %T! tell Shipper devs to classify it", err)
}

func setReleaseClusters(rel *shipper.Release, clusters []*shipper.Cluster) {
	clusterNames := make([]string, 0, len(clusters))
	memo := make(map[string]struct{})
	for _, cluster := range clusters {
		if _, ok := memo[cluster.Name]; ok {
			continue
		}
		clusterNames = append(clusterNames, cluster.Name)
		memo[cluster.Name] = struct{}{}
	}
	sort.Strings(clusterNames)
	rel.Annotations[shipper.ReleaseClustersAnnotation] = strings.Join(clusterNames, ",")
}

func consolidateStrategyStatus(
	isHead, isLastStep bool,
	clusterConditions map[string]conditions.StrategyConditionsMap,
) (
	bool,
	*shipper.ReleaseStrategyStatus,
) {
	waitingForInstallation := false
	waitingForCapacity := false
	waitingForTraffic := false

	newClusterStatuses := make(
		[]shipper.ClusterStrategyStatus, 0, len(clusterConditions))

	for clusterName, conditions := range clusterConditions {
		waitingForInstallation = waitingForInstallation || conditions.IsFalse(shipper.StrategyConditionContenderAchievedInstallation)
		waitingForCapacity = waitingForCapacity || conditions.IsFalse(shipper.StrategyConditionContenderAchievedCapacity)
		waitingForTraffic = waitingForTraffic || conditions.IsFalse(shipper.StrategyConditionContenderAchievedTraffic)

		if isHead {
			waitingForCapacity = waitingForCapacity || conditions.IsFalse(shipper.StrategyConditionIncumbentAchievedCapacity)
			waitingForTraffic = waitingForTraffic || conditions.IsFalse(shipper.StrategyConditionIncumbentAchievedTraffic)
		}

		clusterStatus := shipper.ClusterStrategyStatus{
			Name:       clusterName,
			Conditions: conditions.AsReleaseStrategyConditions(),
		}
		newClusterStatuses = append(newClusterStatuses, clusterStatus)
	}

	sort.Sort(byClusterName(newClusterStatuses))

	stepComplete := !waitingForInstallation && !waitingForCapacity && !waitingForTraffic
	waitingForCommand := stepComplete && !isLastStep
	state := shipper.ReleaseStrategyState{
		WaitingForInstallation: boolToStrategyState(waitingForInstallation),
		WaitingForCapacity:     boolToStrategyState(waitingForCapacity),
		WaitingForTraffic:      boolToStrategyState(waitingForTraffic),
		WaitingForCommand:      boolToStrategyState(waitingForCommand),
	}

	return stepComplete, &shipper.ReleaseStrategyStatus{
		Clusters: newClusterStatuses,
		State:    state,
	}
}

func boolToStrategyState(b bool) shipper.StrategyState {
	if b {
		return shipper.StrategyStateTrue
	}

	return shipper.StrategyStateFalse
}
