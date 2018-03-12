package chart

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"sort"

	"github.com/golang/glog"

	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/engine"
	"k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/helm/pkg/timeconv"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	"strings"
)

func Download(chart shipperv1.Chart) (*bytes.Buffer, error) {
	u, err := url.Parse(chart.RepoURL)
	if err != nil {
		return nil, err
	}

	u.Path = fmt.Sprintf("%s/%s-%s.tgz", u.Path, chart.Name, chart.Version)
	if err != nil {
		return nil, err
	}

	glog.V(10).Infof("trying to download %s", u)

	resp, err := http.Get(u.String()) // TODO retry, timeout
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

	return bytes.NewBuffer(data), err
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
	if _, err := chartutil.LoadRequirements(chrt); err != nil {
		return nil, fmt.Errorf("cannot load requirements: %v", err)
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
	if err != nil {
		return nil, err
	}

	// Now we are able to render the chart. `rendered` is a map where the key is
	// the filename and the value is the rendered template. I'm not sure whether
	// we are interested on the filename from this point on, so we'll remove it.
	e := engine.New()
	rendered, err := e.Render(chrt, vals)
	if err != nil {
		return nil, fmt.Errorf("could not render the chart: %s", err)
	}
	objects := make([]string, 0, len(rendered))
	for n, o := range rendered {
		if len(o) > 0 && strings.HasSuffix(n, ".yaml") {
			objects = append(objects, o)
		}
	}

	ks := newKindSorter(objects, InstallOrder)
	sort.Sort(ks)

	return ks.Manifests(), nil
}

// Render renders a chart, with the given values. It returns a list
// of rendered Kubernetes objects.
func Render(r io.Reader, name, ns string, values *shipperv1.ChartValues) ([]string, error) {
	helmChart, err := chartutil.LoadArchive(r)
	if err != nil {
		return nil, nil
	}

	chartConfig := &chart.Config{Values: map[string]*chart.Value{}}

	if err = chartutil.ProcessRequirementsEnabled(helmChart, chartConfig); err != nil {
		return nil, err
	}

	if err = chartutil.ProcessRequirementsImportValues(helmChart); err != nil {
		return nil, err
	}

	chartOptions := chartutil.ReleaseOptions{
		Name:      name,
		Time:      timeconv.Now(),
		Namespace: ns,
		IsInstall: true,
	}

	helmValues, err := chartutil.ToRenderValues(helmChart, chartConfig, chartOptions)
	if err != nil {
		return nil, err
	}

	rendered, err := engine.New().Render(helmChart, helmValues)
	if err != nil {
		return nil, fmt.Errorf("could not render the chart: %s", err)
	}

	objects := make([]string, 0, len(rendered))
	for n, o := range rendered {
		if len(o) > 0 && strings.HasSuffix(n, ".yaml") {
			objects = append(objects, o)
		}
	}

	return objects, nil
}
