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
	reasonFailed     = "ShippingError"
	reasonTransition = "PhaseTransition"
	reasonShipping   = "Shipping"
)

// Controller is a Kubernetes controller that creates Releases from
// ShipmentOrders.
type Controller struct {
	shipperClientset clientset.Interface

	soLister    listers.ShipmentOrderLister
	soSynced    cache.InformerSynced
	soWorkqueue workqueue.RateLimitingInterface

	relLister    listers.ReleaseLister
	relSynced    cache.InformerSynced
	relWorkqueue workqueue.RateLimitingInterface

	recorder record.EventRecorder
}

// NewController returns a new ShipmentOrder controller.
func NewController(
	shipperClientset clientset.Interface,
	shipperInformerFactory informers.SharedInformerFactory,
	recorder record.EventRecorder,
) *Controller {
	soInformer := shipperInformerFactory.Shipper().V1().ShipmentOrders()
	relInformer := shipperInformerFactory.Shipper().V1().Releases()

	c := &Controller{
		shipperClientset: shipperClientset,

		soLister:    soInformer.Lister(),
		soSynced:    soInformer.Informer().HasSynced,
		soWorkqueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "shipmentorder"),

		relLister:    relInformer.Lister(),
		relSynced:    relInformer.Informer().HasSynced,
		relWorkqueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "release"),

		recorder: recorder,
	}

	enqueueRel := func(obj interface{}) { enqueueWorkItem(c.relWorkqueue, obj) }
	relInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: enqueueRel,
		UpdateFunc: func(_, new interface{}) {
			// Not checking resource version here because we might need to fix the
			// successor pointer even when there hasn't been a change to this particular
			// Release.
			newRel, newOk := new.(*shipperv1.Release)
			if newOk && newRel.Status.Successor != nil {
				enqueueRel(newRel)
			}
		},
	})

	enqueueSo := func(obj interface{}) { enqueueWorkItem(c.soWorkqueue, obj) }
	soInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: enqueueSo,
		UpdateFunc: func(old, new interface{}) {
			oldSo, oldOk := old.(*shipperv1.ShipmentOrder)
			newSo, newOk := new.(*shipperv1.ShipmentOrder)
			if oldOk && newOk && oldSo.ResourceVersion == newSo.ResourceVersion {
				glog.V(6).Info("Received ShipmentOrder re-sync Update")
				return
			}

			enqueueSo(new)
		},
		DeleteFunc: enqueueSo,
	})

	return c
}

// Run starts ShipmentOrder controller workers and blocks until stopCh is
// closed.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) {
	defer runtime.HandleCrash()
	defer c.soWorkqueue.ShutDown()

	glog.V(2).Info("Starting ShipmentOrder controller")
	defer glog.V(2).Info("Shutting down ShipmentOrder controller")

	if !cache.WaitForCacheSync(stopCh, c.soSynced, c.relSynced) {
		runtime.HandleError(fmt.Errorf("failed to sync caches for the ShipmentOrder controller"))
		return
	}

	for i := 0; i < threadiness; i++ {
		go wait.Until(c.shipmentOrderWorker, time.Second, stopCh)
		go wait.Until(c.releaseWorker, time.Second, stopCh)
	}

	glog.V(4).Info("Started ShipmentOrder controller")

	<-stopCh
}

func (c *Controller) shipmentOrderWorker() {
	for processNextWorkItem(c.soWorkqueue, c.syncShipmentOrder) {
	}
}

func (c *Controller) releaseWorker() {
	for processNextWorkItem(c.relWorkqueue, c.syncRelease) {
	}
}

func processNextWorkItem(wq workqueue.RateLimitingInterface, handler func(string) error) bool {
	obj, shutdown := wq.Get()
	if shutdown {
		return false
	}

	// We're done processing this object.
	defer wq.Done(obj)

	var (
		key string
		ok  bool
	)

	if key, ok = obj.(string); !ok {
		// Do not attempt to process invalid objects again.
		wq.Forget(obj)
		runtime.HandleError(fmt.Errorf("invalid object key: %#v", obj))
		return true
	}

	if err := handler(key); err != nil {
		runtime.HandleError(fmt.Errorf("error syncing %q: %s", key, err))
		wq.AddRateLimited(key)
		return true
	}

	// Do not requeue this object because it's already processed.
	wq.Forget(obj)

	glog.V(6).Infof("Successfully synced %q", key)

	return true
}

func enqueueWorkItem(wq workqueue.RateLimitingInterface, obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	wq.AddRateLimited(key)
}

func (c *Controller) syncShipmentOrder(key string) error {
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return fmt.Errorf("invalid resource key: %q", key)
	}

	so, err := c.soLister.ShipmentOrders(ns).Get(name)
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

func (c *Controller) syncRelease(key string) error {
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return fmt.Errorf("invalid resource key: %q", key)
	}

	rel, err := c.relLister.Releases(ns).Get(name)
	if err != nil {
		// The Release resource may no longer exist, which is fine. We just stop
		// processing.
		if errors.IsNotFound(err) {
			glog.V(6).Infof("Release %q has been deleted", key)
			return nil
		}

		return err
	}

	if err := c.processRelease(rel); err != nil {
		c.recorder.Event(rel, corev1.EventTypeWarning, reasonFailed, err.Error())
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

func (c *Controller) processRelease(rel *shipperv1.Release) error {
	if rel.Status.Successor == nil {
		glog.V(6).Infof("Release %q does not have a successor, skipping", metaKey(rel))
		return nil
	}

	succ, err := c.relLister.Releases(rel.GetNamespace()).Get(rel.Status.Successor.Name)
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("check successor pointer for %q: %s", metaKey(rel), err)
	}

	validSuccessor := true
	// TODO verify UID
	if errors.IsNotFound(err) {
		validSuccessor = false
		glog.V(6).Infof("Release %q has a dangling successor pointer", metaKey(rel))
	} else if succ.Status.Phase == shipperv1.ReleasePhaseAborted {
		validSuccessor = false
		glog.V(6).Infof("Release %q points to an aborted Release %q", metaKey(rel), metaKey(succ))
	}

	if validSuccessor {
		glog.V(6).Infof("Release %q has a valid successor pointer to %q", metaKey(rel), metaKey(succ))
		return nil
	}

	// NEVER modify objects from the store. It's a read-only, local cache.
	rel = rel.DeepCopy()
	rel.Status.Successor = nil
	c.shipperClientset.ShipperV1().Releases(rel.GetNamespace()).Update(rel)

	c.recorder.Eventf(
		rel,
		corev1.EventTypeNormal,
		"ReleaseSuccessorFixed",
		"Fixed successor pointer for Release %q",
		metaKey(rel),
	)

	return nil
}
