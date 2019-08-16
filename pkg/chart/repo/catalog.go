package repo

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"path/filepath"
	"sync"

	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	"github.com/bookingcom/shipper/pkg/metrics/instrumentedclient"
)

type RemoteFetcher func(url string) ([]byte, error)

func DefaultRemoteFetcher(url string) ([]byte, error) {
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

type CacheFactory func(name string) (Cache, error)

func DefaultFileCacheFactory(cacheDir string) CacheFactory {
	return func(name string) (Cache, error) {
		return NewFilesystemCache(
			filepath.Join(cacheDir, name),
			0, // TODO set limit
		)
	}
}

type Catalog struct {
	factory CacheFactory
	repos   map[string]*Repo
	fetcher RemoteFetcher
	stopCh  <-chan struct{}
	sync.Mutex
}

func NewCatalog(factory CacheFactory, fetcher RemoteFetcher, stopCh <-chan struct{}) *Catalog {
	return &Catalog{
		factory: factory,
		repos:   make(map[string]*Repo),
		fetcher: fetcher,
		stopCh:  stopCh,
	}
}

func (c *Catalog) CreateRepoIfNotExist(repoURL string) (*Repo, error) {
	if _, err := url.ParseRequestURI(repoURL); err != nil {
		return nil, shippererrors.NewChartRepoInternalError(err)
	}

	c.Lock()
	defer c.Unlock()

	name := url2name(repoURL)
	repo, ok := c.repos[name]
	if !ok {
		cache, err := c.factory(name)
		if err != nil {
			return nil, shippererrors.NewChartRepoInternalError(
				fmt.Errorf("failed to create cache: %v", err),
			)
		}
		repo, err = NewRepo(repoURL, cache, c.fetcher)
		if err != nil {
			return nil, err
		}
		c.repos[name] = repo
		go repo.Start(c.stopCh)
	}

	return repo, nil
}
