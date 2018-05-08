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

import (
	"fmt"
	"strconv"
	"time"

	"github.com/golang/glog"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	"github.com/bookingcom/shipper/pkg/conditions"
	"github.com/bookingcom/shipper/pkg/controller"
)

const (
	AgentName                   = "application-controller"
	DefaultRevisionHistoryLimit = 20
	MinRevisionHistoryLimit     = 1
	MaxRevisionHistoryLimit     = 1000
)

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
		UpdateFunc: func(_, new interface{}) {
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

	if shouldRetry := c.syncApplication(key); shouldRetry {
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
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	c.appWorkqueue.AddRateLimited(key)
}

func (c *Controller) syncApplication(key string) bool {
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %q", key))
		return false
	}

	app, err := c.appLister.Applications(ns).Get(name)
	if err != nil {
		if kerrors.IsNotFound(err) {
			glog.V(3).Infof("Application %q has been deleted", key)
			return false
		}

		runtime.HandleError(fmt.Errorf("error fetching %q from lister (will retry): %s", key, err))
		return true
	}

	// NEVER modify objects from the store. It's a read-only, local cache.
	// do process application
	// transform any error it has into a condition for status
	// get the update history list
	// apply it to the status
	// update status
	app = app.DeepCopy()
	// processApplication will mutate the app struct for status condition updates and annotation changes
	// an err may be returned to signal a retry, but it will also be expressed as a condition
	err = c.processApplication(app)
	shouldRetry := false
	if err != nil {
		shouldRetry = true
		runtime.HandleError(fmt.Errorf("error syncing %q (will retry): %s", key, err))
		c.recorder.Event(app, corev1.EventTypeWarning, "FailedApplication", err.Error())
	}

	newHistory, err := c.getAppHistory(app)
	if err == nil {
		app.Status.History = newHistory
	} else {
		shouldRetry = true
		runtime.HandleError(fmt.Errorf("error fetching history for app %q (will retry): %s", key, err))
	}

	newState, err := c.computeState(app)
	if err == nil {
		app.Status.State = newState
	} else {
		shouldRetry = true
		runtime.HandleError(fmt.Errorf("error computing state for app %q (will retry): %s", key, err))
	}

	// TODO(asurikov): change to UpdateStatus when it's available.
	_, err = c.shipperClientset.ShipperV1().Applications(app.Namespace).Update(app)
	if err != nil {
		runtime.HandleError(fmt.Errorf("error updating %q (will retry): %s", key, err))
		shouldRetry = true
	}

	return shouldRetry
}

/*
* get all the releases owned by this application
* if 0, create new one (generation 0), return
* if >1, find latest (highest generation #), compare hash of that one to application template hash
* if same, do nothing
* if different, create new release (highest generation # + 1)
 */
func (c *Controller) processApplication(app *shipperv1.Application) error {
	if app.Annotations == nil {
		app.Annotations = map[string]string{}
	}

	if app.Spec.RevisionHistoryLimit == nil {
		var i int32 = DefaultRevisionHistoryLimit
		app.Spec.RevisionHistoryLimit = &i
	}

	// this would be better as OpenAPI validation, but it does not support
	// 'nullable' so it cannot be an optional field
	if *app.Spec.RevisionHistoryLimit < MinRevisionHistoryLimit {
		var min int32 = MinRevisionHistoryLimit
		app.Spec.RevisionHistoryLimit = &min
	}

	if *app.Spec.RevisionHistoryLimit > MaxRevisionHistoryLimit {
		var max int32 = MaxRevisionHistoryLimit
		app.Spec.RevisionHistoryLimit = &max
	}

	// clean up excessive releases regardless of exit path
	defer func() {
		releases, err := c.getSortedAppReleases(app)
		if err != nil {
			runtime.HandleError(err)
			return
		}

		// delete the first X ordered by generation. bail out on any error so that
		// we maintain the invariant that we always delete oldest first (rather than
		// failing to delete A and successfully deleting B and C in an 'A B C' history)
		overhead := len(releases) - int(*app.Spec.RevisionHistoryLimit)
		for i := 0; i < overhead; i++ {
			rel := releases[i]
			err = c.shipperClientset.ShipperV1().Releases(app.GetNamespace()).Delete(rel.GetName(), &metav1.DeleteOptions{})
			if err != nil {
				runtime.HandleError(err)
				return
			}
		}
	}()

	latestRelease, err := c.getLatestReleaseForApp(app)
	if err != nil {
		app.Status.Conditions = conditions.SetApplicationCondition(
			app.Status.Conditions,
			shipperv1.ApplicationConditionTypeValidHistory,
			corev1.ConditionFalse,
			conditions.FetchReleaseFailed,
			fmt.Sprintf("could not fetch the latest release: %q", err),
		)
		return err
	}

	if latestRelease == nil {
		err = c.createReleaseForApplication(app, 0)
		if err != nil {
			app.Status.Conditions = conditions.SetApplicationCondition(
				app.Status.Conditions,
				shipperv1.ApplicationConditionTypeReleaseSynced,
				corev1.ConditionFalse,
				conditions.CreateReleaseFailed,
				fmt.Sprintf("could not create a new release: %q", err),
			)

			return err
		}
		app.Annotations[shipperv1.AppHighestObservedGenerationAnnotation] = "0"
		app.Status.Conditions = conditions.SetApplicationCondition(
			app.Status.Conditions,
			shipperv1.ApplicationConditionTypeReleaseSynced,
			corev1.ConditionTrue,
			"", "",
		)
		return nil
	}

	generation, err := controller.GetReleaseGeneration(latestRelease)
	if err != nil {
		app.Status.Conditions = conditions.SetApplicationCondition(
			app.Status.Conditions,
			shipperv1.ApplicationConditionTypeValidHistory,
			corev1.ConditionFalse,
			conditions.BrokenReleaseGeneration,
			fmt.Sprintf("could not get the generation annotation from release %q: %q", latestRelease.GetName(), err),
		)

		return err
	}

	highestObserved, err := getAppHighestObservedGeneration(app)
	if err != nil {
		app.Status.Conditions = conditions.SetApplicationCondition(
			app.Status.Conditions,
			shipperv1.ApplicationConditionTypeValidHistory,
			corev1.ConditionFalse,
			conditions.BrokenApplicationObservedGeneration,
			fmt.Sprintf("could not get the generation annotation: %q", err),
		)

		return err
	}

	// rollback: reset app template & reset latest observed
	if generation < highestObserved {
		app.Spec.Template = *(latestRelease.Environment.DeepCopy())
		app.Annotations[shipperv1.AppHighestObservedGenerationAnnotation] = strconv.Itoa(generation)
		app.Status.Conditions = conditions.SetApplicationCondition(
			app.Status.Conditions,
			shipperv1.ApplicationConditionTypeAborting,
			corev1.ConditionTrue,
			"",
			fmt.Sprintf("abort in progress, returning state to release %q", latestRelease.GetName()),
		)

		return nil
	}

	app.Status.Conditions = conditions.SetApplicationCondition(
		app.Status.Conditions,
		shipperv1.ApplicationConditionTypeAborting,
		corev1.ConditionFalse,
		"", "",
	)

	// assume history is ok
	app.Status.Conditions = conditions.SetApplicationCondition(
		app.Status.Conditions,
		shipperv1.ApplicationConditionTypeValidHistory,
		corev1.ConditionTrue, "", "",
	)

	// ... but overwrite that condition if it is not. this means something is
	// screwy; likely a human changed an annotation themselves, or the process
	// was abnormally exited by some weird reason between the new release was
	// created and app updated with the new water mark.
	//
	// I think the best we can do is bump up to this new high water mark and then proceed as normal
	if generation > highestObserved {
		highestObserved = generation
		app.Annotations[shipperv1.AppHighestObservedGenerationAnnotation] = strconv.Itoa(generation)

		app.Status.Conditions = conditions.SetApplicationCondition(
			app.Status.Conditions,
			shipperv1.ApplicationConditionTypeValidHistory,
			corev1.ConditionFalse,
			conditions.BrokenReleaseGeneration,
			fmt.Sprintf("the generation on release %q (%d) is higher than the highest observed by this application (%d). syncing application's highest observed generation to match. this should self-heal.", latestRelease.GetName(), generation, highestObserved),
		)
	}

	app.Status.Conditions = conditions.SetApplicationCondition(
		app.Status.Conditions,
		shipperv1.ApplicationConditionTypeValidHistory,
		corev1.ConditionTrue,
		"", "",
	)

	// great! nothing to do. highestObserved == latestRelease && the templates are identical
	if identicalEnvironments(app.Spec.Template, latestRelease.Environment) {
		// explicitly setting the annotation here helps recover from a broken 0 case
		app.Annotations[shipperv1.AppHighestObservedGenerationAnnotation] = strconv.Itoa(generation)
		app.Status.Conditions = conditions.SetApplicationCondition(
			app.Status.Conditions,
			shipperv1.ApplicationConditionTypeReleaseSynced,
			corev1.ConditionTrue,
			"", "",
		)
		return nil
	}

	// the normal case: the application template has changed so we should create a new release
	newGen := highestObserved + 1
	err = c.createReleaseForApplication(app, newGen)
	if err != nil {
		app.Status.Conditions = conditions.SetApplicationCondition(
			app.Status.Conditions,
			shipperv1.ApplicationConditionTypeReleaseSynced,
			corev1.ConditionFalse,
			conditions.CreateReleaseFailed,
			fmt.Sprintf("could not create a new release: %q", err),
		)
		return err
	}

	app.Annotations[shipperv1.AppHighestObservedGenerationAnnotation] = strconv.Itoa(newGen)
	app.Status.Conditions = conditions.SetApplicationCondition(
		app.Status.Conditions,
		shipperv1.ApplicationConditionTypeReleaseSynced,
		corev1.ConditionTrue,
		"", "",
	)

	return nil
}
