package strategy

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	"github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	informers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	listers "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1"
	shippercontroller "github.com/bookingcom/shipper/pkg/controller"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

const (
	AgentName = "strategy-controller"

	// maxRetries is the number of times an Application will be retried before we
	// drop it out of the workqueue. The number is chosen with the default rate
	// limiter in mind. This results in the following backoff times: 5ms, 10ms,
	// 20ms, 40ms, 80ms, 160ms, 320ms, 640ms, 1.3s, 2.6s, 5.1s, 10.2s.
	maxRetries = 11
)

type Controller struct {
	clientset                 shipperclientset.Interface
	capacityTargetsLister     listers.CapacityTargetLister
	installationTargetsLister listers.InstallationTargetLister
	trafficTargetsLister      listers.TrafficTargetLister
	applicationsLister        listers.ApplicationLister
	releasesLister            listers.ReleaseLister
	applicationsSynced        cache.InformerSynced
	releasesSynced            cache.InformerSynced
	capacityTargetsSynced     cache.InformerSynced
	trafficTargetsSynced      cache.InformerSynced
	installationTargetsSynced cache.InformerSynced
	dynamicClientPool         dynamic.ClientPool

	releaseWorkqueue workqueue.RateLimitingInterface
	appWorkqueue     workqueue.RateLimitingInterface
	recorder         record.EventRecorder
}

func NewController(
	shipperClient shipperclientset.Interface,
	informerFactory informers.SharedInformerFactory,
	dynamicClientPool dynamic.ClientPool,
	recorder record.EventRecorder,
) *Controller {
	releaseInformer := informerFactory.Shipper().V1().Releases()
	capacityTargetInformer := informerFactory.Shipper().V1().CapacityTargets()
	trafficTargetInformer := informerFactory.Shipper().V1().TrafficTargets()
	installationTargetInformer := informerFactory.Shipper().V1().InstallationTargets()

	controller := &Controller{
		clientset:                 shipperClient,
		capacityTargetsLister:     informerFactory.Shipper().V1().CapacityTargets().Lister(),
		installationTargetsLister: informerFactory.Shipper().V1().InstallationTargets().Lister(),
		trafficTargetsLister:      informerFactory.Shipper().V1().TrafficTargets().Lister(),
		applicationsLister:        informerFactory.Shipper().V1().Applications().Lister(),
		releasesLister:            releaseInformer.Lister(),
		applicationsSynced:        informerFactory.Shipper().V1().Applications().Informer().HasSynced,
		releasesSynced:            releaseInformer.Informer().HasSynced,
		capacityTargetsSynced:     capacityTargetInformer.Informer().HasSynced,
		trafficTargetsSynced:      trafficTargetInformer.Informer().HasSynced,
		installationTargetsSynced: installationTargetInformer.Informer().HasSynced,
		releaseWorkqueue:          workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "strategy_controller_releases"),
		appWorkqueue:              workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "strategy_controller_applications"),
		dynamicClientPool:         dynamicClientPool,
		recorder:                  recorder,
	}

	releaseInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: controller.enqueueRelease,
			UpdateFunc: func(oldObj, newObj interface{}) {
				controller.enqueueRelease(newObj)
			},
		},
	)

	// The InstallationTarget object should have the same name as the Release
	// object it is associated with.
	installationTargetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueInstallationTarget,
		UpdateFunc: func(oldObj, newObj interface{}) {
			controller.enqueueInstallationTarget(newObj)
		},
	})

	// The CapacityTarget object should have the same name as the Release
	// object it is associated with.
	capacityTargetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueCapacityTarget,
		UpdateFunc: func(oldObj, newObj interface{}) {
			controller.enqueueCapacityTarget(newObj)
		},
	})

	// The TrafficTarget object should have the same name as the Release
	// object it is associate with.
	trafficTargetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueTrafficTarget,
		UpdateFunc: func(oldObj, newObj interface{}) {
			controller.enqueueTrafficTarget(newObj)
		},
	})

	return controller
}

func (c *Controller) sortedReleasesForApp(app *v1.Application) ([]*v1.Release, error) {
	selector := labels.Set{
		v1.AppLabel: app.GetName(),
	}.AsSelector()

	releases, err := c.releasesLister.Releases(app.GetNamespace()).List(selector)
	if err != nil {
		return nil, err
	}

	sorted, err := shippercontroller.SortReleasesByGeneration(releases)
	if err != nil {
		return nil, err
	}

	return sorted, nil
}

func (c *Controller) getWorkingReleasePair(app *v1.Application) (*v1.Release, *v1.Release, error) {
	appReleases, err := c.sortedReleasesForApp(app)
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
	var contender *v1.Release
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

	var incumbent *v1.Release
	// walk backwards until we find an installed release that isn't the head of history
	// (for releases A -> B -> C, if B was never finished this allows C to
	// ignore it and let it get deleted so the transition is A->C)
	for i := len(appReleases) - 1; i >= 0; i-- {
		if releaseutil.ReleaseComplete(appReleases[i]) && contender != appReleases[i] {
			incumbent = appReleases[i]
			break
		}
	}

	// it is totally OK if incumbent is nil; that just means this is our first rollout
	return incumbent, contender, nil
}

func (c *Controller) getAssociatedApplicationKey(rel *v1.Release) (string, error) {
	if n := len(rel.OwnerReferences); n != 1 {
		return "", shippercontroller.NewMultipleOwnerReferencesError(shippercontroller.MetaKey(rel), n)
	}

	owningApp := rel.OwnerReferences[0]
	return fmt.Sprintf("%s/%s", rel.Namespace, owningApp.Name), nil
}

func (c *Controller) getAssociatedReleaseKey(obj *metav1.ObjectMeta) (string, error) {
	if n := len(obj.OwnerReferences); n != 1 {
		return "", shippercontroller.NewMultipleOwnerReferencesError(obj.Name, n)
	}

	owner := obj.OwnerReferences[0]

	return fmt.Sprintf("%s/%s", obj.Namespace, owner.Name), nil
}

func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) {
	defer runtime.HandleCrash()
	defer c.appWorkqueue.ShutDown()

	glog.V(2).Info("Starting Strategy controller")
	defer glog.V(2).Info("Shutting down Strategy controller")

	ok := cache.WaitForCacheSync(
		stopCh,
		c.applicationsSynced,
		c.releasesSynced,
		c.installationTargetsSynced,
		c.trafficTargetsSynced,
		c.capacityTargetsSynced,
	)

	if !ok {
		runtime.HandleError(fmt.Errorf("failed to wait for caches to sync"))
		return
	}

	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runReleaseWorker, time.Second, stopCh)
		go wait.Until(c.runAppWorker, time.Second, stopCh)
	}

	glog.V(4).Info("Started Strategy controller")

	<-stopCh
}

func (c *Controller) runAppWorker() {
	for c.processNextAppWorkItem() {
	}
}

func (c *Controller) runReleaseWorker() {
	for c.processNextReleaseWorkItem() {
	}
}

func (c *Controller) processNextReleaseWorkItem() bool {
	obj, shutdown := c.releaseWorkqueue.Get()
	if shutdown {
		return false
	}

	defer c.releaseWorkqueue.Done(obj)
	defer c.releaseWorkqueue.Forget(obj)

	var (
		key string
		ok  bool
	)

	if key, ok = obj.(string); !ok {
		runtime.HandleError(fmt.Errorf("invalid object key (will not retry): %#v", obj))
		return true
	}

	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid object key (will not retry): %q", key))
		return true
	}

	rel, err := c.releasesLister.Releases(ns).Get(name)
	if err != nil {
		runtime.HandleError(fmt.Errorf("error syncing Release %q (will not retry): %s", key, err))
		return true
	}

	if !releaseutil.ReleaseScheduled(rel) {
		glog.V(4).Infof("Release %q is not scheduled yet, skipping", key)
		return false
	}

	appKey, err := c.getAssociatedApplicationKey(rel)
	if err != nil {
		runtime.HandleError(fmt.Errorf("error syncing Release %q (will not retry): %s", key, err))
		return true
	}

	glog.V(4).Infof("Successfully synced Release %q", key)
	c.appWorkqueue.Add(appKey)

	return true
}

func (c *Controller) processNextAppWorkItem() bool {
	obj, shutdown := c.appWorkqueue.Get()
	if shutdown {
		return false
	}

	defer c.appWorkqueue.Done(obj)

	var (
		key string
		ok  bool
	)

	if key, ok = obj.(string); !ok {
		c.appWorkqueue.Forget(obj)
		runtime.HandleError(fmt.Errorf("invalid object key (will not retry): %#v", obj))
		return true
	}

	if shouldRetry := c.syncOne(key); shouldRetry {
		if c.appWorkqueue.NumRequeues(key) >= maxRetries {
			// Drop the Applications's key out of the workqueue and thus reset its
			// backoff. This limits the time a "broken" object can hog a worker.
			glog.Warningf("Application %q has been retried too many times, dropping from the queue", key)
			c.appWorkqueue.Forget(key)

			return true
		}

		c.appWorkqueue.AddRateLimited(key)

		return true
	}

	glog.V(4).Infof("Successfully synced Application %q", key)
	c.appWorkqueue.Forget(obj)

	return true
}

func (c *Controller) syncOne(key string) bool {
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid object key (will not retry): %q", key))
		return false
	}

	app, err := c.applicationsLister.Applications(ns).Get(name)
	if err != nil {
		runtime.HandleError(fmt.Errorf("error syncing Application %q (will retry): %s", key, err))
		return true
	}

	incumbent, contender, err := c.getWorkingReleasePair(app)
	if err != nil {
		runtime.HandleError(fmt.Errorf("error syncing Application %q (will retry): %s", key, err))
		return true
	}

	strategyExecutor, err := c.buildExecutor(incumbent, contender, c.recorder)
	if err != nil {
		runtime.HandleError(fmt.Errorf("error syncing Application %q (will retry): %s", key, err))
		return true
	}

	strategyExecutor.info("will start processing application")

	result, transitions, err := strategyExecutor.execute()
	if err != nil {
		// Currently the only error that can happen here is "invalid strategy step".
		// Doesn't make sense to retry that.
		runtime.HandleError(fmt.Errorf("error syncing Application %q (will not retry): %s", key, err))
		return false
	}

	for _, t := range transitions {
		c.recorder.Eventf(
			strategyExecutor.contender.release,
			corev1.EventTypeNormal,
			"ReleaseStateTransitioned",
			"Release %q had its state %q transitioned to %q",
			shippercontroller.MetaKey(strategyExecutor.contender.release),
			t.State, t.New,
		)
	}

	if len(result) == 0 {
		strategyExecutor.info("strategy verified, nothing to patch")
		return false
	}

	strategyExecutor.info("strategy executed, patches to apply")
	for _, r := range result {
		name, gvk, b := r.PatchSpec()

		client, err := c.clientForGroupVersionKind(gvk, ns)
		if err != nil {
			runtime.HandleError(fmt.Errorf("error syncing Application %q (will not retry): %s", key, err))
			return false
		}

		if _, err := client.Patch(name, types.MergePatchType, b); err != nil {
			runtime.HandleError(fmt.Errorf("error syncing Application %q (will retry): %s", key, err))
			return true
		}
	}

	return false
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
		return nil, fmt.Errorf("could not find the specified resource %q", gvk)
	}

	return client.Resource(resource, ns), nil
}

func (c *Controller) buildReleaseInfo(release *v1.Release) (*releaseInfo, error) {
	installationTarget, err := c.installationTargetsLister.InstallationTargets(release.Namespace).Get(release.Name)
	if err != nil {
		return nil, NewRetrievingInstallationTargetForReleaseError(shippercontroller.MetaKey(release), err)
	}

	capacityTarget, err := c.capacityTargetsLister.CapacityTargets(release.Namespace).Get(release.Name)
	if err != nil {
		return nil, NewRetrievingCapacityTargetForReleaseError(shippercontroller.MetaKey(release), err)
	}

	trafficTarget, err := c.trafficTargetsLister.TrafficTargets(release.Namespace).Get(release.Name)
	if err != nil {
		return nil, NewRetrievingTrafficTargetForReleaseError(shippercontroller.MetaKey(release), err)
	}

	return &releaseInfo{
		release:            release,
		installationTarget: installationTarget,
		trafficTarget:      trafficTarget,
		capacityTarget:     capacityTarget,
	}, nil
}

func (c *Controller) buildExecutor(incumbentRelease, contenderRelease *v1.Release, recorder record.EventRecorder) (*Executor, error) {
	if !releaseutil.ReleaseScheduled(contenderRelease) {
		return nil, NewNotWorkingOnStrategyError(shippercontroller.MetaKey(contenderRelease))
	}

	contenderReleaseInfo, err := c.buildReleaseInfo(contenderRelease)
	if err != nil {
		return nil, err
	}

	strategy := *contenderReleaseInfo.release.Environment.Strategy

	// no incumbent, only this contender: a new application
	if incumbentRelease == nil {
		return &Executor{
			contender: contenderReleaseInfo,
			recorder:  recorder,
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
		recorder:  recorder,
		strategy:  strategy,
	}, nil
}

func (c *Controller) enqueueInstallationTarget(obj interface{}) {
	it, ok := obj.(*v1.InstallationTarget)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipperv1.InstallationTarget: %#v", obj))
		return
	}

	releaseKey, err := c.getAssociatedReleaseKey(&it.ObjectMeta)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	c.releaseWorkqueue.Add(releaseKey)
}

func (c *Controller) enqueueTrafficTarget(obj interface{}) {
	tt, ok := obj.(*v1.TrafficTarget)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipperv1.TrafficTarget: %#v", obj))
		return
	}

	releaseKey, err := c.getAssociatedReleaseKey(&tt.ObjectMeta)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	c.releaseWorkqueue.Add(releaseKey)
}

func (c *Controller) enqueueCapacityTarget(obj interface{}) {
	ct, ok := obj.(*v1.CapacityTarget)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipperv1.CapacityTarget: %#v", obj))
		return
	}

	releaseKey, err := c.getAssociatedReleaseKey(&ct.ObjectMeta)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	c.releaseWorkqueue.Add(releaseKey)
}

func (c *Controller) enqueueRelease(obj interface{}) {
	rel, ok := obj.(*v1.Release)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipperv1.Release: %#v", obj))
		return
	}

	key, err := cache.MetaNamespaceKeyFunc(rel)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	c.releaseWorkqueue.Add(key)
}
