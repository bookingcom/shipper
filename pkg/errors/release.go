package errors

import (
	"fmt"
	"strings"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

type ContenderNotFoundError struct {
	appName string
}

func (e ContenderNotFoundError) Error() string {
	return fmt.Sprintf("no contender release found for application %q", e.appName)
}

func (e ContenderNotFoundError) ShouldRetry() bool {
	return true
}

func IsContenderNotFoundError(err error) bool {
	_, ok := err.(*ContenderNotFoundError)
	return ok
}

func NewContenderNotFoundError(appName string) error {
	return &ContenderNotFoundError{appName: appName}
}

type IncumbentNotFoundError struct {
	appName string
}

func (e IncumbentNotFoundError) Error() string {
	return fmt.Sprintf("no incumbent release found for application %q", e.appName)
}

func (e IncumbentNotFoundError) ShouldRetry() bool {
	return true
}

func IsIncumbentNotFoundError(err error) bool {
	_, ok := err.(*IncumbentNotFoundError)
	return ok
}

func NewIncumbentNotFoundError(appName string) error {
	return &IncumbentNotFoundError{appName: appName}
}

type MissingGenerationAnnotationError struct {
	relName string
}

func (e MissingGenerationAnnotationError) Error() string {
	return fmt.Sprintf("missing label %q in release %q", shipper.ReleaseGenerationAnnotation, e.relName)
}

func (e MissingGenerationAnnotationError) ShouldRetry() bool {
	return true
}

func IsMissingGenerationAnnotationError(err error) bool {
	_, ok := err.(*MissingGenerationAnnotationError)
	return ok
}

func NewMissingGenerationAnnotationError(relName string) error {
	return &MissingGenerationAnnotationError{relName}
}

type InvalidGenerationAnnotationError struct {
	relName string
	err     error
}

func (e *InvalidGenerationAnnotationError) Error() string {
	return fmt.Sprintf("invalid value for label %q in release %q: %s", shipper.ReleaseGenerationAnnotation, e.relName, e.err)
}

func (e *InvalidGenerationAnnotationError) ShouldRetry() bool {
	return true
}

func IsInvalidGenerationAnnotationError(err error) bool {
	_, ok := err.(*InvalidGenerationAnnotationError)
	return ok
}

func NewInvalidGenerationAnnotationError(relName string, err error) error {
	return &InvalidGenerationAnnotationError{relName: relName, err: err}
}

type NoRegionsSpecifiedError struct{}

func (e NoRegionsSpecifiedError) Error() string {
	return "No regions specified in clusterRequirements. Must list at least one region"
}

func (e NoRegionsSpecifiedError) ShouldRetry() bool {
	return false
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

func (e NotEnoughClustersInRegionError) ShouldRetry() bool {
	return false
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

func (e NotEnoughCapableClustersInRegionError) ShouldRetry() bool {
	return false
}

func NewNotEnoughCapableClustersInRegionError(region string, capabilities []string, required, available int) error {
	return NotEnoughCapableClustersInRegionError{
		region:       region,
		capabilities: capabilities,
		required:     required,
		available:    available,
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

func (e ChartFetchFailureError) ShouldRetry() bool {
	return true
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

func (e BrokenChartError) ShouldRetry() bool {
	return false
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

func (e WrongChartDeploymentsError) ShouldRetry() bool {
	return false
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

func (e DuplicateCapabilityRequirementError) ShouldRetry() bool {
	return false
}

func NewDuplicateCapabilityRequirementError(capability string) DuplicateCapabilityRequirementError {
	return DuplicateCapabilityRequirementError{
		capability: capability,
	}
}

type NotWorkingOnStrategyError struct {
	contenderReleaseKey string
}

func (e NotWorkingOnStrategyError) Error() string {
	return fmt.Sprintf("found %s as a contender, but it is not currently working on any strategy", e.contenderReleaseKey)
}

func (e NotWorkingOnStrategyError) ShouldRetry() bool {
	return false
}

func NewNotWorkingOnStrategyError(contenderReleaseKey string) NotWorkingOnStrategyError {
	return NotWorkingOnStrategyError{
		contenderReleaseKey: contenderReleaseKey,
	}
}
