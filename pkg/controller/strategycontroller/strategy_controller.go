package strategycontroller

import (
	"fmt"
	clientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	informers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	"github.com/bookingcom/shipper/pkg/client/listers/shipper/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"time"
)

type Controller struct {
	clientset                 *clientset.Clientset
	capacityTargetsLister     v1.CapacityTargetLister
	installationTargetsLister v1.InstallationTargetLister
	trafficTargetsLister      v1.TrafficTargetLister
	releasesLister            v1.ReleaseLister
	releasesSynced            cache.InformerSynced

	workqueue workqueue.RateLimitingInterface
}

func NewController(
	clientset *clientset.Clientset,
	informerFactory informers.SharedInformerFactory,
) *Controller {

	releaseInformer := informerFactory.Shipper().V1().Releases()

	controller := &Controller{
		clientset:                 clientset,
		capacityTargetsLister:     informerFactory.Shipper().V1().CapacityTargets().Lister(),
		installationTargetsLister: informerFactory.Shipper().V1().InstallationTargets().Lister(),
		trafficTargetsLister:      informerFactory.Shipper().V1().TrafficTargets().Lister(),
		releasesLister:            releaseInformer.Lister(),
		releasesSynced:            releaseInformer.Informer().HasSynced,
		workqueue:                 workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Releases"),
	}

	releaseInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueRelease,
		UpdateFunc: func(oldObj, newObj interface{}) {
			controller.enqueueRelease(newObj)
		},
	})

	return controller
}

func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	if ok := cache.WaitForCacheSync(stopCh, c.releasesSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	return nil
}

func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

func (c *Controller) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()

	if shutdown {
		return false
	}

	if key, ok := obj.(string); !ok {
		c.workqueue.Forget(obj)
		runtime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
		return false
	} else {
		if err := c.syncOne(key); err != nil {
			runtime.HandleError(fmt.Errorf("error syncing: %q: %s", key, err.Error()))
			return false
		} else {
			c.workqueue.Forget(obj)
			return true
		}
	}
}

func (c *Controller) syncOne(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	strategy, err := c.buildStrategy(namespace, name)
	if err != nil {
		return err
	}

	if err := strategy.execute(); err != nil {
		return err
	}

	_, err = c.clientset.ShipperV1().Releases(namespace).Update(strategy.release)
	if err != nil {
		return err
	}

	return nil
}

func (c *Controller) buildStrategy(ns string, name string) (*StrategyExecutor, error) {
	release, err := c.releasesLister.Releases(ns).Get(name)
	if err != nil {
		return nil, err
	}

	installationTarget, err := c.installationTargetsLister.InstallationTargets(ns).Get(name)
	if err != nil {
		return nil, err
	}

	capacityTarget, err := c.capacityTargetsLister.CapacityTargets(ns).Get(name)
	if err != nil {
		return nil, err
	}

	trafficTarget, err := c.trafficTargetsLister.TrafficTargets(ns).Get(name)
	if err != nil {
		return nil, err
	}

	return &StrategyExecutor{
		release:            release,
		installationTarget: installationTarget,
		trafficTarget:      trafficTarget,
		capacityTarget:     capacityTarget,
	}, nil
}

func (c *Controller) enqueueRelease(obj interface{}) {
	if key, err := cache.MetaNamespaceKeyFunc(obj); err != nil {
		runtime.HandleError(err)
	} else {
		c.workqueue.AddRateLimited(key)
	}
}
