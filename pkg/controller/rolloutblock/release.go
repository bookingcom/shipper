package rolloutblock

import (
	"fmt"
	"k8s.io/apimachinery/pkg/labels"

	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
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
		return fmt.Errorf("RolloutBlock %s has been deleted", rbFullName)
	}
	
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
	ns, _, err := cache.SplitMetaNamespaceKey(relFullName)
	if err != nil {
		return err
	}

	gbRB, err := c.rolloutBlockLister.RolloutBlocks(shipper.ShipperNamespace).List(labels.Everything())
	if err != nil {
		runtime.HandleError(err)
	}
	nsRB, err := c.rolloutBlockLister.RolloutBlocks(ns).List(labels.Everything())
	if err != nil {
		runtime.HandleError(err)
	}
	RB := append(nsRB, gbRB...)
	for _, rolloutBlock := range RB {
		if len(rolloutBlock.Status.Overrides.Release) == 0 {
			continue
		}

		rolloutBlock.Status.Overrides.Release = stringUtil.Delete(rolloutBlock.Status.Overrides.Release, relFullName)
		_, err = c.shipperClientset.ShipperV1alpha1().RolloutBlocks(rolloutBlock.Namespace).Update(rolloutBlock)
		if err != nil {
			return shippererrors.NewKubeclientUpdateError(rolloutBlock, err).
				WithShipperKind("RolloutBlock")
		}
	}

	return nil
}

func (c *Controller) getAppFromRelease(rel *shipper.Release) (*shipper.Application, error) {
	appName, err := releaseutil.ApplicationNameForRelease(rel)
	if err != nil {
		return nil, err
	}

	app, err := c.applicationLister.Applications(rel.Namespace).Get(appName)
	if err != nil {
		runtime.HandleError(err)
		return nil, err
	}
	return app, nil
}