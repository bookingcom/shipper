package errors

import (
	"fmt"

	shipperV1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
)

type ChartFetchFailure struct {
	chart shipperV1.Chart
	err   error
}

func IsChartFetchFailure(err error) bool {
	_, ok := err.(ChartFetchFailure)
	return ok
}

func (e ChartFetchFailure) Name() string {
	return "ChartFetchFailure"
}

func (e ChartFetchFailure) Error() string {
	return fmt.Sprintf(
		"%s-%s failed to fetch from %s: %s",
		e.chart.Name, e.chart.Version, e.chart.RepoURL, e.err,
	)
}

// this means the chart repo was unavailable, so retry
func (e ChartFetchFailure) ShouldRetry() bool {
	return true
}

func NewChartFetchFailure(chart shipperV1.Chart, err error) ChartFetchFailure {
	return ChartFetchFailure{
		chart: chart,
		err:   err,
	}
}

type BrokenChart struct {
	chart shipperV1.Chart
	err   error
}

func IsBrokenChart(err error) bool {
	_, ok := err.(BrokenChart)
	return ok
}

func (e BrokenChart) Name() string {
	return "BrokenChart"
}

func (e BrokenChart) Error() string {
	return fmt.Sprintf(
		"Chart %s-%s failed to render: %s",
		e.chart.Name,
		e.chart.Version,
		e.err,
	)
}

func (e BrokenChart) ShouldRetry() bool {
	return false
}

func NewBrokenChart(chart shipperV1.Chart, err error) BrokenChart {
	return BrokenChart{
		chart: chart,
		err:   err,
	}
}

type WrongChartDeployments struct {
	chart           shipperV1.Chart
	deploymentCount int
}

func IsWrongChartDeployments(err error) bool {
	_, ok := err.(WrongChartDeployments)
	return ok
}

func (e WrongChartDeployments) Name() string {
	return "WrongChartDeployments"
}

func (e WrongChartDeployments) Error() string {
	return fmt.Sprintf(
		"Chart %s-%s should have exactly 1 Deployment object, but it has %d",
		e.chart.Name,
		e.chart.Version,
		e.deploymentCount,
	)
}

func (e WrongChartDeployments) ShouldRetry() bool {
	return false
}

func NewWrongChartDeployments(chart shipperV1.Chart, deploymentCount int) WrongChartDeployments {
	return WrongChartDeployments{
		chart:           chart,
		deploymentCount: deploymentCount,
	}
}
