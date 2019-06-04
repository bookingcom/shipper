package rolloutblock

import (
	"fmt"
	"k8s.io/client-go/tools/cache"
	"strings"

	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/util/runtime"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	stringUtil "github.com/bookingcom/shipper/pkg/util/string"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
)

func (c *Controller) processNextRelWorkItem() bool {
	// this happens for every NEW and UPDATED release
	obj, shutdown := c.releaseWorkqueue.Get()
	if shutdown {
		return false
	}

	defer c.releaseWorkqueue.Done(obj)

	var (
		key string
		ok  bool
	)

	if key, ok = obj.(string); !ok {
		c.releaseWorkqueue.Forget(obj)
		runtime.HandleError(fmt.Errorf("invalid object key (will retry: false): %#v", obj))
		return true
	}

	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(shippererrors.NewUnrecoverableError(err))
		return true
	}

	rel, err := c.releaseLister.Releases(ns).Get(name)
	if err != nil {
		if kerrors.IsNotFound(err) {
			glog.V(3).Infof("Release %q has been deleted", key)
			return true
		}

		err := shippererrors.NewKubeclientGetError(ns, name, err).WithShipperKind("Release")
		runtime.HandleError(fmt.Errorf("error syncing Release %q with RolloutBlock : %s", key, err.Error()))
		return true
	}

	overrideRB, ok := rel.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation]
	if !ok {
		return true
	}
	overrideRBs := strings.Split(overrideRB, ",")
	for _, rbKey := range overrideRBs {
		c.addReleaseToRolloutBlockStatus(rel, rbKey)
	}

	return true
}

func (c *Controller) processNextDeletedRelWorkItem() bool {
	// this happens for every DELETED release
	obj, shutdown := c.releaseDeleteWorkqueue.Get()
	if shutdown {
		return false
	}

	defer c.releaseDeleteWorkqueue.Done(obj)

	var (
		key string
		ok  bool
	)

	if key, ok = obj.(string); !ok {
		c.releaseDeleteWorkqueue.Forget(obj)
		runtime.HandleError(fmt.Errorf("invalid object key (will retry: false): %#v", obj))
		return true
	}

	data := strings.Split(key, "*") //TODO
	if len(data) != 2 {
		glog.V(3).Infof("Could not separate release from overriding RolloutBlocks! ", key)
		return true
	}

	relKey := data[0]
	rolloutBlocks := strings.Split(data[1], ",") // TODO: Check if its empty!!!

	for _, rbFullName := range rolloutBlocks {
		err := c.removeReleaseFromRolloutBlockStatus(rbFullName, relKey)
		if err != nil {
			runtime.HandleError(err)
		}
	}

	return true
}

func (c *Controller) addReleaseToRolloutBlockStatus(release *shipper.Release, rolloutblockKey string) {
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

	glog.V(3).Infof("Release %s overrides RolloutBlock %s", release.Name, rolloutBlock.Name)
	relKey, err := cache.MetaNamespaceKeyFunc(release)
	if err != nil {
		runtime.HandleError(err)
		return
	}
	rolloutBlock.Status.Overrides.Release = stringUtil.AppendIfMissing(
		rolloutBlock.Status.Overrides.Release,
		relKey,
	)
	_, err = c.shipperClientset.ShipperV1alpha1().RolloutBlocks(rolloutBlock.Namespace).Update(rolloutBlock)
	if err != nil {
		runtime.HandleError(shippererrors.NewKubeclientUpdateError(rolloutBlock, err).
			WithShipperKind("RolloutBlock"))
	}
}

func (c *Controller) removeReleaseFromRolloutBlockStatus(rbFullName string, relFullName string) error {
	ns, name, err := cache.SplitMetaNamespaceKey(rbFullName)
	if err != nil {
		return shippererrors.NewUnrecoverableError(err)
	}

	rb, err := c.rolloutBlockLister.RolloutBlocks(ns).Get(name)
	if err != nil {
		if kerrors.IsNotFound(err) {
			glog.V(3).Infof("RolloutBlock %q has been deleted", rbFullName)
			return nil
		}

		return shippererrors.NewKubeclientGetError(ns, name, err).
			WithShipperKind("RolloutBlock")
	}

	rb = rb.DeepCopy()

	if rb.Status.Overrides.Release == nil {
		return nil
	}

	rb.Status.Overrides.Release = stringUtil.Delete(rb.Status.Overrides.Release, relFullName)

	_, err = c.shipperClientset.ShipperV1alpha1().RolloutBlocks(rb.Namespace).Update(rb)
	if err != nil {
		return shippererrors.NewKubeclientUpdateError(rb, err).
			WithShipperKind("RolloutBlock")
	}

	return nil
}