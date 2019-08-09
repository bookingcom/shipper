package repo

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	// Importing this yaml package is a very crucial point:
	// the "classical" yaml.v2 does not understand json annotations
	// in structure definitions and therefore always parses empty
	// index structures. This version is patched to understand json
	// annotations and works fine.
	"github.com/Masterminds/semver"
	yaml "github.com/ghodss/yaml"
	"github.com/golang/glog"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/helm/pkg/repo"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
)

const (
	RepoIndexRefreshPeriod = 10 * time.Second
)

var (
	ErrInvalidConstraint = errors.New("invalid constraint")
	ErrNoneMatching      = errors.New("no matching version found")
)

type Repo struct {
	repoURL       string
	indexURL      string
	cache         Cache
	fetcher       RemoteFetcher
	index         atomic.Value
	indexResolved chan struct{}
}

func NewRepo(repoURL string, cache Cache, fetcher RemoteFetcher) (*Repo, error) {
	parsed, err := url.ParseRequestURI(repoURL)
	if err != nil {
		return nil, shippererrors.NewChartRepoIndexError(
			fmt.Errorf("failed to parse repo URL: %v", err),
		)
	}
	parsed.Path = path.Join(parsed.Path, "index.yaml")
	indexURL := parsed.String()

	repo := &Repo{
		repoURL:       repoURL,
		indexURL:      indexURL,
		cache:         cache,
		fetcher:       fetcher,
		indexResolved: make(chan struct{}),
	}

	// runs repo.refreshIndex forever
	go wait.Forever(func() {
		if err := repo.refreshIndex(); err != nil {
			glog.Errorf("failed to refresh repo %q index: %s", repo.repoURL, err)
		}
	}, RepoIndexRefreshPeriod)

	return repo, nil
}

func (r *Repo) refreshIndex() error {
	data, err := r.fetcher(r.indexURL)
	if err != nil {
		return shippererrors.NewChartRepoIndexError(
			fmt.Errorf("failed to fetch %q: %v", r.indexURL, err),
		)
	}

	index, err := loadIndexData(data)
	if err != nil {
		return shippererrors.NewChartRepoIndexError(
			fmt.Errorf("failed to load index file: %v", err),
		)
	}

	r.index.Store(index)

	// close indexResolved once
	select {
	default:
		close(r.indexResolved)
	case <-r.indexResolved:
		// already closed
	}

	return nil
}

func (r *Repo) ResolveVersion(chartspec *shipper.Chart) (*repo.ChartVersion, error) {
	versions, err := r.FetchChartVersions(chartspec)
	if err != nil {
		return nil, err
	}

	if len(versions) == 0 {
		return nil, repo.ErrNoChartVersion
	}

	var highestver *repo.ChartVersion
	var lasterr error

	for _, ver := range versions {
		if _, lasterr = r.LoadCached(ver); lasterr == nil {
			highestver = ver
			break
		}
		if _, lasterr = r.FetchRemote(ver); lasterr == nil {
			highestver = ver
			break
		}
	}
	if highestver == nil {
		if lasterr == nil {
			lasterr = repo.ErrNoChartVersion
		}
		return nil, shippererrors.NewChartVersionResolveError(
			chartspec,
			lasterr,
		)
	}

	return highestver, nil
}

func (r *Repo) FetchChartVersions(chartspec *shipper.Chart) (repo.ChartVersions, error) {

	<-r.indexResolved

	vs, ok := r.index.Load().(*repo.IndexFile).Entries[chartspec.Name]
	if !ok {
		return nil, repo.ErrNoChartName
	}
	if len(vs) == 0 {
		return nil, repo.ErrNoChartVersion
	}

	var constraint *semver.Constraints
	if len(chartspec.Version) == 0 {
		constraint, _ = semver.NewConstraint("*")
	} else {
		var err error
		constraint, err = semver.NewConstraint(chartspec.Version)
		if err != nil {
			return nil, shippererrors.NewBrokenChartSpecError(
				chartspec,
				err,
			)
		}
	}

	versions := make([]*repo.ChartVersion, 0, len(vs))
	for _, ver := range vs {
		test, err := semver.NewVersion(ver.Version)
		if err != nil {
			continue
		}
		if !constraint.Check(test) {
			continue
		}
		versions = append(versions, ver)
	}

	return versions, nil
}

func (r *Repo) LoadCached(cv *repo.ChartVersion) (*chart.Chart, error) {
	filename := chart2file(cv)
	data, err := r.cache.Fetch(filename)
	if err != nil {
		return nil, err
	}

	c, err := loadChartData(data)
	if err != nil {
		return nil, shippererrors.NewBrokenChartVersionError(
			cv,
			err,
		)
	}

	return c, nil
}

func (r *Repo) FetchRemote(cv *repo.ChartVersion) (*chart.Chart, error) {
	if cv == nil {
		return nil, shippererrors.NewBrokenChartVersionError(
			cv,
			fmt.Errorf("chart version is nil, can not proceed"),
		)
	}
	if len(cv.URLs) == 0 {
		return nil, shippererrors.NewBrokenChartVersionError(
			cv,
			fmt.Errorf("chart %q has no downloadable URLs", cv.Name),
		)
	}

	// copy-paste from Helm's chart_downloader.go
	chartURL, err := url.Parse(cv.URLs[0])
	if err != nil {
		return nil, shippererrors.NewBrokenChartVersionError(
			cv,
			fmt.Errorf("invalid chart URL format: %v", cv.URLs[0]),
		)
	}

	// If the URL is relative (no scheme), prepend the chart repo's base URL
	if !chartURL.IsAbs() {
		repoURL, err := url.Parse(r.repoURL)
		if err != nil {
			return nil, err
		}
		query := repoURL.Query()

		// We need a trailing slash for ResolveReference to work, but make sure there isn't already one
		repoURL.Path = strings.TrimSuffix(repoURL.Path, "/") + "/"
		chartURL = repoURL.ResolveReference(chartURL)
		chartURL.RawQuery = query.Encode()
	}

	url := chartURL.String()
	data, err := r.fetcher(url)
	if err != nil {
		return nil, err
	}

	chart, err := loadChartData(data)
	if err != nil {
		return nil, shippererrors.NewChartDataCorruptionError(cv, err)
	}

	filename := chart2file(cv)
	if err := r.cache.Store(filename, data); err != nil {
		return nil, shippererrors.NewChartRepoInternalError(err)
	}

	return chart, nil
}

func (r *Repo) Fetch(chartspec *shipper.Chart) (*chart.Chart, error) {
	versions, err := r.FetchChartVersions(chartspec)
	if err != nil {
		return nil, err
	}
	var chartver *repo.ChartVersion
	for _, ver := range versions {
		if ver.Version == chartspec.Version {
			chartver = ver
			break
		}
	}
	if chartver == nil {
		return nil, repo.ErrNoChartVersion
	}

	if chart, err := r.LoadCached(chartver); err == nil {
		return chart, nil
	}

	return r.FetchRemote(chartver)
}

func loadIndexData(data []byte) (*repo.IndexFile, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("no index content")
	}

	i := &repo.IndexFile{}
	if err := yaml.Unmarshal(data, i); err != nil {
		return nil, err
	}

	i.SortEntries()
	if i.APIVersion == "" {
		// do not support pre-v2.0.0
		return i, repo.ErrNoAPIVersion
	}

	return i, nil
}

func loadChartData(data []byte) (*chart.Chart, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("no body content")
	}

	return chartutil.LoadArchive(bytes.NewBuffer(data))

}

func url2name(v string) string {
	// https-github.com-chartmuseum-helm-push
	v = strings.Replace(v, "://", "-", -1)
	v = strings.Replace(v, "/", "-", -1)
	v = strings.Replace(v, string(filepath.Separator), "-", -1)

	return v
}

func chart2file(cv *repo.ChartVersion) string {
	name, version := cv.GetName(), cv.GetVersion()
	name = strings.Replace(name, "/", "-", -1)
	version = strings.Replace(version, "/", "-", -1)

	return fmt.Sprintf("%s-%s.tgz", name, version)
}
