package rolloutblock

import (
	"fmt"

	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	rolloutBlockOverride "github.com/bookingcom/shipper/pkg/util/rolloutblock"
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

	if rolloutBlock.Status.Overrides.Application == "" {
		return nil
	}

	overridingApps := rolloutBlockOverride.NewOverride(rolloutBlock.Status.Overrides.Application)
	overridingApps.Delete(appFullName)
	rolloutBlock.Status.Overrides.Application = overridingApps.String()
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

	ns, name, err = cache.SplitMetaNamespaceKey(appKey)
	if err != nil {
		return err
	}

	app, err := c.applicationLister.Applications(ns).Get(name)
	if err != nil {
		return err
	}

	return c.addApplicationsToRolloutBlocks(rolloutblockKey, rolloutBlock, app)
}

func (c *Controller) addApplicationsToRolloutBlocks(rolloutBlockKey string, rolloutBlock *shipper.RolloutBlock, applications ...*shipper.Application) error {
	appsKeys := rolloutBlockOverride.NewOverride("")
	for _, app := range applications {
		if app.DeletionTimestamp != nil {
			continue
		}

		appKey, err := cache.MetaNamespaceKeyFunc(app)
		if err != nil {
			runtime.HandleError(err)
			continue
		}

		overrideRB, ok := app.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation]
		if !ok {
			continue
		}

		overrideRBs := rolloutBlockOverride.NewOverride(overrideRB)
		for rbKey := range overrideRBs {
			if rbKey == rolloutBlockKey {
				appsKeys.Add(appKey)
			}
		}
	}

	rolloutBlock.Status.Overrides.Application = appsKeys.String()
	_, err := c.shipperClientset.ShipperV1alpha1().RolloutBlocks(rolloutBlock.Namespace).Update(rolloutBlock)
	if err != nil {
		return shippererrors.NewKubeclientUpdateError(rolloutBlock, err).
			WithShipperKind("RolloutBlock")
	}

	return nil
}
