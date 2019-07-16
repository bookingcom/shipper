package repo

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/url"
	"path"
	"strings"
	"testing"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

const (
	IndexYamlResp = `
---
apiVersion: v1
entries:
  simple:
    - created: 2016-10-06T16:23:20.499814565-06:00
      description: A super simple chart 
      digest: 99c76e403d752c84ead610644d4b1c2f2b453a74b921f422b9dcb8a7c8b559cd
      home: https://k8s.io/helm
      name: simple
      sources:
      - https://github.com/helm/helm
      urls:
      - https://charts.example.com/simple-0.0.1.tgz
      version: 0.0.1
  nginx:
    - created: 2016-10-06T16:23:20.499543808-06:00
      description: Create a basic nginx HTTP server
      digest: aaff4545f79d8b2913a10cb400ebb6fa9c77fe813287afbacf1a0b897cdffffff
      home: https://k8s.io/helm
      name: nginx
      sources:
      - https://github.com/helm/charts
      urls:
      - https://charts.example.com/nginx-0.0.1.tgz
      version: 0.0.1
    - created: 2016-10-06T16:23:20.499543808-06:00
      description: Create a basic nginx HTTP server
      digest: aaff4545f79d8b2913a10cb400ebb6fa9c77fe813287afbacf1a0b897cdffffff
      home: https://k8s.io/helm
      name: nginx
      sources:
      - https://github.com/helm/charts
      urls:
      - https://charts.example.com/nginx-0.0.2.tgz
      version: 0.0.2
    - created: 2016-10-06T16:23:20.499543808-06:00
      description: Create a basic nginx HTTP server
      digest: aaff4545f79d8b2913a10cb400ebb6fa9c77fe813287afbacf1a0b897cdffffff
      home: https://k8s.io/helm
      name: nginx
      sources:
      - https://github.com/helm/charts
      urls:
      - https://charts.example.com/nginx-0.0.3.tgz
      version: 0.0.3
  non-existing:
    - created: 2016-10-06T16:23:20.499543808-06:00
      description: This chart does not really exist
      digest: aaff4545f79d8b2913a10cb400ebb6fa9c77fe813287afbacf1a0b897cdffffff
      home: https://k8s.io/helm
      name: non-existing
      sources:
      - https://github.com/helm/charts
      urls:
      - https://charts.example.com/non-existing-0.0.1.tgz
      version: 0.0.1
    - created: 2016-10-06T16:23:20.499543808-06:00
      description: This chart does not really exist
      digest: aaff4545f79d8b2913a10cb400ebb6fa9c77fe813287afbacf1a0b897cdffffff
      home: https://k8s.io/helm
      name: non-existing
      sources:
      - https://github.com/helm/charts
      urls:
      - https://charts.example.com/non-existing-0.0.2.tgz
      version: 0.0.2
generated: 2016-10-06T16:23:20.499029981-06:00
`

	repoURL = "https://registry.example.com/charts"
)

func localFetch(t *testing.T) func(string) ([]byte, error) {
	return func(requrl string) ([]byte, error) {
		if strings.HasSuffix(requrl, ".yaml") {
			return []byte(IndexYamlResp), nil
		}
		u, err := url.Parse(requrl)
		if err != nil {
			return nil, err
		}
		filename := path.Base(u.Path)
		data, err := ioutil.ReadFile(path.Join("testdata", filename))
		if err != nil {
			return nil, fmt.Errorf("failed to read file %q: %s", filename, err)
		}
		return data, nil
	}
}

func TestRefreshIndex(t *testing.T) {
	tests := []struct {
		name             string
		fetchBody        string
		fetchErr         error
		repoURL          string
		expectedFetchURL string
		expectedErr      error
	}{
		{
			name:             "Plain fetch",
			fetchBody:        IndexYamlResp,
			fetchErr:         nil,
			repoURL:          repoURL,
			expectedFetchURL: repoURL + "/index.yaml",
			expectedErr:      nil,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			var fetchedURL string

			cache := NewTestCache(testCase.name)

			repo := NewRepo(
				testCase.repoURL,
				cache,
				func(url string) ([]byte, error) {
					fetchedURL = url
					return []byte(testCase.fetchBody), testCase.fetchErr
				},
			)

			_, err := repo.RefreshIndex()

			if !equivalent(err, testCase.expectedErr) {
				t.Fatalf("Unexpected error: %q, want: %q", err, testCase.expectedErr)
			}

			if fetchedURL != testCase.expectedFetchURL {
				t.Fatalf("Unexpected fetch URL: %q, want: %q", fetchedURL, testCase.expectedFetchURL)
			}
		})
	}
}

func TestResolveVersion(t *testing.T) {
	tests := []struct {
		name      string
		chartname string
		verspec   string
		wantver   string
		wanterr   error
	}{
		{
			"Existing single version maj min and patch are provided",
			"simple",
			"0.0.1",
			"0.0.1",
			nil,
		},
		{
			"Existing single version comp function applied (exact maj version provided)",
			"simple",
			">=0.0.1",
			"0.0.1",
			nil,
		},
		{
			"Existing single version tilde function applied (different maj and min provided)",
			"simple",
			"~0.0.1",
			"0.0.1",
			nil,
		},
		{
			"Existing single version, no match",
			"simple",
			"=1.0.0",
			"",
			errors.New("no chart version found"),
		},
		{
			"Existing dual version exact match",
			"nginx",
			"=0.0.1",
			"0.0.1",
			nil,
		},
		{
			"Existing dual version >= function applied",
			"nginx",
			">=0.0.1",
			"0.0.2",
			nil,
		},
		{
			"Existing dual version > function applied",
			"nginx",
			">0.0.1",
			"0.0.2",
			nil,
		},
		{
			"Existing dual version <= function applied",
			"nginx",
			"<=0.0.2",
			"0.0.2",
			nil,
		},
		{
			"Existing dual version < function applied",
			"nginx",
			"<0.0.2",
			"0.0.1",
			nil,
		},
		{
			"Failed fetch version downgrade to a lower one",
			"nginx",
			"<=0.0.3",
			"0.0.2",
			nil,
		},
		{
			"Failed fetch version exact match",
			"nginx",
			"0.0.3",
			"",
			fmt.Errorf("failed to resolve chart version [name: \"nginx\", version: \"0.0.3\", repo: \"https://charts.example.com\"]: failed to read file \"nginx-0.0.3.tgz\": open testdata/nginx-0.0.3.tgz: no such file or directory"),
		},
		{
			"Faield fetch version a lower one is in the cache",
			"non-existing",
			"<=0.0.2",
			"0.0.1",
			nil,
		},
		{
			"Failed fetch version exact match with lower version available in cache",
			"non-existing",
			"0.0.2",
			"",
			fmt.Errorf("failed to resolve chart version [name: \"non-existing\", version: \"0.0.2\", repo: \"https://charts.example.com\"]: failed to read file \"non-existing-0.0.2.tgz\": open testdata/non-existing-0.0.2.tgz: no such file or directory"),
		},
	}

	t.Parallel()

	cache := NewTestCache("test-cache")
	data, err := ioutil.ReadFile("testdata/simple-0.0.1.tgz")
	if err != nil {
		t.Fatalf(err.Error())
	}
	cache.Store("non-existing-0.0.1.tgz", data)

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			repo := NewRepo(
				"https://charts.example.com",
				cache,
				localFetch(t),
			)
			if _, err := repo.RefreshIndex(); err != nil {
				t.Fatalf(err.Error())
			}
			chartspec := &shipper.Chart{
				Name:    testCase.chartname,
				Version: testCase.verspec,
				RepoURL: repo.url,
			}
			gotcv, goterr := repo.ResolveVersion(chartspec)

			if !equivalent(goterr, testCase.wanterr) {
				t.Fatalf("unexpected error: %s, want: %s", goterr, testCase.wanterr)
			}

			if goterr != nil {
				return
			}

			if gotcv.Metadata.Version != testCase.wantver {
				t.Fatalf("unexpected version: %s, want: %s", gotcv.Metadata.Version, testCase.wantver)
			}
		})
	}
}

func TestFetch(t *testing.T) {
	tests := []struct {
		name      string
		chartname string
		chartver  string
		wantname  string
		wantver   string
		wanterr   error
	}{
		{
			"Existing chart successful fetch",
			"nginx",
			"0.0.1",
			"nginx",
			"0.0.1",
			nil,
		},
		{
			"Unknown chart name",
			"unknown",
			"0.0.1",
			"",
			"",
			fmt.Errorf("no chart name found"),
		},
		{
			"Non-existing chart",
			"nginx",
			"10.20.30",
			"nginx",
			"",
			fmt.Errorf("no chart version found"),
		},
		{
			"Fails to fetch but exists in the cache",
			"non-existing",
			"0.0.1",
			"simple",
			"0.0.1",
			nil,
		},
		{
			"Fails to fetch specified version but lower one is cached",
			"non-existing",
			"0.0.2",
			"",
			"",
			fmt.Errorf("failed to read file \"non-existing-0.0.2.tgz\": open testdata/non-existing-0.0.2.tgz: no such file or directory"),
		},
	}

	t.Parallel()

	data, err := ioutil.ReadFile("testdata/simple-0.0.1.tgz")
	if err != nil {
		t.Fatalf("failed to read sample chart: %s", err)
	}
	cache := NewTestCache("test-cache")
	cache.Store("non-existing-0.0.1.tgz", data)

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			repo := NewRepo(
				"https://chart.example.com",
				cache,
				localFetch(t),
			)
			if _, err := repo.RefreshIndex(); err != nil {
				t.Fatalf(err.Error())
			}

			chartspec := &shipper.Chart{
				Name:    testCase.chartname,
				Version: testCase.chartver,
				RepoURL: repo.url,
			}

			chart, err := repo.Fetch(chartspec)
			if !equivalent(err, testCase.wanterr) {
				t.Fatalf("unexpected error: %s, want: %s", err, testCase.wanterr)
			}

			if err != nil {
				return
			}

			if chart.Metadata.Name != testCase.wantname {
				t.Fatalf("unexpected chart name: %s, want: %s", chart.Metadata.Name, testCase.chartname)
			}

			if chart.Metadata.Version != testCase.wantver {
				t.Fatalf("unexpected chart version: %s, want: %s", chart.Metadata.Version, testCase.wantver)
			}
		})
	}
}

func equivalent(err1, err2 error) bool {
	if err1 == nil && err2 == nil {
		return true
	}
	if err1 != nil && err2 != nil {
		return err1.Error() == err2.Error()
	}
	return false
}
