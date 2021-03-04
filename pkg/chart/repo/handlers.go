package repo

import (
	helmchart "helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/repo"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	errors "github.com/bookingcom/shipper/pkg/errors"
)

type ChartVersionResolver func(*shipper.Chart) (*repo.ChartVersion, error)

type ChartFetcher func(*shipper.Chart) (*helmchart.Chart, error)

func ResolveChartVersionFunc(c *Catalog) ChartVersionResolver {
	return func(chartspec *shipper.Chart) (*repo.ChartVersion, error) {
		repo, err := c.CreateRepoIfNotExist(chartspec.RepoURL)
		if err != nil {
			return nil, errors.NewChartVersionResolveError(chartspec, err)
		}

		return repo.ResolveVersion(chartspec)
	}
}

func FetchChartFunc(c *Catalog) ChartFetcher {
	return func(chartspec *shipper.Chart) (*helmchart.Chart, error) {
		repo, err := c.CreateRepoIfNotExist(chartspec.RepoURL)
		if err != nil {
			return nil, err
		}

		return repo.Fetch(chartspec)
	}
}
