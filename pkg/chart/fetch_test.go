package chart

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"k8s.io/helm/pkg/repo/repotest"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

const (
	cacheDirName          = "chart-cache"
	testFetchChartName    = "my-complex-app"
	testFetchChartVersion = "0.2.0"
	testFetchChartRepoURL = "localhost"
	tenMb                 = 10 * 1024 * 1024
)

var cache = filepath.Join("testdata", cacheDirName)

func TestFetchRemote(t *testing.T) {
	fetch := FetchRemote()
	srv, hh, err := repotest.NewTempServer("testdata/*.tgz")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		srv.Stop()
		os.RemoveAll(hh.String())
	}()

	inChart := shipper.Chart{
		Name:    testFetchChartName,
		Version: testFetchChartVersion,
		RepoURL: srv.URL(),
	}

	_, err = fetch(inChart)
	if err != nil {
		t.Fatal(err)
	}
}

// TestFetchCacheNoRemote tests fetching a chart from a pre-populated cache with
// no server available.
func TestFetchCacheNoRemote(t *testing.T) {
	_ = os.RemoveAll(cache)
	defer func() {
		_ = os.RemoveAll(cache)
	}()

	filename := fmt.Sprintf("%s-%s.tgz", testFetchChartName, testFetchChartVersion)
	chartFamilyPath := filepath.Join(cache, testFetchChartRepoURL, testFetchChartName)
	err := os.MkdirAll(chartFamilyPath, 0755)
	if err != nil {
		t.Fatal(err)
	}

	srcData, err := ioutil.ReadFile(filepath.Join("testdata", filename))
	if err != nil {
		t.Fatal(err)
	}
	err = ioutil.WriteFile(filepath.Join(chartFamilyPath, filename), srcData, 0644)
	if err != nil {
		t.Fatal(err)
	}

	fetch := FetchRemoteWithCache(cache, tenMb)
	inChart := shipper.Chart{
		Name:    testFetchChartName,
		Version: testFetchChartVersion,
		RepoURL: testFetchChartRepoURL,
	}

	_, err = fetch(inChart)
	if err != nil {
		t.Fatal(err)
	}
}

// TestFetchCacheRemoteGoneAway tests fetching a chart from a remote, caching
// it, then terminating the remote and ensuring the chart is still around.
func TestFetchCacheRemoteGoneAway(t *testing.T) {
	_ = os.RemoveAll(cache)
	defer func() {
		_ = os.RemoveAll(cache)
	}()

	fetch := FetchRemoteWithCache(cache, tenMb)

	srv, hh, err := repotest.NewTempServer("testdata/*.tgz")
	if err != nil {
		t.Fatal(err)
	}

	inChart := shipper.Chart{
		Name:    testFetchChartName,
		Version: testFetchChartVersion,
		RepoURL: srv.URL(),
	}

	remoteChart, err := fetch(inChart)
	if err != nil {
		t.Fatal(err)
	}

	// Terminate the server and fetch again: it should work, and it should be the
	// same.
	srv.Stop()
	os.RemoveAll(hh.String())

	cachedChart, err := fetch(inChart)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(remoteChart, cachedChart) {
		t.Fatalf("remote chart and cached chart are not the same: %v and %v", remoteChart, cachedChart)
	}
}
