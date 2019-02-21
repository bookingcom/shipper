package release

import (
	"fmt"
	"time"

	"github.com/golang/glog"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
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
	shippercontroller "github.com/bookingcom/shipper/pkg/controller"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

const (
	AgentName = "release-controller"

	maxRetries = 11
)

const (
	retry   = true
	noRetry = false
)

type Controller struct {
	clientset      shipperclient.Interface
	chartFetchFunc chart.FetchFunc
	recorder       record.EventRecorder

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

	releaseWorkqueue     workqueue.RateLimitingInterface
	applicationWorkqueue workqueue.RateLimitingInterface
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
	chartFetchFunc chart.FetchFunc,
	recorder record.EventRecorder,
) *Controller {

	applicationInformer := informerFactory.Shipper().V1alpha1().Applications()
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

		releaseWorkqueue: workqueue.NewNamedRateLimitingQueue(
			workqueue.DefaultControllerRateLimiter(),
			"release_controller_releases",
		),
		applicationWorkqueue: workqueue.NewNamedRateLimitingQueue(
			workqueue.DefaultControllerRateLimiter(),
			"release_controller_applications",
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

	installationTargetInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: controller.enqueueInstallationTarget,
			UpdateFunc: func(oldObj, newObj interface{}) {
				controller.enqueueInstallationTarget(newObj)
			},
		})

	capacityTargetInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: controller.enqueueCapacityTarget,
			UpdateFunc: func(oldObj, newObj interface{}) {
				controller.enqueueCapacityTarget(newObj)
			},
		})

	trafficTargetInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: controller.enqueueTrafficTarget,
			UpdateFunc: func(oldObj, newObj interface{}) {
				controller.enqueueTrafficTarget(newObj)
			},
		})

	return controller
}

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

func (c *Controller) processNextReleaseWorkItem() bool {
	obj, shutdown := c.releaseWorkqueue.Get()
	if shutdown {
		return false
	}

	defer c.releaseWorkqueue.Done(obj)

	if _, ok := obj.(string); !ok {
		c.releaseWorkqueue.Forget(obj)
		runtime.HandleError(fmt.Errorf("invalid object key (will not retry): %#v", obj))
	}
	key := obj.(string)

	if shouldRetry := c.syncReleaseHandler(key); shouldRetry {
		if c.releaseWorkqueue.NumRequeues(key) >= maxRetries {
			glog.Warningf("Release %q has been retried too many times, droppping from the queue", key)
			c.releaseWorkqueue.Forget(key)

			return true
		}

		c.releaseWorkqueue.AddRateLimited(key)

		return true
	}

	c.releaseWorkqueue.Forget(obj)
	glog.V(4).Infof("Successfully synced Release %q", key)

	return true
}

func (c *Controller) processNextAppWorkItem() bool {
	obj, shutdown := c.applicationWorkqueue.Get()
	if shutdown {
		return false
	}
	defer c.applicationWorkqueue.Done(obj)

	if _, ok := obj.(string); !ok {
		c.applicationWorkqueue.Forget(obj)
		runtime.HandleError(fmt.Errorf("invalid object key (will not retry): %#v", obj))
	}
	key := obj.(string)

	if shouldRetry := c.syncApplicationHandler(key); shouldRetry {
		if c.applicationWorkqueue.NumRequeues(key) >= maxRetries {
			glog.Warningf("Application %q has been retried too many times, droppping from the queue", key)
			c.applicationWorkqueue.Forget(key)

			return true
		}

		c.applicationWorkqueue.AddRateLimited(key)

		return true
	}

	c.applicationWorkqueue.Forget(obj)
	glog.V(4).Infof("Successfully synced Application %q", key)

	return true
}

func (c *Controller) syncReleaseHandler(key string) bool {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid object key (will not retry): %q", key))
		return noRetry
	}

	rel, err := c.releaseLister.Releases(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			glog.V(3).Infof("Release %q has been deleted", key)
			return noRetry
		}

		runtime.HandleError(fmt.Errorf("failed to process release %q (will retry): %s", key, err))

		return retry
	}

	if releaseutil.IsEmpty(rel) {
		glog.Infof("Release %q has an empty Environment, bailing out", key)
		return noRetry
	}

	if isHead, err := c.releaseIsHead(rel); err != nil {
		runtime.HandleError(fmt.Errorf("Failed to check if release %q is the head of history (will retry): %s", key, err))
	} else if !isHead {
		glog.Infof("Release %q is not the head of history, nothing to do", key)
		return noRetry
	}

	isScheduled, err := c.releaseScheduled(rel)
	if err != nil {
		runtime.HandleError(fmt.Errorf("failed to check if release %q has been scheduled (will retry): %s", key, err))
		return retry
	}

	if !isScheduled {

		glog.V(4).Infof("Release %q is not scheduled yet, processing", key)

		scheduler := NewScheduler(
			c.clientset,
			c.clusterLister,
			c.installationTargetLister,
			c.capacityTargetLister,
			c.trafficTargetLister,
			c.chartFetchFunc,
			c.recorder,
		)

		if _, err = scheduler.ScheduleRelease(rel.DeepCopy()); err != nil {
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
				return retry
			}

			if shouldRetry {
				runtime.HandleError(fmt.Errorf("error syncing Release %q (will retry): %s", key, err))
				return retry
			}

			runtime.HandleError(fmt.Errorf("error syncing Release %q (will not retry): %s", key, err))

			return noRetry
		}

		glog.V(4).Infof("Release %q has been successfully scheduled", key)
	}

	appKey, err := c.getAssociatedApplicationKey(rel)
	if err != nil {
		runtime.HandleError(fmt.Errorf("error fetching Application key for release %q (will not retry): %s", key, err))
		return noRetry
	}

	glog.V(4).Infof("Scheduling Application key %q", appKey)
	c.applicationWorkqueue.Add(appKey)

	glog.V(4).Infof("Done processing Release %q", key)

	return noRetry
}

func (c *Controller) syncApplicationHandler(key string) bool {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid object key (will not retry): %q", key))
		return noRetry
	}
	app, err := c.applicationLister.Applications(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			glog.V(3).Infof("Application %q has been deleted", key)
			return noRetry
		}

		runtime.HandleError(fmt.Errorf("failed to process Application %q (will retry): %s", key, err))

		return retry
	}

	glog.V(4).Infof("Fetching release pair for Application %q", key)
	incumbent, contender, err := c.getWorkingReleasePair(app)
	if err != nil {
		runtime.HandleError(fmt.Errorf("error syncing Application %q (will retry): %s", key, err))
		return retry
	}

	glog.V(4).Infof("Building a strategy excecutor for Application %q", key)
	strategyExecutor, err := c.buildExecutor(incumbent, contender)
	if err != nil {
		runtime.HandleError(fmt.Errorf("error syncing Application %q (will retry): %s", key, err))
		return retry
	}

	glog.V(4).Infof("Executing the strategy on Application %q", key)
	patches, transitions, err := strategyExecutor.Execute()
	if err != nil {
		runtime.HandleError(fmt.Errorf("error syncing Application %q (will not retry): %s", key, err))

		return noRetry
	}

	for _, t := range transitions {
		c.recorder.Eventf(
			strategyExecutor.contender.release,
			corev1.EventTypeNormal,
			"ReleaseStateTransitioned",
			"Release %q had its state %q transitioned to %q",
			shippercontroller.MetaKey(strategyExecutor.contender.release),
			t.State,
			t.New,
		)
	}

	if len(patches) == 0 {
		glog.V(4).Infof("Strategy verified, nothing to patch")
		return noRetry
	}

	glog.V(4).Infof("Strategy has been executed, applying patches")
	for _, patch := range patches {
		name, gvk, b := patch.PatchSpec()
		switch gvk.Kind {
		case "Release":
			if _, err := c.clientset.ShipperV1alpha1().Releases(namespace).Patch(name, types.MergePatchType, b); err != nil {
				runtime.HandleError(fmt.Errorf("error syncing Release for Application %q (will retry): %s", key, err))
				return retry
			}
		case "InstallationTarget":
			if _, err := c.clientset.ShipperV1alpha1().InstallationTargets(namespace).Patch(name, types.MergePatchType, b); err != nil {
				runtime.HandleError(fmt.Errorf("error syncing InstallationTarget for Application %q (will retry): %s", key, err))
				return retry
			}
		case "CapacityTarget":
			if _, err := c.clientset.ShipperV1alpha1().CapacityTargets(namespace).Patch(name, types.MergePatchType, b); err != nil {
				runtime.HandleError(fmt.Errorf("error syncing CapacityTarget for Application %q (will retry): %s", key, err))
				return retry
			}
		case "TrafficTarget":
			if _, err := c.clientset.ShipperV1alpha1().TrafficTargets(namespace).Patch(name, types.MergePatchType, b); err != nil {
				runtime.HandleError(fmt.Errorf("error syncing TrafficTarget for Application %q (will retry): %s", key, err))
				return retry
			}
		default:
			runtime.HandleError(fmt.Errorf("error syncing Application %q (will not retry): unknown GVK resource name: %s", key, gvk.Kind))
			return noRetry
		}
	}

	return noRetry
}

func (c *Controller) buildExecutor(incumbentRelease, contenderRelease *shipper.Release) (*Executor, error) {
	if !releaseutil.ReleaseScheduled(contenderRelease) {
		return nil, NewNotWorkingOnStrategyError(shippercontroller.MetaKey(contenderRelease))
	}

	contenderReleaseInfo, err := c.buildReleaseInfo(contenderRelease)
	if err != nil {
		return nil, err
	}

	strategy := *contenderReleaseInfo.release.Spec.Environment.Strategy

	// No incumbent, only this contender: a new application.
	if incumbentRelease == nil {
		return &Executor{
			contender: contenderReleaseInfo,
			recorder:  c.recorder,
			strategy:  strategy,
		}, nil
	}

	incumbentReleaseInfo, err := c.buildReleaseInfo(incumbentRelease)
	if err != nil {
		return nil, err
	}

	return &Executor{
		contender: contenderReleaseInfo,
		incumbent: incumbentReleaseInfo,
		recorder:  c.recorder,
		strategy:  strategy,
	}, nil
}

func (c *Controller) getAssociatedApplicationName(rel *shipper.Release) (string, error) {
	if n := len(rel.OwnerReferences); n != 1 {
		return "", shippercontroller.NewMultipleOwnerReferencesError(
			shippercontroller.MetaKey(rel), n)
	}

	appref := rel.OwnerReferences[0]

	return appref.Name, nil
}

func (c *Controller) getAssociatedApplicationKey(rel *shipper.Release) (string, error) {
	appName, err := c.getAssociatedApplicationName(rel)
	if err != nil {
		return "", err
	}

	appKey := fmt.Sprintf("%s/%s", rel.Namespace, appName)

	return appKey, nil
}

func (c *Controller) getAssociatedReleaseKey(obj *metav1.ObjectMeta) (string, error) {
	if n := len(obj.OwnerReferences); n != 1 {
		return "", shippercontroller.NewMultipleOwnerReferencesError(obj.Name, n)
	}

	owner := obj.OwnerReferences[0]

	return fmt.Sprintf("%s/%s", obj.Namespace, owner.Name), nil
}

func (c *Controller) sortedReleasesForApp(namespace, name string) ([]*shipper.Release, error) {
	selector := labels.Set{
		shipper.AppLabel: name,
	}.AsSelector()

	releases, err := c.releaseLister.Releases(namespace).List(selector)
	if err != nil {
		return nil, err
	}

	sorted, err := shippercontroller.SortReleasesByGeneration(releases)
	if err != nil {
		return nil, err
	}

	return sorted, nil
}

func (c *Controller) releaseIsHead(rel *shipper.Release) (bool, error) {
	appName, err := c.getAssociatedApplicationName(rel)
	if err != nil {
		return false, err
	}

	releases, err := c.sortedReleasesForApp(rel.GetNamespace(), appName)
	if err != nil {
		return false, err
	}

	return rel == releases[len(releases)-1], nil
}

func (c *Controller) releaseScheduled(rel *shipper.Release) (bool, error) {
	it, err := c.installationTargetLister.InstallationTargets(rel.GetNamespace()).Get(rel.GetName())
	if it == nil || err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	ct, err := c.capacityTargetLister.CapacityTargets(rel.GetNamespace()).Get(rel.GetName())
	if ct == nil || err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	tt, err := c.trafficTargetLister.TrafficTargets(rel.GetNamespace()).Get(rel.GetName())
	if tt == nil || err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

func (c *Controller) getHeadRelease(app *shipper.Application) (*shipper.Release, error) {
	appReleases, err := c.sortedReleasesForApp(app.GetNamespace(), app.GetName())
	if err != nil {
		return nil, err
	}

	if len(appReleases) == 0 {
		return nil, fmt.Errorf("app %q has no releases",
			shippercontroller.MetaKey(app))
	}

	return appReleases[len(appReleases)-1], nil
}

func (c *Controller) getWorkingReleasePair(app *shipper.Application) (*shipper.Release, *shipper.Release, error) {
	appReleases, err := c.sortedReleasesForApp(app.GetNamespace(), app.GetName())
	if err != nil {
		return nil, nil, err
	}

	if len(appReleases) == 0 {
		return nil, nil, fmt.Errorf(
			"zero release records in app %q: will not execute strategy",
			shippercontroller.MetaKey(app),
		)
	}

	// Walk backwards until we find a scheduled release. There may be pending
	// releases ahead of the actual contender, that's not we're looking for.
	var contender *shipper.Release
	for i := len(appReleases) - 1; i >= 0; i-- {
		if releaseutil.ReleaseScheduled(appReleases[i]) {
			contender = appReleases[i]
			break
		}
	}

	if contender == nil {
		return nil, nil, fmt.Errorf("couldn't find a contender for Application %q",
			shippercontroller.MetaKey(app))
	}

	var incumbent *shipper.Release
	// Walk backwards until we find an installed release that isn't the head of
	// history. Ffor releases A -> B -> C, if B was never finished this allows C to
	// ignore it and let it get deleted so the transition is A->C.
	for i := len(appReleases) - 1; i >= 0; i-- {
		if releaseutil.ReleaseComplete(appReleases[i]) && contender != appReleases[i] {
			incumbent = appReleases[i]
			break
		}
	}

	// It is OK if incumbent is nil. It just means this is our first rollout.
	return incumbent, contender, nil
}

func (c *Controller) buildReleaseInfo(rel *shipper.Release) (*releaseInfo, error) {
	installationTarget, err := c.installationTargetLister.InstallationTargets(rel.Namespace).Get(rel.Name)
	if err != nil {
		return nil, NewRetrievingInstallationTargetForReleaseError(shippercontroller.MetaKey(rel), err)
	}

	capacityTarget, err := c.capacityTargetLister.CapacityTargets(rel.Namespace).Get(rel.Name)
	if err != nil {
		return nil, NewRetrievingCapacityTargetForReleaseError(shippercontroller.MetaKey(rel), err)
	}

	trafficTarget, err := c.trafficTargetLister.TrafficTargets(rel.Namespace).Get(rel.Name)
	if err != nil {
		return nil, NewRetrievingTrafficTargetForReleaseError(shippercontroller.MetaKey(rel), err)
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
