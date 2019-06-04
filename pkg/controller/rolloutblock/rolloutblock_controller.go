package rolloutblock

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	clientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shipperlisters "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1alpha1"
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

// Controller is a Kubernetes controller that creates a rolloutblock object.
// It's main objective is to block application and releases rollout during
// an outage.
//
// RolloutBlock Controller has 2 primary workqueues: releases and applications.
type Controller struct {
	shipperClientset clientset.Interface
	recorder         record.EventRecorder

	applicationLister  shipperlisters.ApplicationLister
	applicationsSynced cache.InformerSynced

	releaseLister  shipperlisters.ReleaseLister
	releasesSynced cache.InformerSynced

	rolloutBlockLister shipperlisters.RolloutBlockLister
	rolloutBlockSynced cache.InformerSynced

	releaseWorkqueue           workqueue.RateLimitingInterface // only for adding release
	releaseDeleteWorkqueue     workqueue.RateLimitingInterface // only for deleting release
	applicationWorkqueue       workqueue.RateLimitingInterface // only for adding application
	applicationDeleteWorkqueue workqueue.RateLimitingInterface // only for deleting application
	rolloutblockWorkqueue      workqueue.RateLimitingInterface
}

// NewController returns a new RolloutBlock controller.
func NewController(
	shipperClientset clientset.Interface,
	informerFactory shipperinformers.SharedInformerFactory,
	recorder record.EventRecorder,
) *Controller {
	applicationInformer := informerFactory.Shipper().V1alpha1().Applications()
	releaseInformer := informerFactory.Shipper().V1alpha1().Releases()
	rolloutblockInformer := informerFactory.Shipper().V1alpha1().RolloutBlocks()

	glog.Info("Building a RolloutBlock controller")

	controller := &Controller{
		recorder:         recorder,
		shipperClientset: shipperClientset,

		applicationLister:  applicationInformer.Lister(),
		applicationsSynced: applicationInformer.Informer().HasSynced,

		releaseLister:  releaseInformer.Lister(),
		releasesSynced: releaseInformer.Informer().HasSynced,

		rolloutBlockLister: rolloutblockInformer.Lister(),
		rolloutBlockSynced: rolloutblockInformer.Informer().HasSynced,

		releaseWorkqueue: workqueue.NewNamedRateLimitingQueue(
			workqueue.DefaultControllerRateLimiter(),
			"rolloutblock_controller_releases",
		),
		releaseDeleteWorkqueue: workqueue.NewNamedRateLimitingQueue(
			workqueue.DefaultControllerRateLimiter(),
			"rolloutblock_controller_deleted_releases",
		),
		applicationWorkqueue: workqueue.NewNamedRateLimitingQueue(
			workqueue.DefaultControllerRateLimiter(),
			"rolloutblock_controller_applications",
		),
		applicationDeleteWorkqueue: workqueue.NewNamedRateLimitingQueue(
			workqueue.DefaultControllerRateLimiter(),
			"rolloutblock_controller_deleted_applications",
		),
		rolloutblockWorkqueue: workqueue.NewNamedRateLimitingQueue(
			workqueue.DefaultControllerRateLimiter(),
			"rolloutblock_controller_rolloutblocks",
		),
	}

	glog.Info("Setting up event handlers")

	releaseInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: 	controller.enqueueRelease,
			UpdateFunc: func(oldObj, newObj interface{}) {
				controller.enqueueRelease(newObj)
			},
			DeleteFunc: controller.enqueueDeletedRelease,
		})

	applicationInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    controller.enqueueApplication,
			UpdateFunc: func(oldObj, newObj interface{}) {
				controller.enqueueApplication(newObj)
			},
			DeleteFunc: controller.enqueueDeletedApplication,
		})

	//rolloutblockInformer.Informer().AddEventHandler(
	//	cache.ResourceEventHandlerFuncs{
	//		AddFunc: controller.enqueueRolloutBlock,
	//		UpdateFunc: func(oldObj, newObj interface{}) {
	//			controller.enqueueRolloutBlock(newObj)
	//		},
	//		DeleteFunc: controller.enqueueRolloutBlock,
	//	})

	return controller
}

func (c *Controller) enqueueRelease(obj interface{}) {
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

	c.releaseWorkqueue.Add(key)
}

func (c *Controller) enqueueDeletedRelease(obj interface{}) {
	rel, ok := obj.(*shipper.Release)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.Release: %#v", obj))
		return
	}

	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}
	overrideRB, ok := rel.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation]
	if !ok {
		return
	}
	if len(overrideRB) < 2 {
		return
	}

	c.releaseDeleteWorkqueue.Add(fmt.Sprintf("%s*%s", key, overrideRB))
}

func (c *Controller) enqueueApplication(obj interface{}) {
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

	c.applicationWorkqueue.Add(key)
}

func (c *Controller) enqueueDeletedApplication(obj interface{}) {
	app, ok := obj.(*shipper.Application)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.Application: %#v", obj))
		return
	}

	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}
	overrideRB, ok := app.GetAnnotations()[shipper.RolloutBlocksOverrideAnnotation]
	if !ok {
		return
	}
	if len(overrideRB) < 2 {
		return
	}

	c.applicationDeleteWorkqueue.Add(fmt.Sprintf("%s*%s", key, overrideRB))
}

func (c *Controller) enqueueRolloutBlock(obj interface{}) {
	rolloutBlock, ok := obj.(*shipper.RolloutBlock)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.RolloutBlock: %#v", obj))
		return
	}

	key, err := cache.MetaNamespaceKeyFunc(rolloutBlock)
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
	defer c.releaseWorkqueue.ShutDown()
	defer c.releaseDeleteWorkqueue.ShutDown()
	defer c.applicationWorkqueue.ShutDown()
	defer c.applicationDeleteWorkqueue.ShutDown()
	defer c.rolloutblockWorkqueue.ShutDown()

	glog.V(2).Info("Starting RolloutBlock controller")
	defer glog.V(2).Info("Shutting down RolloutBlock controller")

	if ok := cache.WaitForCacheSync(
		stopCh,
		c.applicationsSynced,
		c.releasesSynced,
	); !ok {
		runtime.HandleError(fmt.Errorf("failed to wait for caches to sync"))
		return
	}

	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runRolloutBlockWorker, time.Second, stopCh)
		go wait.Until(c.runApplicationWorker, time.Second, stopCh)
		go wait.Until(c.runDeletedApplicationWorker, time.Second, stopCh)
		go wait.Until(c.runReleaseWorker, time.Second, stopCh)
		go wait.Until(c.runDeletedReleaseWorker, time.Second, stopCh)
	}

	glog.V(4).Info("Started RolloutBlock controller")

	<-stopCh
}

func (c *Controller) runRolloutBlockWorker() {
	for c.processNextRolloutBlockWorkItem() {
	}
}

func (c *Controller) runApplicationWorker() {
	for c.processNextAppWorkItem() {
	}
}

func (c *Controller) runDeletedApplicationWorker() {
	for c.processNextDeletedAppWorkItem() {
	}
}

func (c *Controller) runReleaseWorker() {
	for c.processNextRelWorkItem() {
	}
}

func (c *Controller) runDeletedReleaseWorker() {
	for c.processNextDeletedRelWorkItem() {
	}
}

// processNextRolloutBlockWorkItem pops an element from the head of the workqueue
// and passes to the sync rolloutblock handler. It returns bool indicating if the
// execution process should go on.
func (c *Controller) processNextRolloutBlockWorkItem() bool {
	// happens when DELETING a rolloutblock!
	obj, shutdown := c.rolloutblockWorkqueue.Get()
	if shutdown {
		return false
	}

	defer c.rolloutblockWorkqueue.Done(obj)

	var (
		key string
		ok  bool
	)

	if key, ok = obj.(string); !ok {
		c.rolloutblockWorkqueue.Forget(obj)
		runtime.HandleError(fmt.Errorf("invalid object key (will retry: false): %#v", obj))
		return true
	}

	//shouldRetry := false
	//err := c.syncRolloutBlock(key)
	//
	//if err != nil {
	//	shouldRetry = shippererrors.ShouldRetry(err)
	//	runtime.HandleError(fmt.Errorf("error syncing RolloutBlock %q (will retry: %t): %s", key, shouldRetry, err.Error()))
	//}
	//
	//if shouldRetry {
	//	if c.rolloutblockWorkqueue.NumRequeues(key) >= maxRetries {
	//		// Drop the RolloutBlock's key out of the workqueue and thus reset its
	//		// backoff. This limits the time a "broken" object can hog a worker.
	//		glog.Warningf("RolloutBlock %q has been retried too many times, dropping from the queue", key)
	//		c.rolloutblockWorkqueue.Forget(key)
	//
	//		return true
	//	}
	//
	//	c.rolloutblockWorkqueue.AddRateLimited(key)
	//
	//	return true
	//}

	glog.V(4).Infof("Successfully synced RolloutBlock %q", key)
	c.rolloutblockWorkqueue.Forget(obj)

	return true
}

//func (c *Controller) syncRolloutBlock(key string) error {
//	ns, name, err := cache.SplitMetaNamespaceKey(key)
//	if err != nil {
//		return shippererrors.NewUnrecoverableError(err)
//	}
//
//	rb, err := c.rolloutBlockLister.RolloutBlocks(ns).Get(name)
//	if err != nil {
//		if kerrors.IsNotFound(err) {
//			glog.V(3).Infof("RolloutBlock %q has been deleted", key)
//			return nil
//		}
//
//		return shippererrors.NewKubeclientGetError(ns, name, err).
//			WithShipperKind("RolloutBlock")
//	}
//
//	rb = rb.DeepCopy()
//
//	if err = c.processRolloutBlock(rb); err != nil {
//		if shippererrors.ShouldBroadcast(err) {
//			c.recorder.Event(rb,
//				corev1.EventTypeWarning,
//				"FailedRolloutBlock",
//				err.Error())
//		}
//		return err
//	}
//
//	_, err = c.shipperClientset.ShipperV1alpha1().RolloutBlocks(rb.Namespace).Update(rb)
//	if err != nil {
//		return shippererrors.NewKubeclientUpdateError(rb, err).
//			WithShipperKind("RolloutBlock")
//	}
//
//	return nil
//}

// find all applications and releases to add to .status.overrides and to worker queue
//func (c *Controller) processDeletedRolloutBlock(rbNamespace string, rbName string) error {
//	var (
//		releases     []*shipper.Release
//		applications []*shipper.Application
//
//		err error
//	)
//
//	if rbNamespace == shipper.ShipperNamespace {
//		if releases, err = c.releaseLister.List(labels.Everything()); err != nil {
//			return err
//		}
//
//		if applications, err = c.applicationLister.List(labels.Everything()); err != nil {
//			return err
//		}
//	} else {
//		if releases, err = c.releaseLister.Releases(rbNamespace).List(labels.Everything()); err != nil {
//			return err
//		}
//
//		if applications, err = c.applicationLister.Applications(rbNamespace).List(labels.Everything()); err != nil {
//			return err
//		}
//	}
//
//	// notify apps and rels controller??
//	return nil // c.updateRolloutBlockStatus(applications, rolloutBlock, releases)
//}

//func (c *Controller) updateRolloutBlockStatus(
//	applications []*shipper.Application,
//	rolloutBlock *shipper.RolloutBlock,
//	releases []*shipper.Release,
//) error {
//	for _, app := range applications {
//		c.addApplicationToRolloutBlockStatus(app, rolloutBlock)
//	}
//	for _, release := range releases {
//		c.addReleaseToRolloutBlockStatus(release, rolloutBlock)
//	}
//
//	return nil
//}
