package chart

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"

	"github.com/golang/glog"

	"k8s.io/helm/pkg/chartutil"
	helmchart "k8s.io/helm/pkg/proto/hapi/chart"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	chartcache "github.com/bookingcom/shipper/pkg/chart/cache"
	"github.com/bookingcom/shipper/pkg/metrics/instrumentedclient"
)

type FetchFunc func(shipperv1.Chart) (*helmchart.Chart, error)

// 5mb limit per chart family (all versions of a given chart)
// a large chart with many objects (but no big bundled files) is 20kb -> 256 versions
// this fits ~2k distinct charts into 10gb of disk
const DefaultCacheLimit = 5 * 1024 * 1024

func FetchRemoteWithCache(dir string, perChartFamilyByteLimit int) FetchFunc {
	cache := chartcache.NewFilesystemCache(dir, perChartFamilyByteLimit)
	return func(chart shipperv1.Chart) (*helmchart.Chart, error) {
		cachedChart, err := cache.Fetch(chart.RepoURL, chart.Name, chart.Version)
		if err != nil {
			// There's a good case to make that it would be better to log and download
			return nil, err
		}

		if cachedChart != nil && cachedChart.Len() > 0 {
			return chartutil.LoadArchive(cachedChart)
		}

		// 0 bytes returned -> no cache hit. download it
		data, err := downloadChart(chart.RepoURL, chart.Name, chart.Version)
		if err != nil {
			return nil, err
		}

		// we didn't find it in the cache earlier and had to fall through to downloading, so write it to the cache
		err = cache.Store(data, chart.RepoURL, chart.Name, chart.Version)
		if err != nil {
			return nil, err
		}

		return chartutil.LoadArchive(bytes.NewReader(data))
	}
}

func FetchRemote() FetchFunc {
	return func(chart shipperv1.Chart) (*helmchart.Chart, error) {
		data, err := downloadChart(chart.RepoURL, chart.Name, chart.Version)
		if err != nil {
			return nil, err
		}
		return chartutil.LoadArchive(bytes.NewReader(data))
	}
}

func downloadChart(repoURL, name, version string) ([]byte, error) {
	u, err := url.Parse(repoURL)
	if err != nil {
		return nil, err
	}

	u.Path = fmt.Sprintf("%s/%s-%s.tgz", u.Path, name, version)
	glog.V(10).Infof("trying to download %s", u)
	resp, err := instrumentedclient.Get(u.String()) // TODO retry
	if err != nil {
		return nil, err
	}

	defer func() {
		err = resp.Body.Close()
		if err != nil {
			glog.V(2).Infof("error closing resp.Body from chart repo: %s", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		// TODO log body
		return nil, fmt.Errorf("download %s: %d", u, resp.StatusCode)
	}

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("0 byte response fetching %s-%s/%s", repoURL, name, version)
	}
	return data, nil
}
