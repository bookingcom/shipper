package release

import (
	"fmt"
	"time"

	"github.com/golang/glog"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/chart"
	shipperclient "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shipperlisters "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1alpha1"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

const (
	AgentName = "release-controller"

	maxRetries = 11
)

type Controller struct {
	clientset      shipperclient.Interface
	chartFetchFunc chart.FetchFunc
	recorder       record.EventRecorder

	releaseLister  shipperlisters.ReleaseLister
	releasesSynced cache.InformerSynced

	clusterLister  shipperlisters.ClusterLister
	clustersSynced cache.InformerSynced

	installationTargetLister  shipperlisters.InstallationTargetLister
	installationTargetsSynced cache.InformerSynced

	trafficTargetLister  shipperlisters.TrafficTargetLister
	trafficTargetsSynced cache.InformerSynced

	capacityTargetLister  shipperlisters.CapacityTargetLister
	capacityTargetsSynced cache.InformerSynced

	workqueue workqueue.RateLimitingInterface
}

func NewController(
	clientset shipperclient.Interface,
	informerFactory shipperinformers.SharedInformerFactory,
	chartFetchFunc chart.FetchFunc,
	recorder record.EventRecorder,
) *Controller {

	releaseInformer := informerFactory.Shipper().V1alpha1().Releases()
	clusterInformer := informerFactory.Shipper().V1alpha1().Clusters()
	installationTargetInformer := informerFactory.Shipper().V1alpha1().InstallationTargets()
	trafficTargetInformer := informerFactory.Shipper().V1alpha1().TrafficTargets()
	capacityTargetInformer := informerFactory.Shipper().V1alpha1().CapacityTargets()

	glog.Info("Building a release controller")

	controller := &Controller{
		clientset:      clientset,
		chartFetchFunc: chartFetchFunc,
		recorder:       recorder,

		releaseLister:  releaseInformer.Lister(),
		releasesSynced: releaseInformer.Informer().HasSynced,

		clusterLister:  clusterInformer.Lister(),
		clustersSynced: clusterInformer.Informer().HasSynced,

		installationTargetLister:  installationTargetInformer.Lister(),
		installationTargetsSynced: installationTargetInformer.Informer().HasSynced,

		trafficTargetLister:  trafficTargetInformer.Lister(),
		trafficTargetsSynced: trafficTargetInformer.Informer().HasSynced,

		capacityTargetLister:  capacityTargetInformer.Lister(),
		capacityTargetsSynced: capacityTargetInformer.Informer().HasSynced,

		workqueue: workqueue.NewNamedRateLimitingQueue(
			workqueue.DefaultControllerRateLimiter(),
			"release_controller_workqueue",
		),
	}

	glog.Info("Setting up event handlers")

	releaseInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: controller.enqueueRelease,
			UpdateFunc: func(oldObj, newObj interface{}) {
				controller.enqueueRelease(newObj)
			},
		})

	return controller
}

func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	glog.V(2).Info("Starting Release controller")
	defer glog.V(2).Info("Shutting down Release controller")

	if ok := cache.WaitForCacheSync(
		stopCh,
		c.releasesSynced,
		c.clustersSynced,
		c.installationTargetsSynced,
		c.trafficTargetsSynced,
		c.capacityTargetsSynced,
	); !ok {
		runtime.HandleError(fmt.Errorf("Failed to wait for caches to sync"))
		return
	}

	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	glog.V(4).Info("Started Release controller")

	<-stopCh
}

func (c *Controller) runWorker() {
	for c.processNextRelease() {
	}
}

func (c *Controller) processNextRelease() bool {
	obj, shutdown := c.workqueue.Get()
	if shutdown {
		return false
	}

	defer c.workqueue.Done(obj)

	if _, ok := obj.(string); !ok {
		c.workqueue.Forget(obj)
		runtime.HandleError(fmt.Errorf("Invalid object key (will not retry): %#v", obj))
	}
	key := obj.(string)

	if shouldRetry := c.syncHandler(key); shouldRetry {
		if c.workqueue.NumRequeues(key) >= maxRetries {
			glog.Warningf("Release %q has been retried too many times, droppping from the queue", key)
			c.workqueue.Forget(key)

			return true
		}

		c.workqueue.AddRateLimited(key)

		return true
	}

	c.workqueue.Forget(obj)
	glog.V(4).Infof("Successfully synced release %q", key)

	return true
}

func (c *Controller) syncHandler(key string) bool {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("Invalid object key (will not retry): %q", key))
		return false
	}
	rel, err := c.releaseLister.Releases(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			glog.V(3).Infof("Release %q has been deleted", key)
			return false
		}

		runtime.HandleError(fmt.Errorf("Failed to process release %q (will retry): %s", key, err))

		return true
	}

	if !releaseutil.ReleaseScheduled(rel) {

		glog.V(4).Infof("Release %q is not scheduled yet, processing", key)

		scheduler := NewScheduler(
			c.clientset,
			c.clusterLister,
			c.installationTargetLister,
			c.trafficTargetLister,
			c.capacityTargetLister,
			c.chartFetchFunc,
			c.recorder,
		)

		_, err = scheduler.ScheduleRelease(rel)
		if err != nil {
			c.recorder.Eventf(
				rel,
				corev1.EventTypeWarning,
				"FailedReleaseScheduling",
				err.Error(),
			)

			reason, shouldRetry := classifyError(err)
			condition := releaseutil.NewReleaseCondition(
				shipper.ReleaseConditionTypeScheduled,
				corev1.ConditionFalse,
				reason,
				err.Error(),
			)
			releaseutil.SetReleaseCondition(&rel.Status, *condition)

			if _, err := c.clientset.ShipperV1alpha1().Releases(namespace).Update(rel); err != nil {
				// always retry failing to write the error out to the Release: we need to communicate this to the user
				return true
			}

			if shouldRetry {
				runtime.HandleError(fmt.Errorf("Error syncing Release %q (will retry): %s", key, err))
				return true
			}

			runtime.HandleError(fmt.Errorf("Error syncing Release %q (will not retry): %s", key, err))

			return false
		}
		glog.V(4).Infof("Release %q has been successfully scheduled", key)
	}

	return false
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

	c.workqueue.Add(key)
}
