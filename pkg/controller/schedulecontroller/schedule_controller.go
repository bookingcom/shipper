package schedulecontroller

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	"github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	clientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	informers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	listers "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
)

const AgentName = "schedule-controller"

//noinspection GoUnusedConst
const (
	SuccessSynced         = "Synced"
	MessageResourceSynced = "Release synced successfully"
)

// Controller is a Kubernetes controller that knows how to schedule Releases.
type Controller struct {
	kubeclientset    kubernetes.Interface
	shipperclientset clientset.Interface
	releasesLister   listers.ReleaseLister
	clustersLister   listers.ClusterLister
	releasesSynced   cache.InformerSynced
	workqueue        workqueue.RateLimitingInterface
	recorder         record.EventRecorder
}

// NewController returns a new Schedule controller.
func NewController(
	kubeclientset kubernetes.Interface,
	shipperclientset clientset.Interface,
	shipperInformerFactory informers.SharedInformerFactory,
	recorder record.EventRecorder,
) *Controller {

	releaseInformer := shipperInformerFactory.Shipper().V1().Releases()
	clusterInformer := shipperInformerFactory.Shipper().V1().Clusters()

	controller := &Controller{
		kubeclientset:    kubeclientset,
		shipperclientset: shipperclientset,
		releasesLister:   releaseInformer.Lister(),
		clustersLister:   clusterInformer.Lister(),
		releasesSynced:   releaseInformer.Informer().HasSynced,
		workqueue:        workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Releases"),
		recorder:         recorder,
	}

	glog.Info("Setting up event handlers")
	releaseInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				release := obj.(*v1.Release)
				return release.Status.Phase == v1.ReleasePhaseWaitingForScheduling
			},
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc: controller.enqueueRelease,
				UpdateFunc: func(oldObj, newObj interface{}) {
					controller.enqueueRelease(newObj)
				},
			},
		})

	return controller
}

func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	glog.V(2).Info("Starting Schedule controller")
	defer glog.V(2).Info("Shutting down Schedule controller")

	if ok := cache.WaitForCacheSync(stopCh, c.releasesSynced); !ok {
		runtime.HandleError(fmt.Errorf("failed to wait for caches to sync"))
		return
	}

	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	glog.V(4).Info("Started Schedule controller")

	<-stopCh
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

	err := func(obj interface{}) error {
		defer c.workqueue.Done(obj)
		var key string
		var ok bool

		if key, ok = obj.(string); !ok {
			c.workqueue.Forget(obj)
			runtime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}

		if err := c.syncOne(key); err != nil {
			return fmt.Errorf("error syncing: '%s': %s", key, err.Error())
		}

		c.workqueue.Forget(obj)
		glog.Infof("Successfully synced '%s'", key)
		return nil
	}(obj)

	if err != nil {
		runtime.HandleError(err)
		return true
	}

	return true
}

func (c *Controller) syncOne(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	release, err := c.releasesLister.Releases(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			runtime.HandleError(fmt.Errorf("release '%s' in work queue no longer exists", key))
			return nil
		}

		return err
	}

	scheduler := NewScheduler(
		release,
		c.shipperclientset,
		c.clustersLister,
	)

	err = scheduler.scheduleRelease()
	if err != nil {
		return err
	}

	c.recorder.Event(scheduler.Release, corev1.EventTypeNormal, SuccessSynced, MessageResourceSynced)
	return nil
}

func (c *Controller) enqueueRelease(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		runtime.HandleError(err)
		return
	}
	c.workqueue.AddRateLimited(key)
}
