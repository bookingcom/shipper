package release

import (
	"fmt"
	"time"

	"github.com/golang/glog"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperrepo "github.com/bookingcom/shipper/pkg/chart/repo"
	shipperclient "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shipperlisters "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

const (
	AgentName = "release-controller"

	maxRetries = 11
)

// Controller is a Kubernetes controller whose role is to pick up a newly created
// release and progress it forward by scheduling the release on a set of
// selected clusters, creating a set of associated objects and executing the
// strategy.
//
// Release Controller has 2 primary workqueues: releases and applications.
type Controller struct {
	clientset shipperclient.Interface

	applicationLister  shipperlisters.ApplicationLister
	applicationsSynced cache.InformerSynced

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

	rolloutBlockLister shipperlisters.RolloutBlockLister
	rolloutBlockSynced cache.InformerSynced

	releaseWorkqueue     workqueue.RateLimitingInterface
	applicationWorkqueue workqueue.RateLimitingInterface

	chartFetcher shipperrepo.ChartFetcher

	recorder record.EventRecorder
}

type releaseInfo struct {
	release            *shipper.Release
	installationTarget *shipper.InstallationTarget
	trafficTarget      *shipper.TrafficTarget
	capacityTarget     *shipper.CapacityTarget
}

type ReleaseStrategyStateTransition struct {
	State    string
	Previous shipper.StrategyState
	New      shipper.StrategyState
}

func NewController(
	clientset shipperclient.Interface,
	informerFactory shipperinformers.SharedInformerFactory,
	chartFetcher shipperrepo.ChartFetcher,
	recorder record.EventRecorder,
) *Controller {

	applicationInformer := informerFactory.Shipper().V1alpha1().Applications()
	releaseInformer := informerFactory.Shipper().V1alpha1().Releases()
	clusterInformer := informerFactory.Shipper().V1alpha1().Clusters()
	installationTargetInformer := informerFactory.Shipper().V1alpha1().InstallationTargets()
	trafficTargetInformer := informerFactory.Shipper().V1alpha1().TrafficTargets()
	capacityTargetInformer := informerFactory.Shipper().V1alpha1().CapacityTargets()
	rolloutBlockInformer := informerFactory.Shipper().V1alpha1().RolloutBlocks()

	glog.Info("Building a release controller")

	controller := &Controller{
		clientset: clientset,

		applicationLister:  applicationInformer.Lister(),
		applicationsSynced: applicationInformer.Informer().HasSynced,

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

		rolloutBlockLister: rolloutBlockInformer.Lister(),
		rolloutBlockSynced: rolloutBlockInformer.Informer().HasSynced,

		releaseWorkqueue: workqueue.NewNamedRateLimitingQueue(
			workqueue.DefaultControllerRateLimiter(),
			"release_controller_releases",
		),
		applicationWorkqueue: workqueue.NewNamedRateLimitingQueue(
			workqueue.DefaultControllerRateLimiter(),
			"release_controller_applications",
		),

		chartFetcher: chartFetcher,

		recorder: recorder,
	}

	glog.Info("Setting up event handlers")

	releaseInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: controller.enqueueRelease,
			UpdateFunc: func(oldObj, newObj interface{}) {
				controller.enqueueReleaseRateLimited(newObj)
			},
			DeleteFunc: controller.enqueueAppFromRelease,
		})

	installationTargetInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: controller.enqueueInstallationTarget,
			UpdateFunc: func(oldObj, newObj interface{}) {
				controller.enqueueInstallationTarget(newObj)
			},
			DeleteFunc: controller.enqueueInstallationTarget,
		})

	capacityTargetInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: controller.enqueueCapacityTarget,
			UpdateFunc: func(oldObj, newObj interface{}) {
				controller.enqueueCapacityTarget(newObj)
			},
			DeleteFunc: controller.enqueueCapacityTarget,
		})

	trafficTargetInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: controller.enqueueTrafficTarget,
			UpdateFunc: func(oldObj, newObj interface{}) {
				controller.enqueueTrafficTarget(newObj)
			},
			DeleteFunc: controller.enqueueTrafficTarget,
		})

	return controller
}

// Run starts Release Controller workers and waits until stopCh is closed.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) {
	defer runtime.HandleCrash()
	defer c.releaseWorkqueue.ShutDown()
	defer c.applicationWorkqueue.ShutDown()

	glog.V(2).Info("Starting Release controller")
	defer glog.V(2).Info("Shutting down Release controller")

	if ok := cache.WaitForCacheSync(
		stopCh,
		c.applicationsSynced,
		c.releasesSynced,
		c.clustersSynced,
		c.installationTargetsSynced,
		c.trafficTargetsSynced,
		c.capacityTargetsSynced,
		c.rolloutBlockSynced,
	); !ok {
		runtime.HandleError(fmt.Errorf("failed to wait for caches to sync"))
		return
	}

	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runReleaseWorker, time.Second, stopCh)
		go wait.Until(c.runApplicationWorker, time.Second, stopCh)
	}

	glog.V(4).Info("Started Release controller")

	<-stopCh
}

func (c *Controller) runReleaseWorker() {
	for c.processNextReleaseWorkItem() {
	}
}

func (c *Controller) runApplicationWorker() {
	for c.processNextAppWorkItem() {
	}
}

// processNextReleaseWorkItem pops an element from the head of the workqueue and
// passes to the sync release handler. It returns bool indicating if the
// execution process should go on.
func (c *Controller) processNextReleaseWorkItem() bool {
	obj, shutdown := c.releaseWorkqueue.Get()
	if shutdown {
		return false
	}

	defer c.releaseWorkqueue.Done(obj)

	var (
		key string
		ok  bool
	)

	if key, ok = obj.(string); !ok {
		c.releaseWorkqueue.Forget(obj)
		runtime.HandleError(fmt.Errorf("invalid object key (will retry: false): %#v", obj))
		return true
	}

	shouldRetry := false
	err := c.syncOneReleaseHandler(key)

	if err != nil {
		shouldRetry = shippererrors.ShouldRetry(err)
		runtime.HandleError(fmt.Errorf("error syncing Release %q (will retry: %t): %s", key, shouldRetry, err.Error()))
	}

	if shouldRetry {
		if c.releaseWorkqueue.NumRequeues(key) >= maxRetries {
			glog.Warningf("Release %q has been retried too many times, droppping from the queue", key)
			c.releaseWorkqueue.Forget(key)
			return true
		}

		c.releaseWorkqueue.AddRateLimited(key)

		return true
	}

	glog.V(4).Infof("Successfully synced Release %q", key)
	c.releaseWorkqueue.Forget(obj)

	return true
}

// syncOneReleaseHandler processes release keys one-by-one. This stage progresses
// the release through a scheduler: assigns a set of chosen clusters, creates
// required associated objects and marks the release as scheduled.
func (c *Controller) syncOneReleaseHandler(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return shippererrors.NewUnrecoverableError(err)
	}

	rel, err := c.releaseLister.Releases(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			glog.V(3).Infof("Release %q not found", key)
			return nil
		}

		return shippererrors.NewKubeclientGetError(namespace, name, err).
			WithShipperKind("Release")
	}

	if releaseutil.HasEmptyEnvironment(rel) {
		return nil
	}

	glog.V(4).Infof("Start processing Release %q", key)

	scheduler := NewScheduler(
		c.clientset,
		c.clusterLister,
		c.installationTargetLister,
		c.capacityTargetLister,
		c.trafficTargetLister,
		c.rolloutBlockLister,
		c.chartFetcher,
		c.recorder,
	)

	// This is a 2-round handler: the 1st round schedules the release on a
	// set of clusters, and the 2nd round creates associated objects and
	// finalizes release scheduling process.
	if !releaseHasClusters(rel) {
		if _, err := scheduler.ChooseClusters(rel.DeepCopy(), false); err != nil {
			return shippererrors.NewRecoverableError(fmt.Errorf("failed to choose clusters for release %q (will retry): %s", key, err))
		}

		// If all went fine, we return here and let informers pick up
		// the change and reschedule this release for the 2nd round
		// naturally. This approach comes for a reason: we are
		// eliminating a replication lag problem. While running the
		// system in production, we observed some cases where a few
		// sequentual updates on the same object experienced apiserver
		// rejections due to the passed object outdate state. This
		// happened due to the synchronisation/replication lag in etcd.
		// This approach helps to eliminate this as a notification will
		// be delivered once all parties are in sync.
		return nil
	}

	if _, err = scheduler.ScheduleRelease(rel.DeepCopy()); err != nil {
		if shippererrors.ShouldBroadcast(err) {
			c.recorder.Eventf(
				rel,
				corev1.EventTypeWarning,
				"FailedReleaseScheduling",
				err.Error(),
			)
		}

		reason := reasonForReleaseCondition(err)
		condition := releaseutil.NewReleaseCondition(
			shipper.ReleaseConditionTypeScheduled,
			corev1.ConditionFalse,
			reason,
			err.Error(),
		)
		releaseutil.SetReleaseCondition(&rel.Status, *condition)

		if _, err := c.clientset.ShipperV1alpha1().Releases(namespace).Update(rel); err != nil {
			return shippererrors.NewKubeclientUpdateError(rel, err)
		}

		return err
	}

	glog.V(4).Infof("Release %q has been successfully scheduled", key)

	appKey, err := c.getAssociatedApplicationKey(rel)
	if err != nil {
		return err
	}

	// If everything went fine, scheduling an application key in the
	// application workqueue.
	glog.V(4).Infof("Scheduling Application key %q", appKey)
	c.applicationWorkqueue.Add(appKey)

	glog.V(4).Infof("Done processing Release %q", key)

	return nil
}

// getAssociatedApplicationKey returns an application key in the format:
// <namespace>/<application name>
func (c *Controller) getAssociatedApplicationKey(rel *shipper.Release) (string, error) {
	appName, err := releaseutil.ApplicationNameForRelease(rel)
	if err != nil {
		return "", err
	}

	appKey := fmt.Sprintf("%s/%s", rel.Namespace, appName)

	return appKey, nil
}

// getAssociatedReleaseKey returns an owner reference release name for an
// associated object in the format:
// <namespace> / <release name>
func (c *Controller) getAssociatedReleaseKey(obj *metav1.ObjectMeta) (string, error) {
	if n := len(obj.OwnerReferences); n != 1 {
		return "", shippererrors.NewMultipleOwnerReferencesError(obj.Name, n)
	}

	owner := obj.OwnerReferences[0]

	return fmt.Sprintf("%s/%s", obj.Namespace, owner.Name), nil
}

// buildReleaseInfo returns a release and it's associated objects fetched from
// the lister interface. If some of them could not be found, it returns a
// corresponding error.
func (c *Controller) buildReleaseInfo(rel *shipper.Release) (*releaseInfo, error) {
	ns := rel.Namespace
	name := rel.Name

	installationTarget, err := c.installationTargetLister.InstallationTargets(ns).Get(name)
	if err != nil {
		return nil, shippererrors.NewKubeclientGetError(ns, name, err).
			WithShipperKind("InstallationTarget")
	}

	capacityTarget, err := c.capacityTargetLister.CapacityTargets(ns).Get(name)
	if err != nil {
		return nil, shippererrors.NewKubeclientGetError(ns, name, err).
			WithShipperKind("CapacityTarget")
	}

	trafficTarget, err := c.trafficTargetLister.TrafficTargets(ns).Get(name)
	if err != nil {
		return nil, shippererrors.NewKubeclientGetError(ns, name, err).
			WithShipperKind("TrafficTarget")
	}

	return &releaseInfo{
		release:            rel,
		installationTarget: installationTarget,
		trafficTarget:      trafficTarget,
		capacityTarget:     capacityTarget,
	}, nil
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

func (c *Controller) enqueueReleaseRateLimited(obj interface{}) {
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

	c.releaseWorkqueue.AddRateLimited(key)
}

func (c *Controller) enqueueAppFromRelease(obj interface{}) {
	rel, ok := obj.(*shipper.Release)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.Release: %#v", obj))
		return
	}

	appName, err := c.getAssociatedApplicationKey(rel)
	if err != nil {
		runtime.HandleError(fmt.Errorf("error fetching Application key for release %v: %s", rel, err))
		return
	}

	c.applicationWorkqueue.Add(appName)
}

func (c *Controller) enqueueInstallationTarget(obj interface{}) {
	it, ok := obj.(*shipper.InstallationTarget)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.InstallationTarget: %#v", obj))
		return
	}

	releaseKey, err := c.getAssociatedReleaseKey(&it.ObjectMeta)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	c.releaseWorkqueue.Add(releaseKey)
}

func (c *Controller) enqueueCapacityTarget(obj interface{}) {
	ct, ok := obj.(*shipper.CapacityTarget)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.CapacityTarget: %#v", obj))
		return
	}

	releaseKey, err := c.getAssociatedReleaseKey(&ct.ObjectMeta)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	c.releaseWorkqueue.Add(releaseKey)
}

func (c *Controller) enqueueTrafficTarget(obj interface{}) {
	tt, ok := obj.(*shipper.TrafficTarget)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.TrafficTarget: %#v", obj))
		return
	}

	releaseKey, err := c.getAssociatedReleaseKey(&tt.ObjectMeta)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	c.releaseWorkqueue.Add(releaseKey)
}

func reasonForReleaseCondition(err error) string {
	switch err.(type) {
	case shippererrors.NoRegionsSpecifiedError:
		return "NoRegionsSpecified"
	case shippererrors.NotEnoughClustersInRegionError:
		return "NotEnoughClustersInRegion"
	case shippererrors.NotEnoughCapableClustersInRegionError:
		return "NotEnoughCapableClustersInRegion"

	case shippererrors.DuplicateCapabilityRequirementError:
		return "DuplicateCapabilityRequirement"

	case shippererrors.ChartFetchFailureError:
		return "ChartFetchFailure"
	case shippererrors.BrokenChartSpecError:
		return "BrokenChartSpec"
	case shippererrors.WrongChartDeploymentsError:
		return "WrongChartDeployments"
	case shippererrors.InvalidRolloutBlockOverrideError:
		return "InvalidRolloutBlockOverride"
	case shippererrors.RolloutBlockError:
		return "RolloutBlock"
	}

	if shippererrors.IsKubeclientError(err) {
		return "FailedAPICall"
	}

	return "unknown error! tell Shipper devs to classify it"
}
