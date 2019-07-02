package rolloutblock

import (
	"fmt"
	"strings"

	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
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

	rolloutBlock.Status.Overrides.Application = stringUtil.Grep(rolloutBlock.Status.Overrides.Application, appFullName)
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

	glog.V(8).Infof("Application %s overrides RolloutBlock %s", appKey, rolloutBlock.Name)
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

func (c *Controller) addApplicationsToRolloutBlocks(rolloutBlockKey string, rolloutBlock *shipper.RolloutBlock, applications ...*shipper.Application) error {
	var appsStrings []string
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

		overrideRBs := strings.Split(overrideRB, ",")
		for _, rbKey := range overrideRBs {
			if rbKey == rolloutBlockKey {
				appsStrings = append(appsStrings, appKey)
			}
		}
	}

	if len(appsStrings) == 0 {
		appsStrings = []string{}
	}

	rolloutBlock.Status.Overrides.Application = appsStrings
	_, err := c.shipperClientset.ShipperV1alpha1().RolloutBlocks(rolloutBlock.Namespace).Update(rolloutBlock)
	if err != nil {
		return shippererrors.NewKubeclientUpdateError(rolloutBlock, err).
			WithShipperKind("RolloutBlock")
	}

	return nil
}
