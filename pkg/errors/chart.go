package errors

import (
	"fmt"

	"k8s.io/helm/pkg/repo"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

type ChartError struct {
	chartName    string
	chartVersion string
	chartRepo    string
}

func newChartError(chartspec *shipper.Chart) ChartError {
	return ChartError{
		chartName:    chartspec.Name,
		chartVersion: chartspec.Version,
		chartRepo:    chartspec.RepoURL,
	}
}

type ChartFetchFailureError struct {
	ChartError
	err error
}

func (e ChartFetchFailureError) Error() string {
	return fmt.Sprintf(
		"failed to fetch chart [name: %q, version: %q, repo: %q]: %s",
		e.chartName, e.chartVersion, e.chartRepo,
		e.err)
}

func (e ChartFetchFailureError) ShouldRetry() bool {
	return true
}

func NewChartFetchFailureError(chartspec *shipper.Chart, err error) ChartFetchFailureError {
	return ChartFetchFailureError{
		ChartError: newChartError(chartspec),
		err:        err,
	}
}

type BrokenChartSpecError struct {
	chartspec *shipper.Chart
	err       error
}

func (e BrokenChartSpecError) Error() string {
	return fmt.Sprintf(
		"broken shipper chart spec [name: %q, version: %q, repo: %q]: %s",
		e.chartspec.Name,
		e.chartspec.Version,
		e.chartspec.RepoURL,
		e.err,
	)
}

func (e BrokenChartSpecError) ShouldRetry() bool {
	return false
}

func NewBrokenChartSpecError(chartspec *shipper.Chart, err error) BrokenChartSpecError {
	return BrokenChartSpecError{
		chartspec: chartspec,
		err:       err,
	}
}

type BrokenChartVersionError struct {
	cv  *repo.ChartVersion
	err error
}

func (e BrokenChartVersionError) Error() string {
	return fmt.Sprintf(
		"broken helm repo ChartVersion [name: %q, version: %q, repo: %q]: %s",
		e.cv.GetName(),
		e.cv.GetVersion(),
		e.cv.URLs[0],
		e.err,
	)
}

func (e BrokenChartVersionError) ShouldRetry() bool {
	return false
}

func NewBrokenChartVersionError(cv *repo.ChartVersion, err error) BrokenChartVersionError {
	return BrokenChartVersionError{
		cv:  cv,
		err: err,
	}
}

type WrongChartDeploymentsError struct {
	ChartError
	deploymentCount int
}

func (e WrongChartDeploymentsError) Error() string {
	return fmt.Sprintf(
		"chart %s-%s should have exactly 1 Deployment object, but it has %d",
		e.chartName,
		e.chartVersion,
		e.deploymentCount,
	)
}

func (e WrongChartDeploymentsError) ShouldRetry() bool {
	return false
}

func NewWrongChartDeploymentsError(chartspec *shipper.Chart, deploymentCount int) WrongChartDeploymentsError {
	return WrongChartDeploymentsError{
		ChartError:      newChartError(chartspec),
		deploymentCount: deploymentCount,
	}
}

type RenderManifestError struct {
	err error
}

func (e RenderManifestError) Error() string {
	return e.err.Error()
}

func (e RenderManifestError) ShouldRetry() bool {
	return false
}

func NewRenderManifestError(err error) RenderManifestError {
	return RenderManifestError{err}
}

type ChartVersionResolveError struct {
	ChartError
	err error
}

func (e ChartVersionResolveError) Error() string {
	return fmt.Sprintf(
		"failed to resolve chart version [name: %q, version: %q, repo: %q]: %s",
		e.chartName, e.chartVersion, e.chartRepo,
		e.err,
	)
}

func (e ChartVersionResolveError) ShouldRetry() bool {
	return true
}

func NewChartVersionResolveError(chartspec *shipper.Chart, err error) ChartVersionResolveError {
	return ChartVersionResolveError{
		ChartError: newChartError(chartspec),
		err:        err,
	}
}

type ChartDataCorruptionError struct {
	ChartError
	err error
}

func (e ChartDataCorruptionError) Error() string {
	return fmt.Sprintf(
		"chart [name: %q, version: %q, repo: %q] data is corrupted: %s",
		e.chartName, e.chartVersion, e.chartRepo,
		e.err)
}

func (e ChartDataCorruptionError) ShouldRetry() bool {
	return false
}

func NewChartDataCorruptionError(cv *repo.ChartVersion, err error) ChartDataCorruptionError {
	return ChartDataCorruptionError{
		ChartError: ChartError{
			chartName:    cv.GetName(),
			chartVersion: cv.GetVersion(),
			chartRepo:    cv.URLs[0],
		},
		err: err,
	}
}

type NoCachedChartRepoIndexError struct {
	err error
}

func (e NoCachedChartRepoIndexError) Error() string {
	return fmt.Sprintf(
		"failed to get chart repo index and there is no index in cache: %s",
		e.err,
	)
}

func (e NoCachedChartRepoIndexError) ShouldRetry() bool {
	return true
}

func NewNoCachedChartRepoIndexError(err error) NoCachedChartRepoIndexError {
	return NoCachedChartRepoIndexError{err: err}
}

type ChartRepoIndexError struct {
	err error
}

func (e ChartRepoIndexError) Error() string {
	return fmt.Sprintf(
		"failed to get chart repo index: %s",
		e.err,
	)
}

func (e ChartRepoIndexError) ShouldRetry() bool {
	return true
}

func NewChartRepoIndexError(err error) ChartRepoIndexError {
	return ChartRepoIndexError{err: err}
}

type ChartRepoInternalError struct {
	err error
}

func (e ChartRepoInternalError) Error() string {
	return fmt.Sprintf(
		"internal chart repo client error: %s",
		e.err,
	)
}

func (e ChartRepoInternalError) ShouldRetry() bool {
	return true
}

func NewChartRepoInternalError(err error) ChartRepoInternalError {
	return ChartRepoInternalError{
		err: err,
	}
}
