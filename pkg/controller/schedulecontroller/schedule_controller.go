package schedulecontroller

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

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	"github.com/bookingcom/shipper/pkg/chart"
	shipper "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shipperlisters "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

const (
	AgentName = "schedule-controller"

	// maxRetries is the number of times a Release will be retried
	// before we drop it out of the workqueue. The number is chosen with the
	// default rate limiter in mind. This results in the following backoff times:
	// 5ms, 10ms, 20ms, 40ms, 80ms, 160ms, 320ms, 640ms, 1.3s, 2.6s, 5.1s, 10.2s.
	maxRetries = 11
)

// Controller is a Kubernetes controller that knows how to schedule Releases.
type Controller struct {
	shipperclientset shipper.Interface

	releasesLister shipperlisters.ReleaseLister
	releasesSynced cache.InformerSynced

	clustersLister shipperlisters.ClusterLister
	clustersSynced cache.InformerSynced

	installationTargerLister shipperlisters.InstallationTargetLister
	capacityTargetLister     shipperlisters.CapacityTargetLister
	trafficTargetLister      shipperlisters.TrafficTargetLister

	workqueue      workqueue.RateLimitingInterface
	chartFetchFunc chart.FetchFunc
	recorder       record.EventRecorder
}

// NewController returns a new Schedule controller.
func NewController(
	shipperclientset shipper.Interface,
	shipperInformerFactory shipperinformers.SharedInformerFactory,
	chartFetchFunc chart.FetchFunc,
	recorder record.EventRecorder,
) *Controller {

	releaseInformer := shipperInformerFactory.Shipper().V1().Releases()
	clusterInformer := shipperInformerFactory.Shipper().V1().Clusters()

	installationTargetLister := shipperInformerFactory.Shipper().V1().InstallationTargets().Lister()
	capacityTargetLister := shipperInformerFactory.Shipper().V1().CapacityTargets().Lister()
	trafficTargetLister := shipperInformerFactory.Shipper().V1().TrafficTargets().Lister()

	controller := &Controller{
		shipperclientset: shipperclientset,

		releasesLister: releaseInformer.Lister(),
		releasesSynced: releaseInformer.Informer().HasSynced,

		clustersLister: clusterInformer.Lister(),
		clustersSynced: clusterInformer.Informer().HasSynced,

		installationTargerLister: installationTargetLister,
		capacityTargetLister:     capacityTargetLister,
		trafficTargetLister:      trafficTargetLister,

		workqueue:      workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "schedule_controller_releases"),
		chartFetchFunc: chartFetchFunc,
		recorder:       recorder,
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

	glog.V(2).Info("Starting Schedule controller")
	defer glog.V(2).Info("Shutting down Schedule controller")

	if ok := cache.WaitForCacheSync(stopCh, c.releasesSynced, c.clustersSynced); !ok {
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

	defer c.workqueue.Done(obj)

	var (
		key string
		ok  bool
	)

	if key, ok = obj.(string); !ok {
		c.workqueue.Forget(obj)
		runtime.HandleError(fmt.Errorf("invalid object key (will not retry): %#v", obj))
		return true
	}

	if shouldRetry := c.syncOne(key); shouldRetry {
		if c.workqueue.NumRequeues(key) >= maxRetries {
			// Drop the Release's key out of the workqueue and thus reset its backoff.
			// This limits the time a "broken" object can hog a worker.
			glog.Warningf("Release %q has been retried too many times, dropping from the queue", key)
			c.workqueue.Forget(key)

			return true
		}

		c.workqueue.AddRateLimited(key)

		return true
	}

	c.workqueue.Forget(obj)
	glog.V(4).Infof("Successfully synced Release %q", key)

	return true
}

func (c *Controller) syncOne(key string) bool {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid object key (will not retry): %#v", key))
		return false
	}

	release, err := c.releasesLister.Releases(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			glog.V(3).Infof("Release %q has been deleted", key)
			return false
		}

		runtime.HandleError(fmt.Errorf("error syncing Release %q (will retry): %s", key, err))
		return true
	}

	if releaseutil.ReleaseScheduled(release) {
		glog.V(4).Info("Release %q has already been scheduled, ignoring", key)
		return false
	}

	scheduler := NewScheduler(
		release,
		c.shipperclientset,
		c.clustersLister,
		c.installationTargerLister,
		c.capacityTargetLister,
		c.trafficTargetLister,
		c.chartFetchFunc,
		c.recorder,
	)

	if err := scheduler.scheduleRelease(); err != nil {
		c.recorder.Eventf(
			scheduler.Release,
			corev1.EventTypeWarning,
			"FailedReleaseScheduling",
			err.Error(),
		)

		reason, shouldRetry := classifyError(err)
		condition := releaseutil.NewReleaseCondition(
			shipperv1.ReleaseConditionTypeScheduled,
			corev1.ConditionFalse,
			reason,
			err.Error(),
		)

		releaseutil.SetReleaseCondition(&release.Status, *condition)

		if _, err := c.shipperclientset.ShipperV1().Releases(namespace).Update(release); err != nil {
			runtime.HandleError(fmt.Errorf("error updating Release %q with condition (will retry): %s", key, err))
			// always retry failing to write the error out to the Release: we need to communicate this to the user
			return true
		}

		if shouldRetry {
			runtime.HandleError(fmt.Errorf("error syncing Release %q (will retry): %s", key, err))
			return true
		}

		runtime.HandleError(fmt.Errorf("error syncing Release %q (will NOT retry): %s", key, err))
		return false
	}

	return false
}

func (c *Controller) enqueueRelease(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	c.workqueue.Add(key)
}
