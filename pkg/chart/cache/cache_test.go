package chart

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

const (
	tenMb            = 10 * 1024 * 1024
	testCacheDir     = "cache_test"
	testChartName    = "test-chart"
	testChartRepo    = "charts.example.com:8889/charts"
	testChartVersion = "0.0.1"
)

func TestStoreFetch(t *testing.T) {
	storedData := "foobar"
	cache := NewFilesystemCache(testCacheDir, tenMb)
	defer cache.Clean()
	err := cache.Store([]byte(storedData), testChartRepo, testChartName, testChartVersion)
	if err != nil {
		t.Fatalf("failed to store %s/%s-%s: %q", testChartRepo, testChartName, testChartVersion, err)
	}

	fetchedData, err := cache.Fetch(testChartRepo, testChartName, testChartVersion)
	if err != nil {
		t.Fatalf("failed to store: %s/%s-%s: %q", testChartRepo, testChartName, testChartVersion, err)
	}
	if fetchedData.String() != storedData {
		t.Fatalf(
			"fetched %s/%s-%s but got wrong contents: expected %q but got %q",
			testChartRepo, testChartName, testChartVersion, storedData, fetchedData,
		)
	}
}

func TestIndividualSizeLimit(t *testing.T) {
	fiveByteData := []byte("abcde")
	cache := NewFilesystemCache(testCacheDir, 4)
	defer cache.Clean()
	err := cache.Store(fiveByteData, testChartRepo, testChartName, testChartVersion)
	if err == nil {
		t.Fatalf("expected a Size Exceeded error from trying to store a 5 byte chart into a 4 byte-per-family cache, but got nil")
	}
}

func TestCacheNameSanitization(t *testing.T) {
	data := []byte("abcde")
	cache := NewFilesystemCache(testCacheDir, tenMb)
	defer cache.Clean()
	// create a 'repoURL' like /foo/bar
	// without cleaning this will create a cache directory like:
	// cachedir/foo/bar/test-chart/test-chart-0.0.1.tgz
	for _, repoURL := range []string{"http://blorg", filepath.Join("foo", "bar")} {
		err := cache.Store(data, repoURL, testChartName, testChartVersion)
		if err != nil {
			t.Fatalf("failed to store a chart which needed some repoURL cleaning")
		}

		fetched, err := cache.Fetch(repoURL, testChartName, testChartVersion)
		if err != nil {
			t.Fatalf("failed to fetch a chart which needed some repoURL cleaning")
		}

		if fetched.String() != string(data) {
			t.Fatalf("cleaned repoURL fetched chart different from stored data")
		}

		unsanitizedPath := filepath.Join(testCacheDir, repoURL)
		if _, err = os.Stat(unsanitizedPath); err == nil {
			t.Fatalf("found a path at %q where there should be no path if santiziation is working", unsanitizedPath)
		}

		if !os.IsNotExist(err) {
			t.Fatalf("got some weird error stat-ing the unsanitizedPath: %v", err)
		}
	}
}

func TestCacheEviction(t *testing.T) {
	// Two existing charts totaling 8 bytes. Want to add a 4 byte chart which would
	// exceed 10 byte limit. Delete one chart to make room.
	charts := [][]byte{
		[]byte("abcd"),
		[]byte("efgh"),
		[]byte("ijkl"),
	}
	cache := NewFilesystemCache(testCacheDir, 10)
	defer cache.Clean()
	for i, data := range charts {
		version := strconv.Itoa(i)
		err := cache.Store(charts[i], testChartRepo, testChartName, version)
		if err != nil {
			t.Fatalf("failed to store test data for cache eviction test: version %s: %q", version, err)
		}
		fetched, err := cache.Fetch(testChartRepo, testChartName, version)
		if err != nil {
			t.Fatalf("failed to fetch stored chart version %s immediately after storing: %q", version, err)
		}
		if fetched.String() != string(data) {
			t.Fatalf("fetched data wasn't the same as the stored! yikes! expected %s got %s", string(data), fetched.String())
		}
	}

	evictedVersion := 0
	shouldHaveBeenEvicted, err := cache.Fetch(testChartRepo, testChartName, strconv.Itoa(evictedVersion))
	if err != nil {
		t.Fatalf("failed to fetch chart which should have been evicted: %q", err)
	}

	if shouldHaveBeenEvicted != nil {
		t.Errorf(
			"chart %s/%s-%s should have been evicted, but it seems to still be around: %s",
			testChartRepo, testChartName, strconv.Itoa(evictedVersion), shouldHaveBeenEvicted,
		)
	}

	survivingVersion := 1
	shouldStillBePresent, err := cache.Fetch(testChartRepo, testChartName, strconv.Itoa(survivingVersion))
	if err != nil {
		t.Fatalf("fetch chart which should still be present after eviction: %q", err)
	}

	if shouldStillBePresent == nil {
		t.Fatal("chart which should still be present after eviction is NOT present")
	}

	if shouldStillBePresent.String() != string(charts[survivingVersion]) {
		t.Errorf(
			"chart %s/%s-%s correctly survived eviction, but the contents are wrong! expected %s got %s",
			testChartRepo, testChartName, strconv.Itoa(evictedVersion), string(charts[survivingVersion]), shouldStillBePresent.String(),
		)
	}
}
