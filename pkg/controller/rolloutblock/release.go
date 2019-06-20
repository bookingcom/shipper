package rolloutblock

import (
	"fmt"
	"github.com/golang/glog"
	"k8s.io/client-go/tools/cache"

	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	stringUtil "github.com/bookingcom/shipper/pkg/util/string"
)

func (c *Controller) addReleaseToRolloutBlockStatus(relFullName string, rbFullName string) error {
	ns, name, err := cache.SplitMetaNamespaceKey(rbFullName)
	if err != nil {
		return err
	}

	rolloutBlock, err := c.rolloutBlockLister.RolloutBlocks(ns).Get(name)
	if err != nil {
		return err
	}

	if rolloutBlock.DeletionTimestamp != nil {
		return fmt.Errorf("RolloutBlock %s/%s has been deleted", rolloutBlock.Namespace, rolloutBlock.Name)
	}

	glog.V(3).Infof("Release %s overrides RolloutBlock %s", relFullName, rolloutBlock.Name)
	rolloutBlock.Status.Overrides.Release = stringUtil.AppendIfMissing(
		rolloutBlock.Status.Overrides.Release,
		relFullName,
	)
	_, err = c.shipperClientset.ShipperV1alpha1().RolloutBlocks(rolloutBlock.Namespace).Update(rolloutBlock)
	if err != nil {
		return shippererrors.NewKubeclientUpdateError(rolloutBlock, err).
			WithShipperKind("RolloutBlock")
	}

	return nil
}

func (c *Controller) removeReleaseFromRolloutBlockStatus(relFullName string, rbFullName string) error {
	ns, name, err := cache.SplitMetaNamespaceKey(rbFullName)
	if err != nil {
		return err
	}

	rolloutBlock, err := c.rolloutBlockLister.RolloutBlocks(ns).Get(name)
	if err != nil {
		return err
	}

	if rolloutBlock.DeletionTimestamp != nil {
		return fmt.Errorf("RolloutBlock %s/%s has been deleted", rolloutBlock.Namespace, rolloutBlock.Name)
	}

	if rolloutBlock.Status.Overrides.Release == nil {
		return nil
	}

	rolloutBlock.Status.Overrides.Release = stringUtil.Delete(rolloutBlock.Status.Overrides.Release, relFullName)
	_, err = c.shipperClientset.ShipperV1alpha1().RolloutBlocks(rolloutBlock.Namespace).Update(rolloutBlock)
	if err != nil {
		return shippererrors.NewKubeclientUpdateError(rolloutBlock, err).
			WithShipperKind("RolloutBlock")
	}

	return nil
}

//func (c *Controller) getAppFromRelease(rel *shipper.Release) (*shipper.Application, error) {
//	appName, err := releaseutil.ApplicationNameForRelease(rel)
//	if err != nil {
//		return nil, err
//	}
//
//	app, err := c.applicationLister.Applications(rel.Namespace).Get(appName)
//	if err != nil {
//		runtime.HandleError(err)
//		return nil, err
//	}
//	return app, nil
//}