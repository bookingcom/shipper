package installation

import (
	shipperV1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shipperChart "github.com/bookingcom/shipper/pkg/chart"
	"k8s.io/client-go/tools/clientcmd"
)

func (c *Controller) renderRelease(release *shipperV1.Release, cluster *shipperV1.Cluster) ([]string, error) {
	options := release.Options(cluster)
	chrt, err := release.Chart()
	if err != nil {
		return nil, err
	}
	vals, err := release.Values()
	if err != nil {
		return nil, err
	}
	return shipperChart.RenderChart(chrt, vals, options)
}

func (c *Controller) installIfMissing(rendered []string, cluster *shipperV1.Cluster) error {
	// - Get client for cluster
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
