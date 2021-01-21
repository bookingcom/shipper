package metrics

import (
	"fmt"
	"reflect"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/bookingcom/shipper/pkg/util/application"
	"github.com/bookingcom/shipper/pkg/util/release"

	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	listers "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1alpha1"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"

	informers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"

	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
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

	shipperClientset shipperclientset.Interface

	appLastModifiedTimes     map[string]*appLastModifiedTimeEntry
	appLastModifiedTimesLock sync.RWMutex

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
	shipperClientset shipperclientset.Interface,
	shipperInformerFactory informers.SharedInformerFactory,
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

		metricsBundle: metricsBundle,

		appLastModifiedTimes: make(map[string]*appLastModifiedTimeEntry),
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
	if reflect.DeepEqual(oldApp.Spec, newApp.Spec) {
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

	// metrics-controller cares only when the following conditions apply:
	// 1. We are at the step 0, because the chart is always installed at step 0
	// 2. The release that generates the event is a contender
	// 3. The old release has not achieved installation and the new release has
	//    achived installation. In other words, we want to keep track of the event
	//    that updates the "contender achived installation" status to true.

	// See condition (1)
	if newRelease.Spec.TargetStep > 0 {
		return
	}

	// See condition (2)
	if !c.isContender(newRelease) {
		return
	}

	// See condition (3). If Status.Strategy.Conditions is nil just ignore
	if oldRelease.Status.Strategy == nil ||
		oldRelease.Status.Strategy.Conditions == nil ||
		newRelease.Status.Strategy == nil ||
		newRelease.Status.Strategy.Conditions == nil {
		return
	}

	oldInstallationCondition := release.GetReleaseStrategyConditionByType(oldRelease.Status.Strategy, shipper.StrategyConditionContenderAchievedInstallation)
	newInstallationCondition := release.GetReleaseStrategyConditionByType(newRelease.Status.Strategy, shipper.StrategyConditionContenderAchievedInstallation)

	if oldInstallationCondition == nil || newInstallationCondition == nil {
		return
	}

	// See condition (3). This release has already achieved installation, so ignore
	if oldInstallationCondition.Status == corev1.ConditionTrue {
		return
	}
	// See condition (3). The new release will not update the installation status, so ignore
	if newInstallationCondition.Status == corev1.ConditionFalse {
		return
	}

	// Yay! Contender has achieved installation!
	releaseName, err := cache.MetaNamespaceKeyFunc(newRelease)
	if err != nil {
		klog.Errorf("Failed to get release key: %q", err)
	}

	appName, ok := newRelease.GetLabels()[shipper.AppLabel]
	if !ok || len(appName) == 0 {
		klog.Errorf("Could not find application name for Release %q", releaseName)
	}

	c.appLastModifiedTimesLock.RLock()
	lastModifiedTime, ok := c.appLastModifiedTimes[appName]
	c.appLastModifiedTimesLock.RUnlock()
	if !ok {
		// This probably means the informer didn't
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

func (c *Controller) isContender(rel *shipper.Release) bool {
	// Find the application of this release
	appName, err := release.ApplicationNameForRelease(rel)
	if err != nil {
		klog.Errorf("Failed get application name for release %q: %v", rel.Name, err)
		return false
	}

	// Find all the releases of the application and sort them by generation
	rels, err := c.findReleasesForApplication(appName, rel.Namespace)
	if err != nil {
		klog.Errorf("Error getting the releases list for app %q: %v", appName, err)
		return false
	}

	// Find the contender
	contender, err := application.GetContender(appName, rels)
	if err != nil {
		klog.Errorf("Error finding the contender for app %q: %v", appName, err)
		return false
	}

	if contender.Name == rel.Name {
		return true
	}
	return false
}

func (c *Controller) findReleasesForApplication(appName, namespace string) ([]*shipper.Release, error) {
	releases, err := c.releaseLister.Releases(namespace).ReleasesForApplication(appName)
	if err != nil {
		return nil, err
	}
	return release.SortByGenerationDescending(releases), nil
}
