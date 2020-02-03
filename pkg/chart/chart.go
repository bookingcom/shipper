package chart

import (
	"regexp"
	"sort"
	"strings"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
)

// Render renders a chart, with the given values. It returns a list of rendered
// Kubernetes objects.
func Render(chart *chart.Chart, name, ns string, shipperValues *shipper.ChartValues) ([]string, error) {
	options := chartutil.ReleaseOptions{
		Name:      name,
		Namespace: ns,
		IsInstall: true,
	}

	var values map[string]interface{}
	if shipperValues != nil {
		values = *shipperValues
	}

	renderValues, err := chartutil.ToRenderValues(
		chart,
		values,
		options,
		chartutil.DefaultCapabilities,
	)
	if err != nil {
		return nil, err
	}

	if err := chartutil.ProcessDependencies(chart, renderValues); err != nil {
		return nil, err
	}

	rendered, err := engine.Render(chart, renderValues)
	if err != nil {
		return nil, err
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
