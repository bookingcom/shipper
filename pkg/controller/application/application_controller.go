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

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	clientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	informers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	listers "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/conditions"
	"github.com/bookingcom/shipper/pkg/controller"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

const (
	AgentName                   = "application-controller"
	DefaultRevisionHistoryLimit = 20
	MinRevisionHistoryLimit     = 1
	MaxRevisionHistoryLimit     = 1000

	// maxRetries is the number of times an Application will be retried before we
	// drop it out of the app workqueue. The number is chosen with the default rate
	// limiter in mind. This results in the following backoff times: 5ms, 10ms,
	// 20ms, 40ms, 80ms, 160ms, 320ms, 640ms, 1.3s, 2.6s, 5.1s, 10.2s.
	maxRetries = 11
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

	recorder record.EventRecorder
}

// NewController returns a new Application controller.
func NewController(
	shipperClientset clientset.Interface,
	shipperInformerFactory informers.SharedInformerFactory,
	recorder record.EventRecorder,
) *Controller {
	appInformer := shipperInformerFactory.Shipper().V1alpha1().Applications()
	relInformer := shipperInformerFactory.Shipper().V1alpha1().Releases()

	c := &Controller{
		shipperClientset: shipperClientset,

		appLister:    appInformer.Lister(),
		appSynced:    appInformer.Informer().HasSynced,
		appWorkqueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "application_controller_applications"),

		relLister: relInformer.Lister(),
		relSynced: relInformer.Informer().HasSynced,

		recorder: recorder,
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
			oldRel, oldOk := old.(*shipper.Release)
			newRel, newOk := new.(*shipper.Release)
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

	defer c.appWorkqueue.Done(obj)

	var (
		key string
		ok  bool
	)

	if key, ok = obj.(string); !ok {
		c.appWorkqueue.Forget(obj)
		runtime.HandleError(fmt.Errorf("invalid object key (will not retry): %#v", obj))
		return true
	}

	if shouldRetry := c.syncApplication(key); shouldRetry {
		if c.appWorkqueue.NumRequeues(key) >= maxRetries {
			// Drop the Application's key out of the workqueue and thus reset its
			// backoff. This limits the time a "broken" object can hog a worker.
			glog.Warningf("Application %q has been retried too many times, dropping from the queue", key)
			c.appWorkqueue.Forget(key)

			return true
		}

		c.appWorkqueue.AddRateLimited(key)

		return true
	}

	glog.V(4).Infof("Successfully synced Application %q", key)
	c.appWorkqueue.Forget(obj)

	return true
}

func (c *Controller) enqueueRel(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	rel, ok := obj.(*shipper.Release)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.Release: %#v", obj))
		return
	}

	if n := len(rel.OwnerReferences); n != 1 {
		runtime.HandleError(fmt.Errorf("expected exactly one owner for Release %q but got %d", key, n))
		return
	}

	owner := rel.OwnerReferences[0]

	c.appWorkqueue.Add(fmt.Sprintf("%s/%s", rel.Namespace, owner.Name))
}

func (c *Controller) enqueueApp(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	c.appWorkqueue.Add(key)
}

func (c *Controller) syncApplication(key string) bool {
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid object key (will not retry): %q", key))
		return false
	}

	app, err := c.appLister.Applications(ns).Get(name)
	if err != nil {
		if kerrors.IsNotFound(err) {
			glog.V(3).Infof("Application %q has been deleted", key)
			return false
		}

		runtime.HandleError(fmt.Errorf("error syncing Application %q (will retry): %s", key, err))
		return true
	}

	app = app.DeepCopy()

	var shouldRetry bool
	if err := c.processApplication(app); err != nil {
		shouldRetry = true
		runtime.HandleError(fmt.Errorf("error syncing Application %q (will retry): %s", key, err))
		c.recorder.Event(app, corev1.EventTypeWarning, "FailedApplication", err.Error())
	}

	if newHistory, err := c.getAppHistory(app); err == nil {
		app.Status.History = newHistory
	} else {
		shouldRetry = true
		runtime.HandleError(fmt.Errorf("error fetching history for Application %q (will retry): %s", key, err))
	}

	if newState, err := c.computeState(app); err == nil {
		app.Status.State = newState
	} else {
		shouldRetry = true
		runtime.HandleError(fmt.Errorf("error computing state for Application %q (will retry): %s", key, err))
	}

	_, err = c.shipperClientset.ShipperV1alpha1().Applications(app.Namespace).Update(app)
	if err != nil {
		shouldRetry = true
		runtime.HandleError(fmt.Errorf("error syncing Application %q (will retry): %s", key, err))
	}

	return shouldRetry
}

/*
* Get all the releases owned by this application.
* If 0, create new one (generation 0), return.
* If >1, find latest (highest generation #), compare hash of that one to application template hash.
*   If same, do nothing.
*   If different, create new release (highest generation # + 1).
 */
func (c *Controller) processApplication(app *shipper.Application) error {
	if app.Annotations == nil {
		app.Annotations = map[string]string{}
	}

	if app.Spec.RevisionHistoryLimit == nil {
		var i int32 = DefaultRevisionHistoryLimit
		app.Spec.RevisionHistoryLimit = &i
	}

	// This would be better as OpenAPI validation, but it does not support
	// 'nullable' so it cannot be an optional field.
	if *app.Spec.RevisionHistoryLimit < MinRevisionHistoryLimit {
		var min int32 = MinRevisionHistoryLimit
		app.Spec.RevisionHistoryLimit = &min
	}

	if *app.Spec.RevisionHistoryLimit > MaxRevisionHistoryLimit {
		var max int32 = MaxRevisionHistoryLimit
		app.Spec.RevisionHistoryLimit = &max
	}

	// Clean up excessive releases regardless of exit path.
	defer c.cleanUpReleasesForApplication(app)

	latestRelease, err := c.getLatestReleaseForApp(app)
	if err != nil {
		app.Status.Conditions = conditions.SetApplicationCondition(
			app.Status.Conditions,
			shipper.ApplicationConditionTypeValidHistory,
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
				shipper.ApplicationConditionTypeReleaseSynced,
				corev1.ConditionFalse,
				conditions.CreateReleaseFailed,
				fmt.Sprintf("could not create a new release: %q", err),
			)

			return err
		}
		app.Annotations[shipper.AppHighestObservedGenerationAnnotation] = "0"
		app.Status.Conditions = conditions.SetApplicationCondition(
			app.Status.Conditions,
			shipper.ApplicationConditionTypeReleaseSynced,
			corev1.ConditionTrue,
			"", "",
		)
		return nil
	}

	generation, err := controller.GetReleaseGeneration(latestRelease)
	if err != nil {
		app.Status.Conditions = conditions.SetApplicationCondition(
			app.Status.Conditions,
			shipper.ApplicationConditionTypeValidHistory,
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
			shipper.ApplicationConditionTypeValidHistory,
			corev1.ConditionFalse,
			conditions.BrokenApplicationObservedGeneration,
			fmt.Sprintf("could not get the generation annotation: %q", err),
		)

		return err
	}

	// Rollback: reset app template & reset latest observed.
	if generation < highestObserved {
		app.Spec.Template = *(latestRelease.Environment.DeepCopy())
		app.Annotations[shipper.AppHighestObservedGenerationAnnotation] = strconv.Itoa(generation)
		app.Status.Conditions = conditions.SetApplicationCondition(
			app.Status.Conditions,
			shipper.ApplicationConditionTypeAborting,
			corev1.ConditionTrue,
			"",
			fmt.Sprintf("abort in progress, returning state to release %q", latestRelease.GetName()),
		)

		return nil
	}

	app.Status.Conditions = conditions.SetApplicationCondition(
		app.Status.Conditions,
		shipper.ApplicationConditionTypeAborting,
		corev1.ConditionFalse,
		"", "",
	)

	// Assume history is ok...
	app.Status.Conditions = conditions.SetApplicationCondition(
		app.Status.Conditions,
		shipper.ApplicationConditionTypeValidHistory,
		corev1.ConditionTrue, "", "",
	)

	// ... but overwrite that condition if it is not. This means something is
	// screwy; likely a human changed an annotation themselves, or the process was
	// abnormally exited by some weird reason between the new release was created
	// and app updated with the new water mark.
	//
	// I think the best we can do is bump up to this new high water mark and then
	// proceed as normal.
	if generation > highestObserved {
		highestObserved = generation
		app.Annotations[shipper.AppHighestObservedGenerationAnnotation] = strconv.Itoa(generation)

		app.Status.Conditions = conditions.SetApplicationCondition(
			app.Status.Conditions,
			shipper.ApplicationConditionTypeValidHistory,
			corev1.ConditionFalse,
			conditions.BrokenReleaseGeneration,
			fmt.Sprintf("the generation on release %q (%d) is higher than the highest observed by this application (%d). syncing application's highest observed generation to match. this should self-heal.", latestRelease.GetName(), generation, highestObserved),
		)
	}

	app.Status.Conditions = conditions.SetApplicationCondition(
		app.Status.Conditions,
		shipper.ApplicationConditionTypeValidHistory,
		corev1.ConditionTrue,
		"", "",
	)

	// Great! Nothing to do. highestObserved == latestRelease && the templates are
	// identical.
	if identicalEnvironments(app.Spec.Template, latestRelease.Environment) {
		// explicitly setting the annotation here helps recover from a broken 0 case
		app.Annotations[shipper.AppHighestObservedGenerationAnnotation] = strconv.Itoa(generation)
		app.Status.Conditions = conditions.SetApplicationCondition(
			app.Status.Conditions,
			shipper.ApplicationConditionTypeReleaseSynced,
			corev1.ConditionTrue,
			"", "",
		)
		return nil
	}

	// The normal case: the application template has changed so we should create a
	// new release.
	newGen := highestObserved + 1
	err = c.createReleaseForApplication(app, newGen)
	if err != nil {
		app.Status.Conditions = conditions.SetApplicationCondition(
			app.Status.Conditions,
			shipper.ApplicationConditionTypeReleaseSynced,
			corev1.ConditionFalse,
			conditions.CreateReleaseFailed,
			fmt.Sprintf("could not create a new release: %q", err),
		)
		return err
	}

	app.Annotations[shipper.AppHighestObservedGenerationAnnotation] = strconv.Itoa(newGen)
	app.Status.Conditions = conditions.SetApplicationCondition(
		app.Status.Conditions,
		shipper.ApplicationConditionTypeReleaseSynced,
		corev1.ConditionTrue,
		"", "",
	)

	return nil
}

func (c *Controller) cleanUpReleasesForApplication(app *shipper.Application) {
	installedReleases := []*shipper.Release{}

	releases, err := c.getSortedAppReleases(app)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	// Delete any releases that are not installed. Don't touch the latest release
	// because a release that isn't installed and is the last release just means
	// that the user is rolling out the application.
	for i := 0; i < len(releases)-1; i++ {
		rel := releases[i]
		if releaseutil.ReleaseComplete(releases[i]) {
			installedReleases = append(installedReleases, releases[i])
			continue
		}

		err = c.shipperClientset.ShipperV1alpha1().Releases(app.GetNamespace()).Delete(rel.GetName(), &metav1.DeleteOptions{})
		if err != nil {
			if kerrors.IsNotFound(err) {
				// Skip this release: it's already deleted.
				continue
			}

			// Handle the error, but keep on going.
			runtime.HandleError(err)
		}
	}

	// Delete the first X ordered by generation. Bail out on any error so that we
	// maintain the invariant that we always delete oldest first (rather than
	// failing to delete A and successfully deleting B and C in an 'A B C'
	// history).
	overhead := len(installedReleases) - int(*app.Spec.RevisionHistoryLimit)
	for i := 0; i < overhead; i++ {
		rel := installedReleases[i]
		err = c.shipperClientset.ShipperV1alpha1().Releases(app.GetNamespace()).Delete(rel.GetName(), &metav1.DeleteOptions{})
		if err != nil {
			runtime.HandleError(err)
			return
		}
	}
}
