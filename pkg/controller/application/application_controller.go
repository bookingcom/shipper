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
/*
TODO

when app updated: check if template matches head of lineage release + no dangling pointer. if not, create new one with current template.

1) no dangling pointer, template matches head of lineage: ALL GOOD
2) no dangling pointer, template does NOT match head of lineage: create new release
3) dangling pointer, template DOES match head of lineage: fix pointer, ALL GOOD
4) dangling pointer, template does NOT match head of lineage: fix pointer, ALL GOOD (overwrite existing app template with head of lineage release contents)

if lineage is broken by deleted release, somehow... don't do that?

*/

package application

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

	relLister    listers.ReleaseLister
	relSynced    cache.InformerSynced
	relWorkqueue workqueue.RateLimitingInterface

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

		relLister:    relInformer.Lister(),
		relSynced:    relInformer.Informer().HasSynced,
		relWorkqueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "application_controller_releases"),

		recorder:   recorder,
		fetchChart: chartFetchFunc,
	}

	enqueueApp := func(obj interface{}) { enqueueWorkItem(c.appWorkqueue, obj) }
	appInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: enqueueApp,
		UpdateFunc: func(old, new interface{}) {
			oldApp, oldOk := old.(*shipperv1.Application)
			newApp, newOk := new.(*shipperv1.Application)
			if oldOk && newOk && oldApp.ResourceVersion == newApp.ResourceVersion {
				glog.V(6).Info("Received Application re-sync Update")
				return
			}

			enqueueApp(new)
		},
		DeleteFunc: enqueueApp,
	})

	relInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: c.enqueueRel,
		UpdateFunc: func(old, new interface{}) {
			// Not checking resource version here because we might need to fix the
			// successor pointer even when there hasn't been a change to this particular
			// Release.
			oldRel, oldOk := old.(*shipperv1.Release)
			newRel, newOk := new.(*shipperv1.Release)
			if oldOk && newOk && oldRel.ResourceVersion == newRel.ResourceVersion {
				glog.V(6).Info("Received Release re-sync Update")
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
	defer c.relWorkqueue.ShutDown()

	glog.V(2).Info("Starting Application controller")
	defer glog.V(2).Info("Shutting down Application controller")

	if !cache.WaitForCacheSync(stopCh, c.appSynced, c.relSynced) {
		runtime.HandleError(fmt.Errorf("failed to sync caches for the Application controller"))
		return
	}

	for i := 0; i < threadiness; i++ {
		go wait.Until(c.applicationWorker, time.Second, stopCh)
	}

	glog.V(4).Info("Started Application controller")

	<-stopCh
}

func (c *Controller) applicationWorker() {
	for processNextWorkItem(c.appWorkqueue, c.syncApplication) {
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

// translate a release update into an application update so that we catch deletes immediately
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

	if len(rel.OwnerReferences) != 1 {
		runtime.HandleError(fmt.Errorf("%s does not have a single owner", key))
		return
	}

	owner := rel.OwnerReferences[0]

	c.appWorkqueue.AddRateLimited(fmt.Sprintf("%s/%s", rel.GetNamespace(), owner.Name))
}

func enqueueWorkItem(wq workqueue.RateLimitingInterface, obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	wq.AddRateLimited(key)
}

func (c *Controller) syncApplication(key string) error {
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return fmt.Errorf("invalid resource key: %q", key)
	}

	app, err := c.appLister.Applications(ns).Get(name)
	if err != nil {
		// The Application resource may no longer exist, which is fine. We just stop
		// processing.
		if errors.IsNotFound(err) {
			glog.V(6).Infof("Application %q has been deleted", key)
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

// TODO: status and events updates
func (c *Controller) processApplication(app *shipperv1.Application) error {
	glog.V(0).Infof(`Application %q is %q`, metaKey(app), app.Status)
	latestReleaseRecord := c.getEndOfHistory(app)

	// no releases present, so unconditionally trigger a deployment by creating a new release
	if latestReleaseRecord == nil {
		return c.createReleaseForApplication(app)
	}

	latestRelease, err := c.relLister.Releases(app.GetNamespace()).Get(latestReleaseRecord.Name)
	if err != nil {
		if errors.IsNotFound(err) {
			// this means we failed to create the release after adding the historical entry; try again to create it.
			if latestReleaseRecord.Status == shipperv1.ReleaseRecordWaitingForObject {
				return c.createReleaseForApplication(app)
			}

			// otherwise our history is corrupt: we may be doing a 'crash abort'
			// where the rolling-out-release was deleted, or there might just be
			// some nonsense in history. In both cases we should reset the
			// app.Spec.Template to match the latest existing release, and trim
			// off any bad history entries before that good one.

			// This prevents us from re-rolling a bad template (as in the
			// 'crash abort' case) or leaving garbage in the history.
			return c.rollbackAppTemplate(app)
		}

		return fmt.Errorf("latestRelease %s for app %q could not be fetched: %q", latestReleaseRecord.Name, metaKey(app), err)
	}

	// if our template is in sync with the current leading release, we have
	// nothing to do this should probably also make sure that this release is
	// active (state 'Installed' instead of 'Superseded'?) somehow if we've
	// marched further back in time
	if identicalEnvironments(app.Spec.Template, latestRelease.Environment) {
		if latestReleaseRecord.Status == shipperv1.ReleaseRecordWaitingForObject {
			return c.markReleaseCreated(latestRelease.GetName(), app)
		}
		return c.cleanHistory(app)
	}

	// our template is not the same, so we should trigger a new rollout by creating a new release
	return c.createReleaseForApplication(app)
}
