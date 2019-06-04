package rolloutblock

import (
	"fmt"

	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
)

func (c *Controller) applicationAndRolloutBlocks(appKey string) (error, *shipper.Application, []*shipper.RolloutBlock) {
	var (
		rbs, nsRolloutBlocks, globalRolloutBlocks []*shipper.RolloutBlock
	)
	ns, name, err := cache.SplitMetaNamespaceKey(appKey)
	if err != nil {
		runtime.HandleError(shippererrors.NewUnrecoverableError(err))
		return err, nil, nil
	}
	app, err := c.applicationLister.Applications(ns).Get(name)
	if err != nil {
		if kerrors.IsNotFound(err) {
			glog.V(3).Infof("Application %q has been deleted", appKey)
			return err, nil, nil
		}

		err := shippererrors.NewKubeclientGetError(ns, name, err).WithShipperKind("Application")
		runtime.HandleError(fmt.Errorf("error syncing Application %q with RolloutBlocks : %s", appKey, err.Error()))
		return err, nil, nil
	}
	if nsRolloutBlocks, err = c.rolloutBlockLister.RolloutBlocks(ns).List(labels.Everything()); err != nil {
		glog.Warning("error getting Namespace RolloutBlocks %s", err)
	}
	if globalRolloutBlocks, err = c.rolloutBlockLister.RolloutBlocks(shipper.ShipperNamespace).List(labels.Everything()); err != nil {
		glog.Warning("error getting Global RolloutBlocks %s", err)
	}
	rbs = append(nsRolloutBlocks, globalRolloutBlocks...)
	return err, app, rbs
}

func (c *Controller) releaseAndRolloutBlocks(releaseKey string) (error, *shipper.Release, []*shipper.RolloutBlock) {
	var (
		rbs, nsRolloutBlocks, globalRolloutBlocks []*shipper.RolloutBlock
	)
	ns, name, err := cache.SplitMetaNamespaceKey(releaseKey)
	if err != nil {
		runtime.HandleError(shippererrors.NewUnrecoverableError(err))
		return err, nil, nil
	}
	release, err := c.releaseLister.Releases(ns).Get(name)
	if err != nil {
		if kerrors.IsNotFound(err) {
			glog.V(3).Infof("Release %q has been deleted", releaseKey)
			return err, nil, nil
		}

		err := shippererrors.NewKubeclientGetError(ns, name, err).WithShipperKind("Release")
		runtime.HandleError(fmt.Errorf("error syncing Release %q with RolloutBlocks : %s", releaseKey, err.Error()))
		return err, nil, nil
	}
	if nsRolloutBlocks, err = c.rolloutBlockLister.RolloutBlocks(ns).List(labels.Everything()); err != nil {
		glog.Warning("error getting Namespace RolloutBlocks %s", err)
	}
	if globalRolloutBlocks, err = c.rolloutBlockLister.RolloutBlocks(shipper.ShipperNamespace).List(labels.Everything()); err != nil {
		glog.Warning("error getting Global RolloutBlocks %s", err)
	}
	rbs = append(nsRolloutBlocks, globalRolloutBlocks...)
	return err, release, rbs
}