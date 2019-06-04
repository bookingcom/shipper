package application

import (
	"fmt"
	"strings"

	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	apputil "github.com/bookingcom/shipper/pkg/util/application"
	rolloutblockUtil "github.com/bookingcom/shipper/pkg/util/rolloutblock"
	stringUtil "github.com/bookingcom/shipper/pkg/util/string"
)

func (c *Controller) processNextRolloutBlockWorkItem() bool {
	// When a RolloutBlock is being deleted
	obj, shutdown := c.rbWorkqueue.Get()
	if shutdown {
		return false
	}

	defer c.rbWorkqueue.Done(obj)

	var (
		key string
		ok  bool
	)

	if key, ok = obj.(string); !ok {
		c.rbWorkqueue.Forget(obj)
		runtime.HandleError(fmt.Errorf("invalid object key (will retry: false): %#v", obj))
		return true
	}

	shouldRetry := false
	err := c.syncDeletedRolloutBlock(key)

	if err != nil {
		shouldRetry = shippererrors.ShouldRetry(err)
		runtime.HandleError(fmt.Errorf("error syncing Application %q (will retry: %t): %s", key, shouldRetry, err.Error()))
	}

	if shouldRetry {
		if c.rbWorkqueue.NumRequeues(key) >= maxRetries {
			// Drop the RolloutBlock's key out of the workqueue and thus reset its
			// backoff. This limits the time a "broken" object can hog a worker.
			glog.Warningf("RolloutBlock %q has been retried too many times, dropping from the queue", key)
			c.rbWorkqueue.Forget(key)

			return true
		}

		c.rbWorkqueue.AddRateLimited(key)

		return true
	}

	glog.V(4).Infof("Successfully synced RolloutBlock in Application %q", key)
	c.rbWorkqueue.Forget(obj)

	return true
}

func (c *Controller) syncDeletedRolloutBlock(key string) error {
	data := strings.Split(key, "*")
	if len(data) != 2 {
		return fmt.Errorf("could not separate RolloutBlock from overriding applications! %s", key)
	}
	rbKey := data[0]
	applications := strings.Split(data[1], ",")

	for _, appFullName := range applications {
		err := c.removeRolloutBlockFromApplication(appFullName, rbKey)
		if err != nil {
			glog.V(3).Info(err.Error())
		}
	}

	ns, name, err := cache.SplitMetaNamespaceKey(rbKey)
	if err != nil {
		if kerrors.IsNotFound(err) {
			glog.V(3).Infof("RolloutBlock %q has been deleted", key)
			return nil
		}

		return shippererrors.NewKubeclientGetError(ns, name, err).
			WithShipperKind("RolloutBlock")
	}

	if ns == shipper.ShipperNamespace {
		// global RB. update all applications!

	} else {
		// update only in NS
	}

	return nil
}

func (c *Controller) removeRolloutBlockFromApplication(key string, rbFullName string) error {
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return shippererrors.NewUnrecoverableError(err)
	}

	app, err := c.appLister.Applications(ns).Get(name)
	if err != nil {
		if kerrors.IsNotFound(err) {
			glog.V(3).Infof("Application %q has been deleted", key)
			return nil
		}

		return shippererrors.NewKubeclientGetError(ns, name, err).
			WithShipperKind("Application")
	}

	app = app.DeepCopy()

	if app.Annotations == nil {
		return nil
	}

	annotations, ok := app.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation]
	if !ok {
		return nil
	}

	annotationsArray := strings.Split(annotations, ",")
	annotationsArray = stringUtil.Delete(annotationsArray, rbFullName)
	if len(annotationsArray) == 0 {
		delete(app.Annotations, shipper.RolloutBlocksOverrideAnnotation)
	} else {
		app.Annotations[shipper.RolloutBlocksOverrideAnnotation] = strings.Join(annotationsArray, ",")
	}

	_, err = c.shipperClientset.ShipperV1alpha1().Applications(app.Namespace).Update(app)
	if err != nil {
		return shippererrors.NewKubeclientUpdateError(app, err).
			WithShipperKind("Application")
	}

	var nsRolloutBlocks, globalRolloutBlocks []*shipper.RolloutBlock
	if nsRolloutBlocks, err = c.rbLister.RolloutBlocks(app.Namespace).List(labels.Everything()); err != nil {
		glog.Warning("error getting Namespace RolloutBlocks %s", err)
	}
	if globalRolloutBlocks, err = c.rbLister.RolloutBlocks(shipper.ShipperNamespace).List(labels.Everything()); err != nil {
		glog.Warning("error getting Global RolloutBlocks %s", err)
	}
	rbs := append(nsRolloutBlocks, globalRolloutBlocks...)
	c.updateApplicationRolloutCondition(rbs, app)


	return nil
}

func (c *Controller) shouldBlockRollout(app *shipper.Application) bool {
	nsRBs, err := c.rbLister.RolloutBlocks(app.Namespace).List(labels.Everything())
	if err != nil {
		runtime.HandleError(fmt.Errorf("error syncing Application %q Because of namespace RolloutBlocks (will retry): %s", app.Name, err))
	}

	gbRBs, err := c.rbLister.RolloutBlocks(shipper.ShipperNamespace).List(labels.Everything())
	if err != nil {
		runtime.HandleError(fmt.Errorf("error syncing Application %q Because of global RolloutBlocks (will retry): %s", app.Name, err))
	}

	overrideRB, ok := app.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation]
	if !ok {
		return false
	}

	overrideRolloutBlock, eventMessage, err := rolloutblockUtil.ShouldOverrideRolloutBlock(overrideRB, nsRBs, gbRBs)
	if err != nil {
		c.recorder.Event(app, corev1.EventTypeWarning, "Overriding RolloutBlock", err.Error())

		switch err.(type) {
		case shippererrors.InvalidRolloutBlockOverrideError:
			// remove from annotation!
		default:
			runtime.HandleError(fmt.Errorf("error overriding rollout block %s", err.Error()))
		}
		return true
	}

	if !overrideRolloutBlock {
		c.recorder.Event(app, corev1.EventTypeWarning, "RolloutBlock", eventMessage)
	} else {
		c.recorder.Event(app, corev1.EventTypeNormal, "Overriding RolloutBlock", eventMessage)
	}
	return !overrideRolloutBlock
}

func (c *Controller) updateApplicationRolloutCondition(rbs []*shipper.RolloutBlock, app *shipper.Application) {
	glog.Info("HILLA updating application condition!!!")
	if len(rbs) > 0 {
		var sb strings.Builder
		for _, rb := range rbs {
			sb.WriteString(rb.Name + " ")
		}
		rolloutBlockCond := apputil.NewApplicationCondition(shipper.ApplicationConditionTypeRolloutBlock, corev1.ConditionTrue, sb.String(), "")
		apputil.SetApplicationCondition(&app.Status, *rolloutBlockCond)
	} else {
		rolloutBlockCond := apputil.NewApplicationCondition(shipper.ApplicationConditionTypeRolloutBlock, corev1.ConditionFalse, "", "")
		apputil.SetApplicationCondition(&app.Status, *rolloutBlockCond)
	}
}
