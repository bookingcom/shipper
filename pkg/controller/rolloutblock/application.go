package rolloutblock

import (
	"fmt"
	"github.com/golang/glog"
	"k8s.io/client-go/tools/cache"

	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	stringUtil "github.com/bookingcom/shipper/pkg/util/string"
)

func (c *Controller) removeAppFromRolloutBlockStatus(appFullName string, rbFullName string) error {
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

	if rolloutBlock.Status.Overrides.Application == nil {
		return nil
	}

	rolloutBlock.Status.Overrides.Application = stringUtil.Delete(rolloutBlock.Status.Overrides.Application, appFullName)
	_, err = c.shipperClientset.ShipperV1alpha1().RolloutBlocks(rolloutBlock.Namespace).Update(rolloutBlock)
	if err != nil {
		return shippererrors.NewKubeclientUpdateError(rolloutBlock, err).
			WithShipperKind("RolloutBlock")
	}

	return nil
}

func (c *Controller) addApplicationToRolloutBlockStatus(appKey string, rolloutblockKey string) error {
	ns, name, err := cache.SplitMetaNamespaceKey(rolloutblockKey)
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

	glog.V(3).Infof("Application %s overrides RolloutBlock %s", appKey, rolloutBlock.Name)
	rolloutBlock.Status.Overrides.Application = stringUtil.AppendIfMissing(
		rolloutBlock.Status.Overrides.Application,
		appKey,
	)
	_, err = c.shipperClientset.ShipperV1alpha1().RolloutBlocks(rolloutBlock.Namespace).Update(rolloutBlock)
	if err != nil {
		return shippererrors.NewKubeclientUpdateError(rolloutBlock, err).
			WithShipperKind("RolloutBlock")
	}

	return nil
}
