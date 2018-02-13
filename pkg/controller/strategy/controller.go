package strategy

import (
	"fmt"
	"github.com/golang/glog"
	clientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	informers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	listers "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"time"
)

type Controller struct {
	clientset                 *clientset.Clientset
	capacityTargetsLister     listers.CapacityTargetLister
	installationTargetsLister listers.InstallationTargetLister
	trafficTargetsLister      listers.TrafficTargetLister
	releasesLister            listers.ReleaseLister
	releasesSynced            cache.InformerSynced
	dynamicClientPool         dynamic.ClientPool
	workqueue                 workqueue.RateLimitingInterface
}

func NewController(
	clientset *clientset.Clientset,
	informerFactory informers.SharedInformerFactory,
	restConfig *rest.Config,
) *Controller {

	dynamicClientPool := dynamic.NewDynamicClientPool(restConfig)
	releaseInformer := informerFactory.Shipper().V1().Releases()
	capacityTargetInformer := informerFactory.Shipper().V1().CapacityTargets()
	installationTargetInformer := informerFactory.Shipper().V1().InstallationTargets()
	trafficTargetInformer := informerFactory.Shipper().V1().TrafficTargets()

	controller := &Controller{
		clientset:                 clientset,
		capacityTargetsLister:     informerFactory.Shipper().V1().CapacityTargets().Lister(),
		installationTargetsLister: informerFactory.Shipper().V1().InstallationTargets().Lister(),
		trafficTargetsLister:      informerFactory.Shipper().V1().TrafficTargets().Lister(),
		releasesLister:            releaseInformer.Lister(),
		releasesSynced:            releaseInformer.Informer().HasSynced,
		workqueue:                 workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Releases"),
		dynamicClientPool:         dynamicClientPool,
	}

	releaseInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueRelease,
		UpdateFunc: func(oldObj, newObj interface{}) {
			controller.enqueueRelease(newObj)
		},
	})

	installationTargetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueRelease,
		UpdateFunc: func(oldObj, newObj interface{}) {
			controller.enqueueRelease(newObj)
		},
	})

	capacityTargetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueRelease,
		UpdateFunc: func(oldObj, newObj interface{}) {
			controller.enqueueRelease(newObj)
		},
	})

	trafficTargetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
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

	glog.Info("Started workers")
	<-stopCh
	glog.Info("Shutting down workers")

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
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	strategy, err := c.buildStrategy(ns, name)
	if err != nil {
		return err
	}

	if result, err := strategy.execute(); err != nil {
		return err
	} else if len(result) > 0 {

		for _, e := range result {

			r := e.(ExecutorResult)

			// XXX: This is work in progress. result implements the ExecutorResult
			// interface, and if it is not nil then a patch is required, using the
			// information from the returned gvk, together with the []byte that
			// represents the patch encoded in JSON.
			name, gvk, b := r.Patch()

			if client, err := c.clientForGroupVersionKind(gvk, ns); err != nil {
				return err
			} else if _, err = client.Patch(name, types.MergePatchType, b); err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *Controller) clientForGroupVersionKind(
	gvk schema.GroupVersionKind,
	ns string,
) (dynamic.ResourceInterface, error) {
	client, err := c.dynamicClientPool.ClientForGroupVersionKind(gvk)
	if err != nil {
		return nil, err
	}

	// This is sort of stupid, it might exist some better way to get the APIResource here...
	var resource *metav1.APIResource
	gv := gvk.GroupVersion().String()
	if resources, err := c.clientset.Discovery().ServerResourcesForGroupVersion(gv); err != nil {
		return nil, err
	} else {
		for _, r := range resources.APIResources {
			if r.Kind == gvk.Kind {
				resource = &r
				break
			}
		}
	}

	if resource == nil {
		return nil, fmt.Errorf("could not find the specified resource")
	}

	return client.Resource(resource, ns), nil
}

func (c *Controller) buildStrategy(ns string, name string) (*Executor, error) {
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

	return &Executor{
		contenderRelease: &releaseInfo{
			release:            release,
			installationTarget: installationTarget,
			trafficTarget:      trafficTarget,
			capacityTarget:     capacityTarget,
		},
	}, nil
}

func (c *Controller) enqueueRelease(obj interface{}) {
	if key, err := cache.MetaNamespaceKeyFunc(obj); err != nil {
		runtime.HandleError(err)
	} else {
		c.workqueue.AddRateLimited(key)
	}
}
