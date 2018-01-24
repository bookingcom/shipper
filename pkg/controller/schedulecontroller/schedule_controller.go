package schedulecontroller

import (
	"fmt"
	"github.com/golang/glog"
	"github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	clientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shipperscheme "github.com/bookingcom/shipper/pkg/client/clientset/versioned/scheme"
	informers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	listers "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"time"
)

//noinspection GoUnusedConst
const (
	controllerAgentName   = "schedule-controller"
	SuccessSynced         = "Synced"
	MessageResourceSynced = "Release synced successfully"
	WaitingForStrategy    = "WaitingForStrategy" // TODO: Move to another package
	PhaseLabel            = "phase"
)

type Controller struct {
	kubeclientset    kubernetes.Interface
	shipperclientset clientset.Interface
	releasesLister   listers.ReleaseLister
	releasesSynced   cache.InformerSynced
	workqueue        workqueue.RateLimitingInterface
	recorder         record.EventRecorder
}

func NewController(
	kubeclientset kubernetes.Interface,
	shipperclientset clientset.Interface,
	shipperInformerFactory informers.SharedInformerFactory,
) *Controller {

	releaseInformer := shipperInformerFactory.Shipper().V1().Releases()

	shipperscheme.AddToScheme(scheme.Scheme)
	glog.V(4).Info("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})

	controller := &Controller{
		kubeclientset:    kubeclientset,
		shipperclientset: shipperclientset,
		releasesLister:   releaseInformer.Lister(),
		releasesSynced:   releaseInformer.Informer().HasSynced,
		workqueue:        workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Releases"),
		recorder:         recorder,
	}

	glog.Info("Setting up event handlers")
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

	glog.Info("Starting Schedule controller")

	glog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.releasesSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	glog.Info("Starting workers")
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

	err := func(obj interface{}) error {
		defer c.workqueue.Done(obj)
		var key string
		var ok bool

		if key, ok = obj.(string); !ok {
			c.workqueue.Forget(obj)
			runtime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}

		if err := c.syncHandler(key); err != nil {
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

func (c *Controller) syncHandler(key string) error {
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

	releaseCopy := release.DeepCopy()

	// Update releaseCopy with the computed target clusters
	releaseCopy.Environment.Clusters = targetClusters(release.Environment.ShipmentOrder.ClusterSelectors)

	// Update phase label to "WaitingForStrategy"
	releaseCopy.Labels[PhaseLabel] = WaitingForStrategy

	// Store releaseCopy
	_, err = c.shipperclientset.ShipperV1().Releases(releaseCopy.Namespace).Update(releaseCopy)
	if err != nil {
		return err
	}

	c.recorder.Event(releaseCopy, corev1.EventTypeNormal, SuccessSynced, MessageResourceSynced)
	return nil
}

//noinspection GoUnusedParameter
func targetClusters(clusterSelectors []v1.ClusterSelector) []string {
	return []string{
		"eu-ams-a",
		"eu-ams-b",
	}
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
