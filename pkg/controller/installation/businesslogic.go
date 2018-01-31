package installation

import (
	shipperV1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shipperChart "github.com/bookingcom/shipper/pkg/chart"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"
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

func (c *Controller) installIfMissing(rendered []string, cluster *shipperV1.Cluster, release *shipperV1.Release) error {

	client, err := cluster.Client()
	if err != nil {
		return err
	}

	// Try to install all the rendered objects in the target cluster. We should
	// fail in the first error to report that this cluster has an issue. Since
	// the InstallationTarget.Status represent a per cluster status with a
	// scalar value, we don't try to install other objects for now.
	for _, e := range rendered {
		result := client.CoreV1().RESTClient().Post().Namespace(release.Namespace).Body(e).Do()
		if err := result.Error(); err != nil {
			// What sort of heuristics should we use to assume that an object
			// has already been created *and* it is the right object? If the right
			// object is already created, then we should continue. For now we will
			// naively assume that if a file with the expected name exists, it was
			// created by us.
			if errors.IsAlreadyExists(err) {
				continue
			}

			// Perhaps we want to annotate differently the error when the request
			// couldn't be constructed? Can be removed later on if not proven useful.
			if rce, ok := err.(*rest.RequestConstructionError); ok {
				return rce
			}

			return err
		}

	}

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

	err = c.installIfMissing(rendered, cluster, release)
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

	// The strategy here is try our best to install as many objects as possible
	// in all target clusters. It is not the Installation Controller job to
	// reason about a target cluster status.
	clusterStatuses := make([]shipperV1.ClusterInstallationStatus, 0)
	for _, name := range it.Spec.Clusters {
		status := shipperV1.ClusterInstallationStatus{Name: name}

		if cluster, err := c.clusterLister.Get(name); err != nil {
			status.Status = shipperV1.InstallationStatusFailed
			status.Message = err.Error()
		} else {
			if err = c.clusterBusinessLogic(release, it, cluster); err != nil {
				status.Status = shipperV1.InstallationStatusFailed
				status.Message = err.Error()
			} else {
				status.Status = shipperV1.InstallationStatusSucceeded
			}
		}

		clusterStatuses = append(clusterStatuses, status)
	}

	it.Status.Clusters = clusterStatuses

	_, err = c.shipperclientset.ShipperV1().InstallationTargets(it.Namespace).Update(it)
	if err != nil {
		return err
	}

	return nil
}
