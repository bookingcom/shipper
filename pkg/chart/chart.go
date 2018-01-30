package chart

import (
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/downloader"
	"k8s.io/helm/pkg/engine"
	"k8s.io/helm/pkg/getter"
	"k8s.io/helm/pkg/helm/environment"
	"k8s.io/helm/pkg/helm/helmpath"
	"k8s.io/helm/pkg/proto/hapi/chart"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
)

// DownloadChart downloads a chart using the specified workDir as Helm home
func DownloadChart(workDir string, chart shipperv1.Chart, repoURL string) (string, error) {
	// Then, we need to configure Helm's home path, since the ChartDownloader
	// needs it. (This first implementation I'll use the libraries provided by
	// Helm).
	hh := helmpath.Home(workDir)

	dest := filepath.Join(hh.String(), "dest")

	// Create all required directories in Helm's home
	configDirectories := []string{
		hh.String(),
		hh.Repository(),
		hh.Cache(),
		dest,
	}
	for _, p := range configDirectories {
		if fi, err := os.Stat(p); err != nil {
			if err := os.MkdirAll(p, 0755); err != nil {
				return "", fmt.Errorf("could not create %s: %s", p, err)
			}
		} else if !fi.IsDir() {
			return "", fmt.Errorf("%s must be a directory", p)
		}
	}

	// Attempt to download the specified chart from Shipment Request
	chartRef := fmt.Sprintf("%s/%s-%s.tgz", repoURL, chart.Name, chart.Version)
	d := downloader.ChartDownloader{
		HelmHome: hh,
		Out:      os.Stderr,
		Verify:   downloader.VerifyNever,
		Getters:  getter.All(environment.EnvSettings{}),
	}
	where, _, err := d.DownloadTo(chartRef, "", dest)
	if err != nil {
		return "", fmt.Errorf("could not download %s: %s", chartRef, err)
	}

	return where, nil
}

func LoadChart(chartPath string) (*chart.Chart, error) {
	chrt, err := chartutil.LoadFile(chartPath)
	if err != nil {
		return chrt, fmt.Errorf("could not load chart from %s: %s", chartPath, err)
	}
	return chrt, nil
}

// RenderChart renders a chart, with the given values. It returns a list
// of rendered Kubernetes objects.
func RenderChart(chrt *chart.Chart, chrtVals *chart.Config, options chartutil.ReleaseOptions) ([]string, error) {

	// Shamelessly copied from k8s.io/helm/cmd/helm/template L153
	//
	// The code below load the requirements for the given chart. In the original
	// code there's a call to checkDependencies(), which I didn't yet copy here
	// since I don't know if we need to make this validation since `helm
	// package` itself complains about missing requirements when packaging the
	// chart.
	if req, err := chartutil.LoadRequirements(chrt); err != nil {
		return nil, fmt.Errorf("cannot load requirements: %v", err)
	} else {
		fmt.Printf("%+v", req)
	}

	// Removes disabled charts from the dependencies.
	err := chartutil.ProcessRequirementsEnabled(chrt, chrtVals)
	if err != nil {
		return nil, err
	}

	// Import specified chart values from children to parent.
	err = chartutil.ProcessRequirementsImportValues(chrt)
	if err != nil {
		return nil, err
	}

	// ToRenderValues() add all the metadata values that chart can use to
	// process the templates, such as .Release.Name (coming out of the `options`
	// var above), .Chart.Name (extracted from the chart itself), and others.
	vals, err := chartutil.ToRenderValues(chrt, chrtVals, options)

	// Now we are able to render the chart. `rendered` is a map where the key is
	// the filename and the value is the rendered template. I'm not sure whether
	// we are interested on the filename from this point on, so we'll remove it.
	e := engine.New()
	rendered, err := e.Render(chrt, vals)
	if err != nil {
		return nil, fmt.Errorf("could not render the chart: %s", err)
	}
	renderedObjects := make([]string, 0)
	for _, o := range rendered {
		renderedObjects = append(renderedObjects, o)
	}

	return renderedObjects, nil
}
