package rolloutblock

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	clientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shipperlisters "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1alpha1"
	shippercontroller "github.com/bookingcom/shipper/pkg/controller"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	"github.com/bookingcom/shipper/pkg/util/rolloutblock"
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
	resyncPeriod     time.Duration
	shipperClientset clientset.Interface
	recorder         record.EventRecorder

	applicationLister  shipperlisters.ApplicationLister
	applicationsSynced cache.InformerSynced

	releaseLister  shipperlisters.ReleaseLister
	releasesSynced cache.InformerSynced

	rolloutBlockLister shipperlisters.RolloutBlockLister
	rolloutBlockSynced cache.InformerSynced

	rolloutblockWorkqueue workqueue.RateLimitingInterface
	releaseWorkqueue      workqueue.RateLimitingInterface
	applicationWorkqueue  workqueue.RateLimitingInterface
}

// NewController returns a new RolloutBlock controller.
func NewController(
	shipperClientset clientset.Interface,
	informerFactory shipperinformers.SharedInformerFactory,
	recorder record.EventRecorder,
	resyncPeriod time.Duration,
) *Controller {
	applicationInformer := informerFactory.Shipper().V1alpha1().Applications()
	releaseInformer := informerFactory.Shipper().V1alpha1().Releases()
	rolloutBlockInformer := informerFactory.Shipper().V1alpha1().RolloutBlocks()

	klog.Info("Building a RolloutBlock controller")

	controller := &Controller{
		resyncPeriod:     resyncPeriod,
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
		releaseWorkqueue: workqueue.NewNamedRateLimitingQueue(
			workqueue.DefaultControllerRateLimiter(),
			"rolloutblock_controller_releases",
		),
		applicationWorkqueue: workqueue.NewNamedRateLimitingQueue(
			workqueue.DefaultControllerRateLimiter(),
			"rolloutblock_controller_applications",
		),
	}

	klog.Info("Setting up event handlers")

	releaseInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: controller.enqueueReleaseBlock,
			UpdateFunc: func(oldObj, newObj interface{}) {
				controller.onUpdateRelease(oldObj, newObj)
			},
			DeleteFunc: controller.enqueueReleaseBlock,
		})

	applicationInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: controller.enqueueApplicationBlock,
			UpdateFunc: func(oldObj, newObj interface{}) {
				controller.onUpdateApplication(oldObj, newObj)
			},
			DeleteFunc: controller.enqueueApplicationBlock,
		})

	rolloutBlockInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    controller.onAddRolloutBlock,
			DeleteFunc: controller.onDeleteRolloutBlock,
		})

	return controller
}

func (c *Controller) onUpdateRelease(oldObj interface{}, newObj interface{}) {
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

	oldOverrideRBs := rolloutblock.NewObjectNameList(oldRel.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation])
	newOverrideRBs := rolloutblock.NewObjectNameList(newRel.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation])
	// rolloutblock object should be updated if a release has added it to override annotation,
	// or removed it from override annotation
	if len(oldOverrideRBs.Diff(newOverrideRBs)) > 0 {
		if oldRel.GetResourceVersion() == newRel.GetResourceVersion() {
			c.enqueueRolloutBlockKeysLater(oldOverrideRBs)
		} else {
			c.enqueueRolloutBlockKeys(oldOverrideRBs)
		}
	} else {
		if oldRel.GetResourceVersion() == newRel.GetResourceVersion() {
			c.enqueueRolloutBlockKeysLater(newOverrideRBs)
		} else {
			c.enqueueRolloutBlockKeys(newOverrideRBs)
		}
	}
}

func (c *Controller) enqueueReleaseBlock(obj interface{}) {
	rel, ok := obj.(*shipper.Release)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.Release: %#v", obj))
		return
	}

	overrideRBs := rolloutblock.NewObjectNameList(rel.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation])
	// We are pushing all RolloutBlocks that this release is overriding to the queue. We're pushing
	// them separately in order to utilize the deduplication quality of the queue. This way the
	// controller will handle each RolloutBlock object once.
	for rbKey := range overrideRBs {
		c.rolloutblockWorkqueue.Add(rbKey)
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

	overrideRBs := rolloutblock.NewObjectNameList(newApp.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation])
	if oldApp.GetResourceVersion() == newApp.GetResourceVersion() {
		c.enqueueRolloutBlockKeysLater(overrideRBs)
	} else {
		c.enqueueRolloutBlockKeys(overrideRBs)
	}
}

func (c *Controller) enqueueApplicationBlock(obj interface{}) {
	app, ok := obj.(*shipper.Application)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.Application: %#v", obj))
		return
	}

	overrideRBs := rolloutblock.NewObjectNameList(app.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation])
	c.enqueueRolloutBlockKeys(overrideRBs)
}

func (c *Controller) enqueueRolloutBlockKeys(overrideRBs rolloutblock.ObjectNameList) {
	// We are pushing all RolloutBlocks that this application is overriding to the queue. We're pushing
	// them separately in order to utilize the deduplication quality of the queue. This way the
	// controller will handle each RolloutBlock object once.
	for rbKey := range overrideRBs {
		c.rolloutblockWorkqueue.Add(rbKey)
	}
}

func (c *Controller) enqueueRolloutBlockKeysLater(overrideRBs rolloutblock.ObjectNameList) {
	// We are pushing all RolloutBlocks that this application is overriding to the queue. We're pushing
	// them separately in order to utilize the deduplication quality of the queue. This way the
	// controller will handle each RolloutBlock object once.
	for rbKey := range overrideRBs {
		duration := shippercontroller.GetRandomDuration(c.resyncPeriod)
		c.rolloutblockWorkqueue.AddAfter(rbKey, duration)
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

	c.rolloutblockWorkqueue.Add(key)
}

func (c *Controller) onDeleteRolloutBlock(obj interface{}) {
	rb, ok := obj.(*shipper.RolloutBlock)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.RolloutBlock: %#v", obj))
		return
	}

	// When deleting a rolloutblock, update all overriding object to not override dead rolloutblock
	apps := rolloutblock.NewObjectNameList(rb.Status.Overrides.Application)
	for appKey := range apps {
		c.applicationWorkqueue.Add(appKey)
	}
	rels := rolloutblock.NewObjectNameList(rb.Status.Overrides.Release)
	for relKey := range rels {
		c.releaseWorkqueue.Add(relKey)
	}
}

// Run starts RolloutBlock controller workers and blocks until stopCh is
// closed.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) {
	defer runtime.HandleCrash()
	defer c.rolloutblockWorkqueue.ShutDown()

	klog.V(2).Info("Starting RolloutBlock controller")
	defer klog.V(2).Info("Shutting down RolloutBlock controller")

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
		go wait.Until(c.runApplicationWorker, time.Second, stopCh)
		go wait.Until(c.runReleaseWorker, time.Second, stopCh)
		go wait.Until(c.runRolloutBlockWorker, time.Second, stopCh)
	}

	klog.V(4).Info("Started RolloutBlock controller")

	<-stopCh
}

func (c *Controller) runApplicationWorker() {
	for c.processNextApplicationWorkItem() {
	}
}

func (c *Controller) runReleaseWorker() {
	for c.processNextReleaseWorkItem() {
	}
}

func (c *Controller) runRolloutBlockWorker() {
	for c.processNextRolloutBlockWorkItem() {
	}
}

func (c *Controller) processNextApplicationWorkItem() bool {
	obj, shutdown := c.applicationWorkqueue.Get()
	if shutdown {
		return false
	}

	defer c.applicationWorkqueue.Done(obj)

	var (
		ok  bool
		key string
	)

	if key, ok = obj.(string); !ok {
		c.applicationWorkqueue.Forget(obj)
		runtime.HandleError(fmt.Errorf("invalid object key (will retry: false): %#v", obj))
		return true
	}

	shouldRetry := false
	err := c.syncApplication(key)

	if err != nil {
		shouldRetry = shippererrors.ShouldRetry(err)
		runtime.HandleError(fmt.Errorf("error syncing Application %q (will retry: %t): %s", key, shouldRetry, err.Error()))
	}

	if shouldRetry {
		if c.applicationWorkqueue.NumRequeues(key) >= maxRetries {
			// Drop this update out of the workqueue and thus reset its
			// backoff. This limits the time a "broken" object can hog a worker.
			klog.Warningf("Update %q for Application has been retried too many times, dropping from the queue", key)
			c.applicationWorkqueue.Forget(key)

			return true
		}

		c.applicationWorkqueue.AddRateLimited(key)

		return true
	}

	klog.V(4).Infof("Successfully synced Application after RolloutBlock Delete %q", key)
	c.applicationWorkqueue.Forget(obj)

	return true
}

func (c *Controller) processNextReleaseWorkItem() bool {
	obj, shutdown := c.releaseWorkqueue.Get()
	if shutdown {
		return false
	}

	defer c.releaseWorkqueue.Done(obj)

	var (
		ok  bool
		key string
	)

	if key, ok = obj.(string); !ok {
		c.releaseWorkqueue.Forget(obj)
		runtime.HandleError(fmt.Errorf("invalid object key (will retry: false): %#v", obj))
		return true
	}

	shouldRetry := false
	err := c.syncRelease(key)

	if err != nil {
		shouldRetry = shippererrors.ShouldRetry(err)
		runtime.HandleError(fmt.Errorf("error syncing Release %q (will retry: %t): %s", key, shouldRetry, err.Error()))
	}

	if shouldRetry {
		if c.releaseWorkqueue.NumRequeues(key) >= maxRetries {
			// Drop this update out of the workqueue and thus reset its
			// backoff. This limits the time a "broken" object can hog a worker.
			klog.Warningf("Update %q for Release has been retried too many times, dropping from the queue", key)
			c.releaseWorkqueue.Forget(key)

			return true
		}

		c.releaseWorkqueue.AddRateLimited(key)

		return true
	}

	klog.V(4).Infof("Successfully synced Application after RolloutBlock Delete %q", key)
	c.releaseWorkqueue.Forget(obj)

	return true
}

func (c *Controller) processNextRolloutBlockWorkItem() bool {
	obj, shutdown := c.rolloutblockWorkqueue.Get()
	if shutdown {
		return false
	}

	defer c.rolloutblockWorkqueue.Done(obj)

	var (
		ok  bool
		key string
	)

	if key, ok = obj.(string); !ok {
		c.rolloutblockWorkqueue.Forget(obj)
		runtime.HandleError(fmt.Errorf("invalid object key (will retry: false): %#v", obj))
		return true
	}

	shouldRetry := false
	err := c.syncRolloutBlock(key)

	if err != nil {
		shouldRetry = shippererrors.ShouldRetry(err)
		runtime.HandleError(fmt.Errorf("error syncing RolloutBlock %q (will retry: %t): %s", key, shouldRetry, err.Error()))
	}

	if shouldRetry {
		if c.rolloutblockWorkqueue.NumRequeues(key) >= maxRetries {
			// Drop this update out of the workqueue and thus reset its
			// backoff. This limits the time a "broken" object can hog a worker.
			klog.Warningf("Update %q for RolloutBlock has been retried too many times, dropping from the queue", key)
			c.rolloutblockWorkqueue.Forget(key)

			return true
		}

		c.rolloutblockWorkqueue.AddRateLimited(key)

		return true
	}

	klog.V(4).Infof("Successfully synced RolloutBlock Updater %q", key)
	c.rolloutblockWorkqueue.Forget(obj)

	return true
}

func (c *Controller) syncRolloutBlock(key string) error {
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
	case shipper.GlobalRolloutBlockNamespace:
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

	_, err = c.shipperClientset.ShipperV1alpha1().RolloutBlocks(rolloutBlock.Namespace).Update(rolloutBlock)
	if err != nil {
		return shippererrors.NewKubeclientUpdateError(rolloutBlock, err).
			WithShipperKind("RolloutBlock")
	}

	return nil
}
