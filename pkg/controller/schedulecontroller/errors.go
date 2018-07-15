package schedulecontroller

import (
	"fmt"
	"strings"
)

const (
	retry   = true
	noRetry = false
)

func classifyError(err error) (string, bool) {
	switch err.(type) {
	case NotEnoughClustersInRegionError:
		return "NotEnoughClustersInRegion", noRetry
	case NotEnoughCapableClustersInRegionError:
		return "NotEnoughCapableClustersInRegion", noRetry

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
