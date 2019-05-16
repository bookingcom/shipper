package release

import (
	"fmt"
	"strings"
)

func classifyError(err error) (string, bool) {
	switch err.(type) {
	case NoRegionsSpecifiedError:
		return "NoRegionsSpecified", noRetry
	case NotEnoughClustersInRegionError:
		return "NotEnoughClustersInRegion", noRetry
	case NotEnoughCapableClustersInRegionError:
		return "NotEnoughCapableClustersInRegion", noRetry

	case DuplicateCapabilityRequirementError:
		return "DuplicateCapabilityRequirement", noRetry

	case ChartFetchFailureError:
		return "ChartFetchFailure", retry
	case BrokenChartError:
		return "BrokenChart", noRetry
	case WrongChartDeploymentsError:
		return "WrongChartDeployments", noRetry

	case InvalidReleaseOwnerRefsError:
		return "InvalidReleaseOwnerRefs", noRetry

	case FailedAPICallError:
		return "FailedAPICall", retry
	}

	return "unknown error! tell Shipper devs to classify it", retry
}

type FailedAPICallError struct {
	call string
	err  error
}

func (e FailedAPICallError) Error() string {
	return fmt.Sprintf(
		"Failed API call %q: %q", e.call, e.err,
	)
}

func NewFailedAPICallError(call string, err error) FailedAPICallError {
	return FailedAPICallError{
		call: call,
		err:  err,
	}
}

type NoRegionsSpecifiedError struct{}

func (e NoRegionsSpecifiedError) Error() string {
	return "No regions specified in clusterRequirements. Must list at least one region"
}

func NewNoRegionsSpecifiedError() NoRegionsSpecifiedError {
	return NoRegionsSpecifiedError{}
}

type NotEnoughClustersInRegionError struct {
	region    string
	required  int
	available int
}

func (e NotEnoughClustersInRegionError) Error() string {
	return fmt.Sprintf("Not enough clusters in region %q. Required: %d / Available: %d", e.region, e.required, e.available)
}

func NewNotEnoughClustersInRegionError(region string, required, available int) NotEnoughClustersInRegionError {
	return NotEnoughClustersInRegionError{
		region:    region,
		required:  required,
		available: available,
	}
}

type NotEnoughCapableClustersInRegionError struct {
	region       string
	capabilities []string
	required     int
	available    int
}

func (e NotEnoughCapableClustersInRegionError) Error() string {
	capabilitiesString := strings.Join(e.capabilities, ",")
	return fmt.Sprintf(
		"Not enough clusters in region %q with required capabilities %q. Required: %d / Available: %d",
		e.region, capabilitiesString, e.required, e.available,
	)
}

func NewNotEnoughCapableClustersInRegionError(region string, capabilities []string, required, available int) error {
	return NotEnoughCapableClustersInRegionError{
		region:       region,
		capabilities: capabilities,
		required:     required,
		available:    available,
	}
}

type InvalidReleaseOwnerRefsError struct {
	count int
}

func (e InvalidReleaseOwnerRefsError) Error() string {
	return fmt.Sprintf(
		"Releases should only ever have 1 owner, but this one has %d", e.count,
	)
}

func NewInvalidReleaseOwnerRefsError(count int) InvalidReleaseOwnerRefsError {
	return InvalidReleaseOwnerRefsError{
		count: count,
	}
}

type ChartError struct {
	chartName    string
	chartVersion string
	chartRepo    string
}

type ChartFetchFailureError struct {
	ChartError
	err error
}

func (e ChartFetchFailureError) Error() string {
	return fmt.Sprintf(
		"%s-%s failed to fetch from %s: %s",
		e.chartName, e.chartVersion, e.chartRepo, e.err,
	)
}

func NewChartFetchFailureError(chartName, chartVersion, chartRepo string, err error) ChartFetchFailureError {
	return ChartFetchFailureError{
		ChartError: ChartError{
			chartName:    chartName,
			chartVersion: chartVersion,
			chartRepo:    chartRepo,
		},
		err: err,
	}
}

type BrokenChartError struct {
	ChartError
	err error
}

func (e BrokenChartError) Error() string {
	return fmt.Sprintf(
		"Chart %s-%s failed to render: %s",
		e.chartName,
		e.chartVersion,
		e.err,
	)
}

func NewBrokenChartError(chartName, chartVersion, chartRepo string, err error) BrokenChartError {
	return BrokenChartError{
		ChartError: ChartError{
			chartName:    chartName,
			chartVersion: chartVersion,
			chartRepo:    chartRepo,
		},
		err: err,
	}
}

type WrongChartDeploymentsError struct {
	ChartError
	deploymentCount int
}

func (e WrongChartDeploymentsError) Error() string {
	return fmt.Sprintf(
		"Chart %s-%s should have exactly 1 Deployment object, but it has %d",
		e.chartName,
		e.chartVersion,
		e.deploymentCount,
	)
}

func NewWrongChartDeploymentsError(chartName, chartVersion, chartRepo string, deploymentCount int) WrongChartDeploymentsError {
	return WrongChartDeploymentsError{
		ChartError: ChartError{
			chartName:    chartName,
			chartVersion: chartVersion,
			chartRepo:    chartRepo,
		},
		deploymentCount: deploymentCount,
	}
}

type DuplicateCapabilityRequirementError struct {
	capability string
}

func (e DuplicateCapabilityRequirementError) Error() string {
	return fmt.Sprintf(
		"Capability %q listed more than once in clusterRequirements",
		e.capability,
	)
}

func NewDuplicateCapabilityRequirementError(capability string) DuplicateCapabilityRequirementError {
	return DuplicateCapabilityRequirementError{
		capability: capability,
	}
}

type NotWorkingOnStrategyError struct {
	contenderReleaseKey string
}

func NewNotWorkingOnStrategyError(contenderReleaseKey string) NotWorkingOnStrategyError {
	return NotWorkingOnStrategyError{
		contenderReleaseKey: contenderReleaseKey,
	}
}

func (e NotWorkingOnStrategyError) Error() string {
	return fmt.Sprintf("found %s as a contender, but it is not currently working on any strategy", e.contenderReleaseKey)
}

type RetrievingInstallationTargetForReleaseError struct {
	releaseKey string
	err        error
}

func NewRetrievingInstallationTargetForReleaseError(releaseKey string, err error) RetrievingInstallationTargetForReleaseError {
	return RetrievingInstallationTargetForReleaseError{
		releaseKey: releaseKey,
		err:        err,
	}
}

func (e RetrievingInstallationTargetForReleaseError) Error() string {
	return fmt.Sprintf("error when retrieving installation target for release %s: %s", e.releaseKey, e.err)
}

type RetrievingCapacityTargetForReleaseError struct {
	releaseKey string
	err        error
}

func NewRetrievingCapacityTargetForReleaseError(releaseKey string, err error) RetrievingCapacityTargetForReleaseError {
	return RetrievingCapacityTargetForReleaseError{
		releaseKey: releaseKey,
		err:        err,
	}
}

func (e RetrievingCapacityTargetForReleaseError) Error() string {
	return fmt.Sprintf("error when retrieving capacity target for release %s: %s", e.releaseKey, e.err)
}

type RetrievingTrafficTargetForReleaseError struct {
	releaseKey string
	err        error
}

func NewRetrievingTrafficTargetForReleaseError(releaseKey string, err error) RetrievingTrafficTargetForReleaseError {
	return RetrievingTrafficTargetForReleaseError{
		releaseKey: releaseKey,
		err:        err,
	}
}

func (e RetrievingTrafficTargetForReleaseError) Error() string {
	return fmt.Sprintf("error when retrieving traffic target for release %s: %s", e.releaseKey, e.err)
}
