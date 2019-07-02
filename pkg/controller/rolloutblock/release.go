package rolloutblock

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	stringUtil "github.com/bookingcom/shipper/pkg/util/string"
)

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

	rolloutBlock.Status.Overrides.Release = stringUtil.Grep(rolloutBlock.Status.Overrides.Release, relFullName)
	_, err = c.shipperClientset.ShipperV1alpha1().RolloutBlocks(rolloutBlock.Namespace).Update(rolloutBlock)
	if err != nil {
		return shippererrors.NewKubeclientUpdateError(rolloutBlock, err).
			WithShipperKind("RolloutBlock")
	}

	return nil
}

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

	ns, name, err = cache.SplitMetaNamespaceKey(relFullName)
	if err != nil {
		return err
	}

	rel, err := c.releaseLister.Releases(ns).Get(name)
	if err != nil {
		return err
	}

	return c.addReleasesToRolloutBlocks(rbFullName, rolloutBlock, rel)
}

func (c *Controller) addReleasesToRolloutBlocks(rolloutBlockKey string, rolloutBlock *shipper.RolloutBlock, releases ...*shipper.Release) error {
	var relsKeys []string
	for _, release := range releases {
		if release.DeletionTimestamp != nil {
			continue
		}

		relKey, err := cache.MetaNamespaceKeyFunc(release)
		if err != nil {
			runtime.HandleError(err)
			continue
		}

		overrideRB, ok := release.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation]
		if !ok {
			continue
		}

		overrideRBs := strings.Split(overrideRB, ",")
		for _, rbKey := range overrideRBs {
			if rbKey == rolloutBlockKey {
				relsKeys = append(relsKeys, relKey)
			}
		}
	}

	rolloutBlock.Status.Overrides.Release = relsKeys
	_, err := c.shipperClientset.ShipperV1alpha1().RolloutBlocks(rolloutBlock.Namespace).Update(rolloutBlock)
	if err != nil {
		return shippererrors.NewKubeclientUpdateError(rolloutBlock, err).
			WithShipperKind("RolloutBlock")
	}

	return nil
}
