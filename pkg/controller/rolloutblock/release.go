package rolloutblock

import (
	"fmt"

	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
	stringUtil "github.com/bookingcom/shipper/pkg/util/string"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
)

func (c *Controller) addReleaseToRolloutBlockStatus(relKey string, rolloutblockKey string) {
	ns, name, err := cache.SplitMetaNamespaceKey(rolloutblockKey)
	if err != nil {
		glog.Infof("HILLA this is the error: %s", err.Error())
		runtime.HandleError(err)
		return
	}

	rolloutBlock, err := c.rolloutBlockLister.RolloutBlocks(ns).Get(name)
	if err != nil {
		glog.Infof("HILLA this is the error: %s", err.Error())
		runtime.HandleError(err)
		return
	}

	if rolloutBlock.DeletionTimestamp != nil {
		runtime.HandleError(fmt.Errorf("HILLA RolloutBlock %s/%s has been deleted", rolloutBlock.Namespace, rolloutBlock.Name))
		return
	}

	glog.V(3).Infof("HILLA Release %s overrides RolloutBlock %s", relKey, rolloutBlock.Name)
	rolloutBlock.Status.Overrides.Release = stringUtil.AppendIfMissing(
		rolloutBlock.Status.Overrides.Release,
		relKey,
	)
	_, err = c.shipperClientset.ShipperV1alpha1().RolloutBlocks(rolloutBlock.Namespace).Update(rolloutBlock)
	if err != nil {
		glog.Infof("HILLA this is the error: %s", err.Error())
		runtime.HandleError(shippererrors.NewKubeclientUpdateError(rolloutBlock, err).
			WithShipperKind("RolloutBlock"))
	}
}

func (c *Controller) removeReleaseFromRolloutBlockStatus(relFullName string, rbFullName string)  {
	ns, name, err := cache.SplitMetaNamespaceKey(rbFullName)
	if err != nil {
		runtime.HandleError(err)
		glog.Infof("HILLA this is the error: %s", err.Error())
		return
	}

	rb, err := c.rolloutBlockLister.RolloutBlocks(ns).Get(name)
	if err != nil {
		if kerrors.IsNotFound(err) {
			glog.V(3).Infof("HILLA RolloutBlock %q has been deleted", rbFullName)
			return
		}

		glog.Infof("HILLA this is the error: %s", err.Error())
		runtime.HandleError(err)
		return
	}

	rb = rb.DeepCopy()

	if rb.Status.Overrides.Release == nil {
		return
	}

	rb.Status.Overrides.Release = stringUtil.Delete(rb.Status.Overrides.Release, relFullName)

	_, err = c.shipperClientset.ShipperV1alpha1().RolloutBlocks(rb.Namespace).Update(rb)
	if err != nil {
		glog.Infof("HILLA this is the error: %s", err.Error())
		runtime.HandleError(err)
	}

	return
}

func (c *Controller) getAppFromRelease(rel *shipper.Release) (*shipper.Application, error) {
	appName, err := releaseutil.ApplicationNameForRelease(rel)
	glog.Infof("HILLA release %s has an application %s", rel.Name, appName)
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