/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package shipmentorder

import (
	"fmt"
	"time"

	"github.com/golang/glog"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	clientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	informers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	listers "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1"
)

const (
	controllerMetricsName = "shipmentorder"

	reasonFailed     = "ShippingError"
	reasonTransition = "PhaseTransition"
	reasonShipping   = "Shipping"
)

// Controller is a Kubernetes controller that creates Releases from
// ShipmentOrders.
type Controller struct {
	shipperClientset     clientset.Interface
	shipmentOrdersLister listers.ShipmentOrderLister
	shipmentOrdersSynced cache.InformerSynced

	workqueue workqueue.RateLimitingInterface
	recorder  record.EventRecorder
}

// NewController returns a new ShipmentOrder controller.
func NewController(
	shipperClientset clientset.Interface,
	shipperInformerFactory informers.SharedInformerFactory,
	recorder record.EventRecorder,
) *Controller {
	informer := shipperInformerFactory.Shipper().V1().ShipmentOrders()

	controller := &Controller{
		shipperClientset:     shipperClientset,
		shipmentOrdersLister: informer.Lister(),
		shipmentOrdersSynced: informer.Informer().HasSynced,

		workqueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), controllerMetricsName),
		recorder:  recorder,
	}

	informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueShipmentOrder,
		UpdateFunc: func(old, new interface{}) {
			oldSo, oldOK := old.(*shipperv1.ShipmentOrder)
			newSo, newOK := new.(*shipperv1.ShipmentOrder)
			if oldOK && newOK && oldSo.ResourceVersion == newSo.ResourceVersion {
				glog.V(6).Info("Received ShipmentOrder re-sync Update")
				return
			}

			controller.enqueueShipmentOrder(new)
		},
		DeleteFunc: controller.enqueueShipmentOrder,
	})

	return controller
}

// Run starts ShipmentOrder controller workers and blocks until stopCh is
// closed.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	glog.V(2).Info("Starting ShipmentOrder controller")
	defer glog.V(2).Info("Shutting down ShipmentOrder controller")

	if !cache.WaitForCacheSync(stopCh, c.shipmentOrdersSynced) {
		runtime.HandleError(fmt.Errorf("failed to sync caches for the ShipmentOrder controller"))
		return
	}

	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	glog.V(4).Info("Started ShipmentOrder controller")

	<-stopCh
}

func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

func (c *Controller) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()
	if shutdown {
		return false
	}

	// We're done processing this object.
	defer c.workqueue.Done(obj)

	var (
		key string
		ok  bool
	)

	if key, ok = obj.(string); !ok {
		// Do not attempt to process invalid objects again.
		c.workqueue.Forget(obj)
		runtime.HandleError(fmt.Errorf("invalid object key: %#v", obj))
		return true
	}

	if err := c.syncOne(key); err != nil {
		runtime.HandleError(fmt.Errorf("error syncing %q: %s", key, err))
		c.workqueue.AddRateLimited(key)
		return true
	}

	// Do not requeue this object because it's already processed.
	c.workqueue.Forget(obj)

	glog.V(6).Infof("Successfully synced %q", key)

	return true
}

func (c *Controller) enqueueShipmentOrder(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	c.workqueue.AddRateLimited(key)
}

func (c *Controller) syncOne(key string) error {
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return fmt.Errorf("invalid resource key: %q", key)
	}

	so, err := c.shipmentOrdersLister.ShipmentOrders(ns).Get(name)
	if err != nil {
		// The ShipmentOrder resource may no longer exist, which is fine. We just stop
		// processing.
		if errors.IsNotFound(err) {
			glog.V(6).Infof("ShipmentOrder %q has been deleted", key)
			return nil
		}

		return err
	}

	// NEVER modify objects from the store. It's a read-only, local cache.
	so = so.DeepCopy()
	if err := c.processOrder(so); err != nil {
		c.recorder.Event(so, corev1.EventTypeWarning, reasonFailed, err.Error())
		return err
	}

	return nil
}

func (c *Controller) processOrder(so *shipperv1.ShipmentOrder) error {
	glog.V(6).Infof(`ShipmentOrder %q is %q`, metaKey(so), so.Status.Phase)

	var err error
	switch so.Status.Phase {
	case shipperv1.ShipmentOrderPhasePending:
		err = c.processPending(so)

	case shipperv1.ShipmentOrderPhaseShipping:
		err = c.processShipping(so)

	case shipperv1.ShipmentOrderPhaseShipped:
		// If we decide to start cleaning up shipped orders, this could be the place.

	default:
		err = fmt.Errorf("invalid ShipmentOrder phase: %q", so.Status.Phase)
	}

	return err
}

func (c *Controller) processPending(so *shipperv1.ShipmentOrder) error {
	return c.transitionShipmentOrderPhase(so, shipperv1.ShipmentOrderPhaseShipping)
}

func (c *Controller) processShipping(so *shipperv1.ShipmentOrder) error {
	if c.shipmentOrderHasRelease(so) {
		nextPhase := shipperv1.ShipmentOrderPhaseShipped
		glog.V(4).Infof(
			`ShipmentOrder %q is %q but actually has a Release, marking as %q`,
			metaKey(so),
			so.Status.Phase,
			nextPhase,
		)
		return c.transitionShipmentOrderPhase(so, nextPhase)
	}

	if err := c.createReleaseForShipmentOrder(so); err != nil {
		return err
	}

	return c.transitionShipmentOrderPhase(so, shipperv1.ShipmentOrderPhaseShipped)
}
