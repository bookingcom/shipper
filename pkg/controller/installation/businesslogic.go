package installation

import (
	shipperV1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shipperChart "github.com/bookingcom/shipper/pkg/chart"
	"gopkg.in/yaml.v2"
	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/helm/pkg/timeconv"
)

func (c *Controller) renderRelease(release *shipperV1.Release, cluster *shipperV1.Cluster) ([]string, error) {
	chrt := loadChartFromEmbedded(release.Environment.Chart) // TODO: Make this a method of EmbeddedChart
	vals, err := loadValsFromRelease(release)                // TODO: Make this a method of Release
	if err != nil {
		return nil, err
	}
	options := optionsForCluster(release, cluster)
	return shipperChart.RenderChart(chrt, vals, options)
}

func optionsForCluster(release *shipperV1.Release, cluster *shipperV1.Cluster) chartutil.ReleaseOptions {
	options := chartutil.ReleaseOptions{
		Name:      release.Name,
		Time:      timeconv.Now(),
		Namespace: release.Namespace,
		IsInstall: true,
	}
	return options

}

func loadValsFromRelease(release *shipperV1.Release) (*chart.Config, error) {
	yamlValues, err := yaml.Marshal(release.Environment.ShipmentOrder.Values)
	if err != nil {
		return nil, err
	}
	return &chart.Config{Raw: string(yamlValues)}, nil
}

func loadChartFromEmbedded(embeddedChart shipperV1.EmbeddedChart) *chart.Chart {
	// base64 -> []byte -> *chart.Chart
	return nil
}

func (c *Controller) installIfMissing(rendered []string, cluster *shipperV1.Cluster) error {
	// - Check if rendered objects already exist in target cluster
	//   - If do not exist, create them (Research how smart Tiller is here)
	// - Install rendered objects in target cluster
	return nil
}

func (c *Controller) clusterBusinessLogic(
	release *shipperV1.Release,
	it *shipperV1.InstallationTarget,
	cluster *shipperV1.Cluster,
) error {
	rendered, err := c.renderRelease(release, cluster)
	if err != nil {
		return err
	}

	err = c.installIfMissing(rendered, cluster)
	if err != nil {
		return err
	}
	return nil
}

func (c *Controller) businessLogic(it *shipperV1.InstallationTarget) error {

	release, err := c.releaseLister.Releases(it.Namespace).Get(it.Name)
	if err != nil {
		return err
	}

	// Iterate list of target clusters:
	for _, clusterName := range it.Spec.Clusters {
		cluster, err := c.clusterLister.Get(clusterName)
		if err != nil {
			return err
		}

		if err = c.clusterBusinessLogic(release, it, cluster); err != nil {
			return err
		}
	}

	_, err = c.shipperclientset.ShipperV1().InstallationTargets(it.Namespace).Update(it)
	if err != nil {
		return err
	}

	return nil
}
