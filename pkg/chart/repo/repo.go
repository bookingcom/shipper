package repo

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/bookingcom/shipper/pkg/metrics/instrumentedclient"
	yaml "gopkg.in/yaml.v2"
	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/helm/pkg/repo"
)

var (
	ErrInvalidConstraint = errors.New("invalid constraint")
)

type CacheFabric func(name string) (Cache, error)

type Catalog struct {
	fabric CacheFabric
	repos  map[string]*Repo
	sync.Mutex
}

func NewCatalog(fabric CacheFabric) *Catalog {
	return &Catalog{fabric: fabric}
}

func (c *Catalog) CreateRepoIfNotExist(repoURL string) (*Repo, error) {
	if _, err := url.Parse(repoURL); err != nil {
		return nil, fmt.Errorf("invalid URL: %v", err)
	}

	c.Lock()
	defer c.Unlock()

	name := url2name(repoURL)
	repo, ok := c.repos[name]
	if !ok {
		cache, err := c.fabric(name)
		if err != nil {
			return nil, fmt.Errorf("failed to create cache: %v", err)
		}

		repo = &Repo{url: repoURL, cache: cache}
		c.repos[name] = repo
	}

	return repo, nil
}

type Repo struct {
	url   string
	cache Cache
	sync.Mutex
}

func (r *Repo) RefreshIndex() error {
	r.Lock()
	defer r.Unlock()
	// relying on instrumentedclient's HTTP client to not stuck forever

	parsed, _ := url.Parse(r.url) // URL is validated before
	parsed.Path = path.Join(parsed.Path, "index.yaml")
	indexURL := parsed.String()

	data, err := fetch(indexURL)
	if err != nil {
		return fmt.Errorf("failed to fetch %q: %v", indexURL, err)
	}

	if _, err := loadIndex(data); err != nil {
		return fmt.Errorf("failed to load index file: %v", err)
	}

	if err := r.cache.Store("index.yaml", data); err != nil {
		return fmt.Errorf("failed to cache index.yaml: %v", err)
	}

	return nil
}

func (r *Repo) ResolveRange(chart, version string) (*repo.ChartVersion, error) {
	data, err := r.cache.Fetch("index.yaml")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch index.yaml from cache: %v", err)
	}

	index, err := loadIndex(data)
	if err != nil {
		return nil, fmt.Errorf("failed to load index file: %v", err)
	}

	cv, err := index.Get(chart, version)
	if strings.Contains(err.Error(), "constraint Parser Error") {
		return nil, ErrInvalidConstraint
	} else if strings.Contains(err.Error(), "improper constraint") {
		return nil, ErrInvalidConstraint
	}

	return cv, err
}

func (r *Repo) Fetch(cv *repo.ChartVersion) (*chart.Chart, error) {
	if len(cv.URLs) == 0 {
		return nil, fmt.Errorf("chart %q has no downloadable URLs", cv.Name)
	}

	// copy-paste from Helm's chart_downloader.go
	chartURL, err := url.Parse(cv.URLs[0])
	if err != nil {
		return nil, fmt.Errorf("invalid chart URL format: %v", cv.URLs[0])
	}

	// If the URL is relative (no scheme), prepend the chart repo's base URL
	if !chartURL.IsAbs() {
		repoURL, _ := url.Parse(r.url) // URL is validated before
		query := repoURL.Query()

		// We need a trailing slash for ResolveReference to work, but make sure there isn't already one
		repoURL.Path = strings.TrimSuffix(repoURL.Path, "/") + "/"
		chartURL = repoURL.ResolveReference(chartURL)
		chartURL.RawQuery = query.Encode()
	}

	url := chartURL.String()
	data, err := fetch(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %q: %v", url, err)
	}

	chart, err := loadChart(data)
	if err != nil {
		return nil, err
	}

	filename := chart2file(cv.Name, cv.Version)
	if err := r.cache.Store(filename, data); err != nil {
		return nil, fmt.Errorf("failed to cache chart %s: %v", filename, err)
	}

	return chart, nil
}

func (r *Repo) FetchIfNotCached(chart, version string) (*chart.Chart, error) {
	filename := chart2file(chart, version)
	data, err := r.cache.Fetch(filename)
	if err == nil {
		return loadChart(data)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to fetch %q from cache: %v", filename, err)
	}

	cv, err := r.ResolveRange(chart, version)
	if err == ErrInvalidConstraint {
		return nil, fmt.Errorf("failed to resolve chart %s %s in repo index: %v", chart, version, err)
	} else if err != nil {
		if err := r.RefreshIndex(); err != nil {
			return nil, fmt.Errorf("failed to refresh index: %v", err)
		}

		cv, err = r.ResolveRange(chart, version)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve chart %s %s in repo index: %v", chart, version, err)
		}
	}

	return r.Fetch(cv)
}

func fetch(url string) ([]byte, error) {
	resp, err := instrumentedclient.Get(url)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unsuccessful response code: %s (%d)", resp.Status, resp.StatusCode)
	}

	return ioutil.ReadAll(resp.Body)
}

func loadIndex(data []byte) (*repo.IndexFile, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty content")
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

func loadChart(data []byte) (*chart.Chart, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty content")
	}

	buf := bytes.NewBuffer(data)
	return chartutil.LoadArchive(buf)
}

func url2name(v string) string {
	// https-github.com-chartmuseum-helm-push
	v = strings.Replace(v, "://", "-", -1)
	v = strings.Replace(v, "/", "-", -1)
	v = strings.Replace(v, string(filepath.Separator), "-", -1)
	return v
}

func chart2file(name, version string) string {
	name = strings.Replace(name, "/", "-", -1)
	version = strings.Replace(version, "/", "-", -1)
	return fmt.Sprintf("%s-%s.tgz", name, version)
}
