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

package application

kapow

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
	"github.com/bookingcom/shipper/pkg/chart"
	clientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	informers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	listers "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1"
	"github.com/bookingcom/shipper/pkg/controller"
)

const (
	reasonFailed = "placeholder error message"
)

const AgentName = "application-controller"

// Controller is a Kubernetes controller that creates Releases from
// Applications.
type Controller struct {
	shipperClientset clientset.Interface

	appLister    listers.ApplicationLister
	appSynced    cache.InformerSynced
	appWorkqueue workqueue.RateLimitingInterface

	relLister listers.ReleaseLister
	relSynced cache.InformerSynced

	recorder   record.EventRecorder
	fetchChart chart.FetchFunc
}

// NewController returns a new Application controller.
func NewController(
	shipperClientset clientset.Interface,
	shipperInformerFactory informers.SharedInformerFactory,
	recorder record.EventRecorder,
	chartFetchFunc chart.FetchFunc,
) *Controller {
	appInformer := shipperInformerFactory.Shipper().V1().Applications()
	relInformer := shipperInformerFactory.Shipper().V1().Releases()

	c := &Controller{
		shipperClientset: shipperClientset,

		appLister:    appInformer.Lister(),
		appSynced:    appInformer.Informer().HasSynced,
		appWorkqueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "application_controller_applications"),

		relLister: relInformer.Lister(),
		relSynced: relInformer.Informer().HasSynced,

		recorder:   recorder,
		fetchChart: chartFetchFunc,
	}

	appInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: c.enqueueApp,
		UpdateFunc: func(old, new interface{}) {
			oldApp, oldOk := old.(*shipperv1.Application)
			newApp, newOk := new.(*shipperv1.Application)
			if oldOk && newOk && oldApp.ResourceVersion == newApp.ResourceVersion {
				glog.V(4).Info("Received Application re-sync Update")
				return
			}

			c.enqueueApp(new)
		},
		DeleteFunc: c.enqueueApp,
	})

	relInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: c.enqueueRel,
		UpdateFunc: func(old, new interface{}) {
			oldRel, oldOk := old.(*shipperv1.Release)
			newRel, newOk := new.(*shipperv1.Release)
			if oldOk && newOk && oldRel.ResourceVersion == newRel.ResourceVersion {
				glog.V(4).Info("Received Release re-sync Update")
				return
			}

			c.enqueueRel(newRel)
		},
		DeleteFunc: c.enqueueRel,
	})

	return c
}

// Run starts Application controller workers and blocks until stopCh is
// closed.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) {
	defer runtime.HandleCrash()
	defer c.appWorkqueue.ShutDown()

	glog.V(2).Info("Starting Application controller")
	defer glog.V(2).Info("Shutting down Application controller")

	if !cache.WaitForCacheSync(stopCh, c.appSynced, c.relSynced) {
		runtime.HandleError(fmt.Errorf("failed to sync caches for the Application controller"))
		return
	}

	for i := 0; i < threadiness; i++ {
		go wait.Until(c.applicationWorker, time.Second, stopCh)
	}

	glog.V(2).Info("Started Application controller")

	<-stopCh
}

func (c *Controller) applicationWorker() {
	for c.processNextWorkItem() {
	}
}

func (c *Controller) processNextWorkItem() bool {
	obj, shutdown := c.appWorkqueue.Get()
	if shutdown {
		return false
	}

	// We're done processing this object.
	defer c.appWorkqueue.Done(obj)

	var (
		key string
		ok  bool
	)

	if key, ok = obj.(string); !ok {
		// Do not attempt to process invalid objects again.
		c.appWorkqueue.Forget(obj)
		runtime.HandleError(fmt.Errorf("invalid object key: %#v", obj))
		return true
	}

	if err := c.syncApplication(key); err != nil {
		runtime.HandleError(fmt.Errorf("error syncing %q (will retry): %s", key, err))
		c.appWorkqueue.AddRateLimited(key)
		return true
	}

	// Do not requeue this object because it's already processed.
	c.appWorkqueue.Forget(obj)

	glog.V(4).Infof("Successfully synced %q", key)

	return true
}

// Translate Release changes into Application changes (to handle e.g. aborts).
func (c *Controller) enqueueRel(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	rel, ok := obj.(*shipperv1.Release)
	if !ok {
		runtime.HandleError(fmt.Errorf("%s is not a shipperv1.Release", key))
		return
	}

	if n := len(rel.OwnerReferences); n != 1 {
		runtime.HandleError(fmt.Errorf("expected exactly one owner for Release %q but got %d", key, n))
		return
	}

	owner := rel.OwnerReferences[0]

	c.appWorkqueue.AddRateLimited(fmt.Sprintf("%s/%s", rel.GetNamespace(), owner.Name))
}

func (c *Controller) enqueueApp(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	c.appWorkqueue.AddRateLimited(key)
}

func (c *Controller) syncApplication(key string) error {
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %q", key))
		return nil
	}

	app, err := c.appLister.Applications(ns).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			glog.V(3).Infof("Application %q has been deleted", key)
			return nil
		}

		return err
	}

	// NEVER modify objects from the store. It's a read-only, local cache.
	app = app.DeepCopy()
	if err := c.processApplication(app); err != nil {
		c.recorder.Event(app, corev1.EventTypeWarning, reasonFailed, err.Error())
		return err
	}

	return nil
}

func (c *Controller) processApplication(app *shipperv1.Application) error {
	latestReleaseRecord := c.getEndOfHistory(app)

	if latestReleaseRecord == nil {
		glog.Infof("Application %q is getting its first Release", controller.MetaKey(app))
		return c.createReleaseForApplication(app)
	}

	latestRelease, err := c.relLister.Releases(app.GetNamespace()).Get(latestReleaseRecord.Name)
	if err != nil {
		if errors.IsNotFound(err) {
			if latestReleaseRecord.Status == shipperv1.ReleaseRecordWaitingForObject {
				// This means we failed to create the Release after adding the historical
				// entry; try again to create it.
				glog.Infof("Application %q has uncommitted history entry %v",
					controller.MetaKey(app), latestReleaseRecord)
				return c.createReleaseForApplication(app)
			}

			// Otherwise our history is corrupt: we may be doing a 'crash abort' where
			// the rolling-out-release was deleted, or there might just be some nonsense
			// in history. In both cases we should reset the app.Spec.Template to match
			// the latest existing release, and trim off any bad history entries before
			// that good one.
			// This prevents us from re-rolling a bad template (as in the 'crash abort'
			// case) or leaving garbage in the history.

			glog.Infof("Application %q's latest Release (%v) doesn't exist, rolling back",
				controller.MetaKey(app), latestReleaseRecord)
			return c.rollbackAppTemplate(app)
		}

		return fmt.Errorf("fetch Release %q for Application %q: %s",
			latestReleaseRecord.Name, controller.MetaKey(app), err)
	}

	// XXX this should probably also make sure that this Release is active (state
	// 'Installed' instead of 'Superseded'?) somehow if we've marched further back
	// in time.
	if identicalEnvironments(app.Spec.Template, latestRelease.Environment) {
		if latestReleaseRecord.Status == shipperv1.ReleaseRecordWaitingForObject {
			// We've recovered from failing to create a Release for a historical entry,
			// mark the application accordingly.
			glog.Infof("Application %q has uncommitted history entry %v",
				controller.MetaKey(app), latestReleaseRecord)
			return c.markReleaseCreated(latestRelease.GetName(), app)
		}

		// Clean up 'crash abort'-ed Releases from history.
		glog.Infof("Application %q's history contains aborted Releases",
			controller.MetaKey(app))
		return c.cleanHistory(app)
	}

	// Our template is not the same, so we should trigger a new rollout by creating
	// a new release.
	glog.Info("Application %q is getting a new Release", controller.MetaKey(app))
	return c.createReleaseForApplication(app)
}
