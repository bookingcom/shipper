package v1

import (
	"bytes"
	"encoding/base64"

	"gopkg.in/yaml.v2"
	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/helm/pkg/timeconv"
)

func (r *Release) Chart() (*chart.Chart, error) {
	tarball, err := base64.StdEncoding.DecodeString(r.Environment.Chart.Tarball)
	if err != nil {
		return nil, err
	}

	chrt, err := chartutil.LoadArchive(bytes.NewReader(tarball))
	if err != nil {
		return nil, err
	}

	return chrt, nil
}

func (r *Release) Values() (*chart.Config, error) {
	yamlValues, err := yaml.Marshal(r.Environment.ShipmentOrder.Values)
	if err != nil {
		return nil, err
	}
	return &chart.Config{Raw: string(yamlValues)}, nil
}

func (r *Release) Options(cluster *Cluster) chartutil.ReleaseOptions {
	options := chartutil.ReleaseOptions{
		Name:      r.GetName(),
		Time:      timeconv.Now(),
		Namespace: r.Namespace,
		IsInstall: true,
	}
	return options
}
