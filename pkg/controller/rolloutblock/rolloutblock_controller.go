package rolloutblock

import (
	"fmt"
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
			AddFunc: controller.enqueueReleaseBlock,
			UpdateFunc: func(oldObj, newObj interface{}) {
				controller.enqueueReleaseBlock(newObj)
			},
			DeleteFunc: controller.enqueueReleaseBlock,
		})

	applicationInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: controller.enqueueApplicationBlock,
			UpdateFunc: func(oldObj, newObj interface{}) {
				controller.enqueueApplicationBlock(newObj)
			},
			DeleteFunc: controller.enqueueApplicationBlock,
		})

	rolloutBlockInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: controller.onAddRolloutBlock,
		})

	return controller
}

func (c *Controller) enqueueReleaseBlock(obj interface{}) {
	rel, ok := obj.(*shipper.Release)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.Release: %#v", obj))
		return
	}

	overrideRBs := rolloutblock.NewOverride(rel.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation])
	// We are pushing all RolloutBlocks that this release is overriding to the queue. We're pushing
	// them separately in order to utilize the deduplication quality of the queue. This way the
	// controller will handle each RolloutBlock object once.
	for rbKey := range overrideRBs {
		c.rolloutblockWorkqueue.Add(rbKey)
	}
}

func (c *Controller) enqueueApplicationBlock(obj interface{}) {
	app, ok := obj.(*shipper.Application)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.Application: %#v", obj))
		return
	}

	overrideRBs := rolloutblock.NewOverride(app.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation])
	// We are pushing all RolloutBlocks that this application is overriding to the queue. We're pushing
	// them separately in order to utilize the deduplication quality of the queue. This way the
	// controller will handle each RolloutBlock object once.
	for rbKey := range overrideRBs {
		c.rolloutblockWorkqueue.Add(rbKey)
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
			glog.Warningf("Update %q for RolloutBlock has been retried too many times, dropping from the queue", key)
			c.rolloutblockWorkqueue.Forget(key)

			return true
		}

		c.rolloutblockWorkqueue.AddRateLimited(key)

		return true
	}

	glog.V(4).Infof("Successfully synced RolloutBlock Updater %q", key)
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
