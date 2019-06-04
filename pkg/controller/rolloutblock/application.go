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

func (c *Controller) processNextAppWorkItem() bool {
	// this happens for every NEW and UPDATED application
	obj, shutdown := c.applicationWorkqueue.Get()
	if shutdown {
		return false
	}

	defer c.applicationWorkqueue.Done(obj)

	var (
		key string
		ok  bool
	)

	if key, ok = obj.(string); !ok {
		c.applicationWorkqueue.Forget(obj)
		runtime.HandleError(fmt.Errorf("invalid object key (will retry: false): %#v", obj))
		return true
	}

	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(shippererrors.NewUnrecoverableError(err))
		return true
	}
	app, err := c.applicationLister.Applications(ns).Get(name)
	if err != nil {
		if kerrors.IsNotFound(err) {
			glog.V(3).Infof("Application %q has been deleted", key)
			return true
		}

		err := shippererrors.NewKubeclientGetError(ns, name, err).WithShipperKind("Application")
		runtime.HandleError(fmt.Errorf("error syncing Application %q with RolloutBlock : %s", key, err.Error()))
		return true
	}

	overrideRB, ok := app.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation]
	if !ok {
		return true
	}
	overrideRBs := strings.Split(overrideRB, ",")
	for _, rbKey := range overrideRBs {
		c.addApplicationToRolloutBlockStatus(app, rbKey)
	}

	return true
}

func (c *Controller) processNextDeletedAppWorkItem() bool {
	// this happens for every DELETED application
	obj, shutdown := c.applicationDeleteWorkqueue.Get()
	if shutdown {
		return false
	}

	defer c.applicationDeleteWorkqueue.Done(obj)

	var (
		key string
		ok  bool
	)

	if key, ok = obj.(string); !ok {
		c.applicationDeleteWorkqueue.Forget(obj)
		runtime.HandleError(fmt.Errorf("invalid object key (will retry: false): %#v", obj))
		return true
	}

	data := strings.Split(key, "*") // TODO : fix this hacky thing!
	if len(data) != 2 {
		glog.V(3).Infof("Could not separate application from overriding RolloutBlocks! ", key)
		return true
	}

	appKey := data[0]
	rolloutBlocks := strings.Split(data[1], ",")// TODO: Check if its empty!!!

	for _, rbFullName := range rolloutBlocks {
		err := c.removeAppFromRolloutBlockStatus(rbFullName, appKey)
		if err != nil {
			runtime.HandleError(err)
		}
	}

	return true
}

func (c *Controller) removeAppFromRolloutBlockStatus(rbFullName string, appFullName string) error {
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

	if rb.Status.Overrides.Application == nil {
		return nil
	}

	rb.Status.Overrides.Application = stringUtil.Delete(rb.Status.Overrides.Application, appFullName)
	_, err = c.shipperClientset.ShipperV1alpha1().RolloutBlocks(rb.Namespace).Update(rb)
	if err != nil {
		return shippererrors.NewKubeclientUpdateError(rb, err).
			WithShipperKind("RolloutBlock")
	}

	return nil
}

func (c *Controller) addApplicationToRolloutBlockStatus(app *shipper.Application, rolloutblockKey string) {
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

	glog.V(3).Infof("Application %s overrides RolloutBlock %s", app.Name, rolloutBlock.Name)
	appKey, err := cache.MetaNamespaceKeyFunc(app)
	if err != nil {
		runtime.HandleError(err)
		return
	}
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
