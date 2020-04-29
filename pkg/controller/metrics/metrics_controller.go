package metrics

import (
	"fmt"
	"reflect"
	"sync"
	"time"

	objectutil "github.com/bookingcom/shipper/pkg/util/object"

	corev1 "k8s.io/api/core/v1"

	releaseconditions "github.com/bookingcom/shipper/pkg/util/release"

	listers "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1alpha1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	clientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"

	informers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"

	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog"
)

const (
	AgentName               = "metrics-controller"
	cacheExpirationDuration = 1 * time.Hour
)

// Controller is the controller implementation for gathering metrics
// in real-time by subscribing to events
type Controller struct {
	appLister  listers.ApplicationLister
	appsSynced cache.InformerSynced

	releaseLister  listers.ReleaseLister
	releasesSynced cache.InformerSynced

	recorder record.EventRecorder

	shipperClientset clientset.Interface

	appLastModifiedTimes     map[string]*appLastModifiedTimeEntry
	appLastModifiedTimesLock sync.Mutex

	metricsBundle *MetricsBundle
}

// appLastModifiedTimeEntry encapsulates when an app was last
// modified, and when that timestamp was recorded. This is so that we
// can later clean up data that has been in the cache for a long time
// for no reason.
type appLastModifiedTimeEntry struct {
	time      time.Time
	entryTime time.Time
}

func NewController(
	shipperClientset clientset.Interface,
	shipperInformerFactory informers.SharedInformerFactory,
	recorder record.EventRecorder,
	metricsBundle *MetricsBundle,
) *Controller {
	appInformer := shipperInformerFactory.Shipper().V1alpha1().Applications()
	relInformer := shipperInformerFactory.Shipper().V1alpha1().Releases()

	c := &Controller{
		shipperClientset: shipperClientset,

		appLister:  appInformer.Lister(),
		appsSynced: appInformer.Informer().HasSynced,

		releaseLister:  relInformer.Lister(),
		releasesSynced: relInformer.Informer().HasSynced,

		recorder:      recorder,
		metricsBundle: metricsBundle,
	}

	appInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.handleAppCreates,
		UpdateFunc: c.handleAppUpdates,
		// TODO(parhamdoustdar): handle deletes to measure how long it takes us to do garbage collection
	})

	relInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: c.handleReleaseUpdates,
		// TODO(parhamdoustdar): handle deletions as well, since we use them to do garbage collection
	})

	return c
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(stopCh <-chan struct{}) {
	defer runtime.HandleCrash()

	klog.V(2).Info("Starting Metrics controller")
	defer klog.V(2).Info("Shutting down Metrics controller")

	if !cache.WaitForCacheSync(stopCh, c.appsSynced, c.releasesSynced) {
		runtime.HandleError(fmt.Errorf("failed to wait for caches to sync"))
		return
	}

	klog.V(4).Info("Started Metrics controller")

	wait.Until(c.cleanCache, 1*time.Hour, stopCh)

	<-stopCh
}

func (c *Controller) handleAppCreates(obj interface{}) {
	app, ok := obj.(*shipper.Application)
	if !ok {
		return
	}

	c.appLastModifiedTimesLock.Lock()
	defer c.appLastModifiedTimesLock.Unlock()

	// We don't need the extra marshalling support that
	// metav1.Time provides, because we're not going to pass this
	// value around with HTTP
	c.appLastModifiedTimes[app.Name] = &appLastModifiedTimeEntry{
		time:      app.GetCreationTimestamp().Time,
		entryTime: time.Now(),
	}

	return
}

func (c *Controller) handleAppUpdates(old, new interface{}) {
	oldApp, ok := old.(*shipper.Application)
	if !ok {
		return
	}

	newApp, ok := new.(*shipper.Application)
	if !ok {
		return
	}

	// If the specs are the same, this is not a user modification
	// and should be ignored
	if !reflect.DeepEqual(oldApp.Spec, newApp.Spec) {
		return
	}

	c.appLastModifiedTimesLock.Lock()
	defer c.appLastModifiedTimesLock.Unlock()

	// Kubernetes doesn't give us the last modified time, so we'll
	// just have to use the time from our side
	// TODO: if there is a better way to do this, please go ahead!
	c.appLastModifiedTimes[newApp.Name] = &appLastModifiedTimeEntry{
		time:      time.Now(),
		entryTime: time.Now(),
	}

	return
}

func (c *Controller) handleReleaseUpdates(old, new interface{}) {
	oldRelease, ok := old.(*shipper.Release)
	if !ok {
		return
	}

	newRelease, ok := new.(*shipper.Release)
	if !ok {
		return
	}

	// If the chart wasn't installed and now it is, calculate how
	// long it took. The chart is always installed on step 0, so
	// return if we've passed that step
	if newRelease.Spec.TargetStep > 0 {
		return
	}

	oldInstallationCondition := releaseconditions.GetReleaseStrategyConditionByType(oldRelease.Status.Strategy, shipper.StrategyConditionContenderAchievedInstallation)
	newInstallationCondition := releaseconditions.GetReleaseStrategyConditionByType(newRelease.Status.Strategy, shipper.StrategyConditionContenderAchievedInstallation)
	if newInstallationCondition != nil {
		return
	}

	if oldInstallationCondition != nil && oldInstallationCondition.Status == corev1.ConditionTrue {
		// This release has already achieved installation, so ignore
		return
	}

	if newInstallationCondition.Status == corev1.ConditionFalse {
		return
	}

	// Yay! Contender has achieved installation!
	releaseName, err := cache.MetaNamespaceKeyFunc(newRelease)
	if err != nil {
		klog.Errorf("Failed to get release key: %q", err)
	}

	appName, err := objectutil.GetApplicationLabel(newRelease)
	if err != nil {
		klog.Errorf("Could not find application name for Release %q", releaseName)
	}

	lastModifiedTime, ok := c.appLastModifiedTimes[appName]
	if !ok {
		// This probably means the 8informer didn't
		// run our application create/update callback,
		// so silently ignore the error
		return
	}

	installationDuration := newInstallationCondition.LastTransitionTime.Sub(lastModifiedTime.time)
	c.metricsBundle.TimeToInstallation.Observe(installationDuration.Seconds())

	// Remove last modified time since we don't need it any more
	c.appLastModifiedTimesLock.Lock()
	delete(c.appLastModifiedTimes, appName)
	c.appLastModifiedTimesLock.Unlock()
}

func (c *Controller) cleanCache() {
	c.appLastModifiedTimesLock.Lock()
	defer c.appLastModifiedTimesLock.Unlock()

	for appName, lastModifiedTime := range c.appLastModifiedTimes {
		if time.Since(lastModifiedTime.entryTime) > cacheExpirationDuration {
			delete(c.appLastModifiedTimes, appName)
		}
	}
}
