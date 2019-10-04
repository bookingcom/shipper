package chart

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/engine"
	helmchart "k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/helm/pkg/timeconv"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

// Render renders a chart, with the given values. It returns a list of rendered
// Kubernetes objects.
func Render(chart *helmchart.Chart, name, ns string, shipperValues *shipper.ChartValues) ([]string, error) {
	chartConfig := &helmchart.Config{}
	if shipperValues != nil {
		values := chartutil.Values(*shipperValues)

		var yaml string
		yaml, err := values.YAML()
		if err != nil {
			return nil, err
		}
		chartConfig = &helmchart.Config{Raw: yaml}
	}

	if err := chartutil.ProcessRequirementsEnabled(chart, chartConfig); err != nil {
		return nil, err
	}

	if err := chartutil.ProcessRequirementsImportValues(chart); err != nil {
		return nil, err
	}

	chartOptions := chartutil.ReleaseOptions{
		Name:      name,
		Time:      timeconv.Now(),
		Namespace: ns,
		IsInstall: true,
	}

	helmValues, err := chartutil.ToRenderValues(chart, chartConfig, chartOptions)
	if err != nil {
		return nil, err
	}

	rendered, err := engine.New().Render(chart, helmValues)
	if err != nil {
		return nil, fmt.Errorf("could not render the chart: %s", err)
	}

	objects := CollectObjects(rendered)

	ks, err := newKindSorter(objects, InstallOrder)
	if err != nil {
		return nil, err
	}
	sort.Sort(ks)

	return ks.Manifests(), nil
}

var sep = regexp.MustCompile("(?:^|\\s*\n)---\\s*")

func CollectObjects(rendered map[string]string) []string {
	objects := make([]string, 0, len(rendered))

	for n, o := range rendered {
		if !strings.HasSuffix(n, ".yaml") && !strings.HasSuffix(n, ".yml") {
			continue
		}

		// Making sure that any extra whitespace in YAML stream doesn't
		// interfere in splitting documents correctly.
		objs := sep.Split(strings.TrimSpace(o), -1)

		for _, o := range objs {
			o = strings.TrimSpace(o)
			if len(o) > 0 {
				objects = append(objects, o)
			}
		}
	}

	return objects
}
