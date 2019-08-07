package rolloutblock

import (
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	"github.com/bookingcom/shipper/pkg/util/rolloutblock"
)

func (c *Controller) addReleasesToRolloutBlocks(rolloutBlockKey string, rolloutBlock *shipper.RolloutBlock, releases ...*shipper.Release) error {
	relsKeys := rolloutblock.NewObjectNameList("")
	for _, release := range releases {
		if release.DeletionTimestamp != nil {
			continue
		}

		relKey, err := cache.MetaNamespaceKeyFunc(release)
		if err != nil {
			runtime.HandleError(err)
			continue
		}

		rbOverrideAnnotation, ok := release.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation]
		if !ok {
			continue
		}

		overrideRBs := rolloutblock.NewObjectNameList(rbOverrideAnnotation)
		for rbKey := range overrideRBs {
			if rbKey == rolloutBlockKey {
				relsKeys.Add(relKey)
			}
		}
	}

	rolloutBlock.Status.Overrides.Release = relsKeys.String()

	return nil
}

func (c *Controller) syncRelease(key string) error {
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	rel, err := c.releaseLister.Releases(ns).Get(name)
	if err != nil {
		return err
	}
	overrides := rolloutblock.NewObjectNameList(rel.Annotations[shipper.RolloutBlocksOverrideAnnotation])
	for rbkey := range overrides {
		rbNs, rbName, err := cache.SplitMetaNamespaceKey(rbkey)
		if err != nil {
			continue
		}
		_, err = c.rolloutBlockLister.RolloutBlocks(rbNs).Get(rbName)
		if errors.IsNotFound(err) {
			overrides.Delete(rbkey)
		}
	}
	rel.Annotations[shipper.RolloutBlocksOverrideAnnotation] = overrides.String()
	_, err = c.shipperClientset.ShipperV1alpha1().Releases(ns).Update(rel)
	if err != nil {
		return shippererrors.NewKubeclientUpdateError(rel, err).
			WithShipperKind("Release")
	}

	return nil
}
