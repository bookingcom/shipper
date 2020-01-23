package repo

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	// Importing this yaml package is a very crucial point:
	// the "classical" yaml.v2 does not understand json annotations
	// in structure definitions and therefore always parses empty
	// index structures. This version is patched to understand json
	// annotations and works fine.
	"sigs.k8s.io/yaml"

	"github.com/Masterminds/semver"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/helm/pkg/repo"
	"k8s.io/klog"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
)

const (
	RepoIndexRefreshPeriod = 10 * time.Second
	RepoFetchIndexTimeout  = 2 * time.Second
)

var ErrFetchNoResponseYet = errors.New("no response from chart repo yet")

type Repo struct {
	repoURL  string
	indexURL string
	cache    Cache
	fetcher  RemoteFetcher
	mutex    sync.RWMutex
	index    *repo.IndexFile
	lastErr  error
	resolved chan struct{}
	once     sync.Once
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

	r := &Repo{
		repoURL:  repoURL,
		indexURL: indexURL,
		cache:    cache,
		fetcher:  fetcher,
		resolved: make(chan struct{}),
	}

	return r, nil
}

func (r *Repo) Start(stopCh <-chan struct{}) {
	wait.Until(func() {
		if err := r.refreshIndex(); err != nil {
			klog.Errorf("failed to refresh repo %q index: %s", r.repoURL, err)
		}
	}, RepoIndexRefreshPeriod, stopCh)
}

func (r *Repo) refreshIndex() error {
	var data []byte
	var err error
	var index *repo.IndexFile

	data, err = r.fetcher(r.indexURL)
	if err != nil {
		_, cacheErr := r.cache.Fetch("index.yaml")
		if cacheErr != nil {
			multiError := shippererrors.NewMultiError()
			multiError.Append(
				shippererrors.NewChartRepoIndexError(
					fmt.Errorf("failed to fetch %q: %v", r.indexURL, err),
				))
			multiError.Append(
				shippererrors.NewNoCachedChartRepoIndexError(
					fmt.Errorf("failed to fetch %q: %v", r.indexURL, cacheErr),
				))
			err = multiError
			goto AtomicSave
		}
		err = shippererrors.NewChartRepoIndexError(
			fmt.Errorf("failed to fetch %q: %v", r.indexURL, err),
		)
		goto AtomicSave
	}

	index, err = loadIndexData(data)
	if err != nil {
		err = shippererrors.NewChartRepoIndexError(
			fmt.Errorf("failed to load index file: %v", err),
		)
		goto AtomicSave
	}

	if r.index != nil {
		if len(r.index.Entries) != 0 && len(index.Entries) == 0 {
			err = shippererrors.NewChartRepoIndexError(
				fmt.Errorf("the new index contains no entries whereas the previous fetch returned a non-empty result"),
			)
			goto AtomicSave
		}
	}

	// marking the repo index as at-least-once-resolved
	r.once.Do(func() {
		close(r.resolved)
	})

AtomicSave:
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.lastErr = err
	if err == nil {
		r.index = index
	}

	return err
}

func (r *Repo) ResolveVersion(chartspec *shipper.Chart) (*repo.ChartVersion, error) {
	versions, err := r.FetchChartVersions(chartspec)
	if err != nil {
		return nil, err
	}

	if len(versions) == 0 {
		return nil, shippererrors.NewChartVersionResolveError(chartspec, repo.ErrNoChartVersion)
	}

	return versions[0], nil
}

func (r *Repo) FetchChartVersions(chartspec *shipper.Chart) (repo.ChartVersions, error) {

	select {
	case <-r.resolved:
	case <-time.After(RepoFetchIndexTimeout):
		// fresh repo returns this error until it gets resolved
		return nil, shippererrors.NewNoCachedChartRepoIndexError(ErrFetchNoResponseYet)
	}

	r.mutex.RLock()
	defer r.mutex.RUnlock()

	if r.index == nil {
		return nil, r.lastErr
	}

	vs, ok := r.index.Entries[chartspec.Name]
	if !ok {
		return nil, shippererrors.NewChartVersionResolveError(chartspec, repo.ErrNoChartName)
	}
	if len(vs) == 0 {
		return nil, shippererrors.NewChartVersionResolveError(chartspec, repo.ErrNoChartVersion)
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
		chart, convErr := newChart(cv)
		if convErr != nil {
			return nil, shippererrors.NewChartRepoInternalError(convErr)
		}
		return nil, shippererrors.NewChartFetchFailureError(chart, err)
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

	maxIx := len(versions)
	ix := sort.Search(maxIx, func(i int) bool {
		return versions[i].Version <= chartspec.Version
	})

	if ix == maxIx { // nothing found
		return nil, shippererrors.NewChartVersionResolveError(chartspec, repo.ErrNoChartVersion)
	}

	chartver := versions[ix]

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
		return nil, repo.ErrNoAPIVersion
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

func newChart(cv *repo.ChartVersion) (*shipper.Chart, error) {
	if len(cv.URLs) < 1 {
		return nil, fmt.Errorf("chart version is missing URLs")
	}
	return &shipper.Chart{
		Name:    cv.GetName(),
		Version: cv.GetVersion(),
		RepoURL: cv.URLs[0],
	}, nil
}
