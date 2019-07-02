package rolloutblock

import (
	"fmt"
	"strings"
	"time"

	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	clientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shipperlisters "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	stringUtil "github.com/bookingcom/shipper/pkg/util/string"
)

const (
	AgentName = "rolloutblock-controller"

	// maxRetries is the number of times an RolloutBlock will be retried before
	// we drop it out of the rolloutblock workqueue. The number is chosen with
	// the default rate limiter in mind. This results in the following backoff
	// times: 5ms, 10ms, 20ms, 40ms, 80ms, 160ms, 320ms, 640ms, 1.3s, 2.6s,
	// 5.1s, 10.2s.
	maxRetries = 11
)

// Controller is a Kubernetes controller that updates rolloutblock objects.
// The RolloutBlock objects' main objective is to block application and releases rollout during
// an outage.
//
// RolloutBlock Controller has one primary workqueues: a rolloutblock updater
// object queue.
type Controller struct {
	shipperClientset clientset.Interface
	recorder         record.EventRecorder

	applicationLister  shipperlisters.ApplicationLister
	applicationsSynced cache.InformerSynced

	releaseLister  shipperlisters.ReleaseLister
	releasesSynced cache.InformerSynced

	rolloutBlockLister shipperlisters.RolloutBlockLister
	rolloutBlockSynced cache.InformerSynced

	rolloutblockWorkqueue workqueue.RateLimitingInterface
}

// NewController returns a new RolloutBlock controller.
func NewController(
	shipperClientset clientset.Interface,
	informerFactory shipperinformers.SharedInformerFactory,
	recorder record.EventRecorder,
) *Controller {
	applicationInformer := informerFactory.Shipper().V1alpha1().Applications()
	releaseInformer := informerFactory.Shipper().V1alpha1().Releases()
	rolloutBlockInformer := informerFactory.Shipper().V1alpha1().RolloutBlocks()

	glog.Info("Building a RolloutBlock controller")

	controller := &Controller{
		recorder:         recorder,
		shipperClientset: shipperClientset,

		applicationLister:  applicationInformer.Lister(),
		applicationsSynced: applicationInformer.Informer().HasSynced,

		releaseLister:  releaseInformer.Lister(),
		releasesSynced: releaseInformer.Informer().HasSynced,

		rolloutBlockLister: rolloutBlockInformer.Lister(),
		rolloutBlockSynced: rolloutBlockInformer.Informer().HasSynced,

		rolloutblockWorkqueue: workqueue.NewNamedRateLimitingQueue(
			workqueue.DefaultControllerRateLimiter(),
			"rolloutblock_controller_rolloutblocks",
		),
	}

	glog.Info("Setting up event handlers")

	releaseInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: controller.onAddRelease,
			UpdateFunc: func(oldObj, newObj interface{}) {
				controller.onUpdateRelease(oldObj, newObj)
			},
			DeleteFunc: controller.onDeleteRelease,
		})

	applicationInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: controller.onAddApplication,
			UpdateFunc: func(oldObj, newObj interface{}) {
				controller.onUpdateApplication(oldObj, newObj)
			},
			DeleteFunc: controller.onDeleteApplication,
		})

	rolloutBlockInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: controller.onAddRolloutBlock,
		})

	return controller
}

func (c *Controller) onAddRelease(obj interface{}) {
	rel, ok := obj.(*shipper.Release)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.Release: %#v", obj))
		return
	}

	key, err := cache.MetaNamespaceKeyFunc(rel)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	overrideRB, ok := rel.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation]
	if !ok {
		overrideRB = ""
		return
	}

	overrideRBs := strings.Split(overrideRB, ",")
	for _, rbKey := range overrideRBs {
		rolloutBlockUpdater := RolloutBlockUpdater{
			rbKey,
			OverridingRelease,
			key,
			false,
		}
		c.rolloutblockWorkqueue.Add(rolloutBlockUpdater)
	}
}

func (c *Controller) onDeleteRelease(obj interface{}) {
	release, ok := obj.(*shipper.Release)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.Application: %#v", obj))
		return
	}

	key, err := cache.MetaNamespaceKeyFunc(release)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	overrideRB, ok := release.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation]
	if !ok {
		overrideRB = ""
		return
	}

	overrideRBs := strings.Split(overrideRB, ",")
	for _, rbKey := range overrideRBs {
		rolloutBlockUpdater := RolloutBlockUpdater{
			rbKey,
			OverridingRelease,
			key,
			true,
		}
		c.rolloutblockWorkqueue.Add(rolloutBlockUpdater)
	}
}

func (c *Controller) onUpdateRelease(oldObj, newObj interface{}) {
	oldRel, ok := oldObj.(*shipper.Release)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.Release: %#v", oldObj))
		return
	}

	newRel, ok := newObj.(*shipper.Release)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.Release: %#v", newObj))
		return
	}

	newOverride, newOk := newRel.Annotations[shipper.RolloutBlocksOverrideAnnotation]
	oldOverride, oldOk := oldRel.Annotations[shipper.RolloutBlocksOverrideAnnotation]
	if !oldOk {
		oldOverride = ""
	}
	if !newOk {
		newOverride = ""
	}

	if newOverride == oldOverride {
		return
	}

	newOverrides := strings.Split(newOverride, ",")
	oldOverrides := strings.Split(oldOverride, ",")

	relKey, err := cache.MetaNamespaceKeyFunc(newRel)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	// add all new release overrides to RBs status
	for _, rbKey := range newOverrides {
		rolloutBlockUpdater := RolloutBlockUpdater{
			rbKey,
			OverridingRelease,
			relKey,
			false,
		}
		c.rolloutblockWorkqueue.Add(rolloutBlockUpdater)
	}

	// remove deleted release overrides from RBs status
	removedRBs := stringUtil.SetDifference(oldOverrides, newOverrides)
	for _, rbKey := range removedRBs {
		rolloutBlockUpdater := RolloutBlockUpdater{
			rbKey,
			OverridingRelease,
			relKey,
			true,
		}
		c.rolloutblockWorkqueue.Add(rolloutBlockUpdater)
	}

}

func (c *Controller) onAddApplication(obj interface{}) {
	app, ok := obj.(*shipper.Application)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.Application: %#v", obj))
		return
	}

	key, err := cache.MetaNamespaceKeyFunc(app)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	overrideRB, ok := app.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation]
	if !ok {
		overrideRB = ""
		return
	}

	overrideRBs := strings.Split(overrideRB, ",")
	for _, rbKey := range overrideRBs {
		rolloutBlockUpdater := RolloutBlockUpdater{
			rbKey,
			OverridingApplication,
			key,
			false,
		}

		c.rolloutblockWorkqueue.Add(rolloutBlockUpdater)
	}
}

func (c *Controller) onDeleteApplication(obj interface{}) {
	app, ok := obj.(*shipper.Application)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.Application: %#v", obj))
		return
	}

	key, err := cache.MetaNamespaceKeyFunc(app)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	overrideRB, ok := app.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation]
	if !ok {
		overrideRB = ""
		return
	}

	overrideRBs := strings.Split(overrideRB, ",")
	for _, rbKey := range overrideRBs {
		rolloutBlockUpdater := RolloutBlockUpdater{
			rbKey,
			OverridingApplication,
			key,
			true,
		}
		c.rolloutblockWorkqueue.Add(rolloutBlockUpdater)
	}
}

func (c *Controller) onUpdateApplication(oldObj, newObj interface{}) {
	oldApp, ok := oldObj.(*shipper.Application)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.Application: %#v", oldObj))
		return
	}

	newApp, ok := newObj.(*shipper.Application)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.Application: %#v", newObj))
		return
	}

	newOverride, newOk := newApp.Annotations[shipper.RolloutBlocksOverrideAnnotation]
	oldOverride, oldOk := oldApp.Annotations[shipper.RolloutBlocksOverrideAnnotation]
	if !oldOk {
		oldOverride = ""
	}
	if !newOk {
		newOverride = ""
	}

	if newOverride == oldOverride {
		return
	}
	newOverrides := strings.Split(newOverride, ",")
	oldOverrides := strings.Split(oldOverride, ",")

	appKey, err := cache.MetaNamespaceKeyFunc(newApp)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	// add all new app overrides to RBs status
	for _, rbKey := range newOverrides {
		rolloutBlockUpdater := RolloutBlockUpdater{
			rbKey,
			OverridingApplication,
			appKey,
			false,
		}
		c.rolloutblockWorkqueue.Add(rolloutBlockUpdater)
	}

	// remove deleted app overrides from RBs status
	removedRBs := stringUtil.SetDifference(oldOverrides, newOverrides)
	for _, rbKey := range removedRBs {
		rolloutBlockUpdater := RolloutBlockUpdater{
			rbKey,
			OverridingApplication,
			appKey,
			true,
		}
		c.rolloutblockWorkqueue.Add(rolloutBlockUpdater)
	}
}

func (c *Controller) onAddRolloutBlock(obj interface{}) {
	rb, ok := obj.(*shipper.RolloutBlock)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.RolloutBlock: %#v", obj))
		return
	}

	key, err := cache.MetaNamespaceKeyFunc(rb)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	rolloutBlockUpdater := RolloutBlockUpdater{
		key,
		NewRolloutBlockObject,
		"",
		true,
	}

	c.rolloutblockWorkqueue.Add(rolloutBlockUpdater)
}

// Run starts RolloutBlock controller workers and blocks until stopCh is
// closed.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) {
	defer runtime.HandleCrash()
	defer c.rolloutblockWorkqueue.ShutDown()

	glog.V(2).Info("Starting RolloutBlock controller")
	defer glog.V(2).Info("Shutting down RolloutBlock controller")

	if ok := cache.WaitForCacheSync(
		stopCh,
		c.applicationsSynced,
		c.releasesSynced,
		c.rolloutBlockSynced,
	); !ok {
		runtime.HandleError(fmt.Errorf("failed to wait for caches to sync"))
		return
	}

	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runRolloutBlockWorker, time.Second, stopCh)
	}

	glog.V(4).Info("Started RolloutBlock controller")

	<-stopCh
}

func (c *Controller) runRolloutBlockWorker() {
	for c.processNextRolloutBlockWorkItem() {
	}
}

func (c *Controller) processNextRolloutBlockWorkItem() bool {
	obj, shutdown := c.rolloutblockWorkqueue.Get()
	if shutdown {
		return false
	}

	defer c.rolloutblockWorkqueue.Done(obj)

	var (
		ok                  bool
		rolloutBlockUpdater RolloutBlockUpdater
	)

	if _, ok = obj.(RolloutBlockUpdater); !ok {
		c.rolloutblockWorkqueue.Forget(obj)
		runtime.HandleError(fmt.Errorf("invalid object key (will retry: false): %#v", obj))
		return true
	}
	rolloutBlockUpdater = obj.(RolloutBlockUpdater)

	shouldRetry := false
	err := c.syncRolloutBlock(rolloutBlockUpdater)

	if err != nil {
		shouldRetry = shippererrors.ShouldRetry(err)
		runtime.HandleError(fmt.Errorf("error syncing RolloutBlock %q (will retry: %t): %s", rolloutBlockUpdater.RolloutBlockKey, shouldRetry, err.Error()))
	}

	if shouldRetry {
		if c.rolloutblockWorkqueue.NumRequeues(rolloutBlockUpdater) >= maxRetries {
			// Drop this update out of the workqueue and thus reset its
			// backoff. This limits the time a "broken" object can hog a worker.
			glog.Warningf("Update %q for RolloutBlock has been retried too many times, dropping from the queue", rolloutBlockUpdater)
			c.rolloutblockWorkqueue.Forget(rolloutBlockUpdater)

			return true
		}

		c.rolloutblockWorkqueue.AddRateLimited(rolloutBlockUpdater)

		return true
	}

	glog.V(4).Infof("Successfully synced RolloutBlock Updater %q", rolloutBlockUpdater)
	c.rolloutblockWorkqueue.Forget(obj)

	return true
}

func (c *Controller) syncRolloutBlock(rolloutBlockUpdater RolloutBlockUpdater) error {
	key := rolloutBlockUpdater.RolloutBlockKey
	updaterType := rolloutBlockUpdater.UpdaterType
	if updaterType == NewRolloutBlockObject {
		return c.syncNewRolloutBlockObject(key)
	}

	overridingKey := rolloutBlockUpdater.OverridingObjectKey
	if len(key) == 0 || len(overridingKey) == 0 {
		return nil
	}

	var err error
	isDeletedObject := rolloutBlockUpdater.IsDeletedObject

	if isDeletedObject {
		switch updaterType {
		case OverridingRelease:
			err = c.removeReleaseFromRolloutBlockStatus(overridingKey, key)
		case OverridingApplication:
			err = c.removeAppFromRolloutBlockStatus(overridingKey, key)
		}
	} else {
		switch updaterType {
		case OverridingRelease:
			err = c.addReleaseToRolloutBlockStatus(overridingKey, key)
		case OverridingApplication:
			err = c.addApplicationToRolloutBlockStatus(overridingKey, key)
		}
	}

	return err
}

func (c *Controller) syncNewRolloutBlockObject(key string) error {
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}

	rolloutBlock, err := c.rolloutBlockLister.RolloutBlocks(ns).Get(name)
	if err != nil {
		return err
	}

	var appListerFunc func(selector labels.Selector) (ret []*shipper.Application, err error)
	var relListerFunc func(selector labels.Selector) (ret []*shipper.Release, err error)
	switch ns {
	case shipper.ShipperNamespace:
		appListerFunc = c.applicationLister.List
		relListerFunc = c.releaseLister.List
	default:
		appListerFunc = c.applicationLister.Applications(ns).List
		relListerFunc = c.releaseLister.Releases(ns).List
	}

	apps, err := appListerFunc(labels.Everything())
	if err != nil {
		return err
	}
	if err = c.addApplicationsToRolloutBlocks(key, rolloutBlock, apps...); err != nil {
		return err
	}

	rels, err := relListerFunc(labels.Everything())
	if err != nil {
		return err
	}
	if err = c.addReleasesToRolloutBlocks(key, rolloutBlock, rels...); err != nil {
		return err
	}

	return nil
}


