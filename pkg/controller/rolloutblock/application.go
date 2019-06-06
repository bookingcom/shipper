package rolloutblock

import (
	"fmt"
	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"

	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	stringUtil "github.com/bookingcom/shipper/pkg/util/string"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
)

func (c *Controller) removeAppFromRolloutBlockStatus(appFullName string, rbFullName string)  {
	ns, name, err := cache.SplitMetaNamespaceKey(rbFullName)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	rb, err := c.rolloutBlockLister.RolloutBlocks(ns).Get(name)
	if err != nil {
		if kerrors.IsNotFound(err) {
			glog.V(3).Infof("RolloutBlock %q has been deleted", rbFullName)
			return
		}

		runtime.HandleError(err)
		return
	}

	rb = rb.DeepCopy()

	if rb.Status.Overrides.Application == nil {
		return
	}

	rb.Status.Overrides.Application = stringUtil.Delete(rb.Status.Overrides.Application, appFullName)
	_, err = c.shipperClientset.ShipperV1alpha1().RolloutBlocks(rb.Namespace).Update(rb)
	if err != nil {
		runtime.HandleError(err)
	}

	return
}

func (c *Controller) addApplicationToRolloutBlockStatus(appKey string, rolloutblockKey string) {
	glog.Infof("HILLA  adding application %s to rollout block %s", appKey, rolloutblockKey)
	ns, name, err := cache.SplitMetaNamespaceKey(rolloutblockKey)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	rolloutBlock, err := c.rolloutBlockLister.RolloutBlocks(ns).Get(name)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	if rolloutBlock.DeletionTimestamp != nil {
		runtime.HandleError(fmt.Errorf("RolloutBlock %s/%s has been deleted", rolloutBlock.Namespace, rolloutBlock.Name))
		return
	}

	glog.V(3).Infof("HILLA Application %s overrides RolloutBlock %s", appKey, rolloutBlock.Name)
	rolloutBlock.Status.Overrides.Application = stringUtil.AppendIfMissing(
		rolloutBlock.Status.Overrides.Application,
		appKey,
	)
	_, err = c.shipperClientset.ShipperV1alpha1().RolloutBlocks(rolloutBlock.Namespace).Update(rolloutBlock)
	if err != nil {
		runtime.HandleError(shippererrors.NewKubeclientUpdateError(rolloutBlock, err).
			WithShipperKind("RolloutBlock"))
	}
}
