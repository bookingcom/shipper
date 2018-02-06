package chart

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"

	"github.com/golang/glog"

	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/engine"
	"k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/helm/pkg/timeconv"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
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
	defer resp.Body.Close()

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

// Render renders a chart, with the given values. It returns a list
// of rendered Kubernetes objects.
func Render(r io.Reader, name, ns string, values *shipperv1.ChartValues) ([]string, error) {
	helmChart, err := chartutil.LoadArchive(r)
	if err != nil {
		return nil, nil
	}

	chartConfig := &chart.Config{Values: map[string]*chart.Value{}}

	if err := chartutil.ProcessRequirementsEnabled(helmChart, chartConfig); err != nil {
		return nil, err
	}

	if err := chartutil.ProcessRequirementsImportValues(helmChart); err != nil {
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

	objects := make([]string, len(rendered), len(rendered))
	for _, o := range rendered {
		objects = append(objects, o)
	}

	return objects, nil
}
