package chart

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/helm/helmpath"
	"k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/helm/pkg/repo/repotest"
	"k8s.io/helm/pkg/timeconv"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
)

func TestDownloadChart(t *testing.T) {
	tmp, err := ioutil.TempDir("", "shipper-api-downloadchart-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)

	// Separate directory creation from downloadChart function
	hh := helmpath.Home(tmp)
	os.MkdirAll(hh.Cache(), 0755)

	srv := repotest.NewServer(tmp)
	defer srv.Stop()
	if _, err := srv.CopyCharts("testdata/*.tgz"); err != nil {
		t.Error(err)
		return
	}
	if err := srv.LinkIndices(); err != nil {
		t.Fatal(err)
	}

	inChart := shipperv1.Chart{
		Name:    "my-complex-app",
		Version: "0.1.0",
	}

	_, err = DownloadChart(tmp, inChart, srv.URL())
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoadChart(t *testing.T) {
	cwd, _ := filepath.Abs(".")
	chartPath := filepath.Join(cwd, "testdata", "my-complex-app-0.1.0.tgz")

	_, err := LoadChart(chartPath)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRenderChart(t *testing.T) {
	cwd, _ := filepath.Abs(".")

	// Load the test chart.
	chartPath := filepath.Join(cwd, "testdata", "my-complex-app-0.1.0.tgz")
	chrt, err := LoadChart(chartPath)
	if err != nil {
		t.Fatal(err)
	}

	// The yaml file here *must* be a map[string]*chart.Value{}, otherwise
	// errors like "could not render the chart: render error in
	// "my-app/templates/service.yaml": template:
	// my-app/templates/_helpers.tpl:14:40: executing "my-app.fullname" at
	// <.Values.nameOverride>: can't evaluate field nameOverride in type
	// interface {}" will pop up.
	releaseValuesPath := filepath.Join(cwd, "testdata", "TestRenderChart-values.yaml")
	valuesYaml, err := ioutil.ReadFile(releaseValuesPath)
	if err != nil {
		t.Fatal(err)
	}

	// Create the chart configuration out of valuesYaml
	chrtVals := &chart.Config{Raw: string(valuesYaml)}

	// Charts expect to have a couple of values available under .Release to
	// drive the template rendering, so we need to provide some default values.
	options := chartutil.ReleaseOptions{
		Name:      "my-complex-app",
		Time:      timeconv.Now(),
		Namespace: "my-complex-app",
		IsInstall: true,
	}

	// Now we can reallyRenderChart()
	_, err = RenderChart(chrt, chrtVals, options)
	if err != nil {
		t.Fatal(err)
	}
}
