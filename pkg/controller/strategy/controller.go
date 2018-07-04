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
	"github.com/bookingcom/shipper/pkg/conditions"
	shippercontroller "github.com/bookingcom/shipper/pkg/controller"
)

const AgentName = "strategy-controller"

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
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				rel, ok := obj.(*v1.Release)
				return ok && isScheduled(rel)
			},
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc: controller.enqueueRelease,
				UpdateFunc: func(oldObj, newObj interface{}) {
					rel := newObj.(*v1.Release)
					controller.enqueueRelease(rel)
				},
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

func isScheduled(r *v1.Release) bool {
	return conditions.IsReleaseConditionTrue(r.Status.Conditions, v1.ReleaseConditionTypeScheduled)
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
			"zero release records in app %s/%s: will not execute strategy",
			app.GetNamespace(), app.GetName(),
		)
	}

	contender := appReleases[len(appReleases)-1]
	var incumbent *v1.Release
	// walk backwards until we find an installed release that isn't the head of history
	// (for releases A -> B -> C, if B was never finished this allows C to
	// ignore it and let it get deleted so the transition is A->C)
	for i := len(appReleases) - 1; i >= 0; i-- {
		if conditions.IsReleaseInstalled(appReleases[i]) && contender != appReleases[i] {
			incumbent = appReleases[i]
			break
		}
	}

	// it is totally OK if incumbent is nil; that just means this is our first rollout
	return incumbent, contender, nil
}

func (c *Controller) getAssociatedApplicationKey(rel *v1.Release) string {
	if n := len(rel.OwnerReferences); n != 1 {
		glog.Warningf("expected exactly one OwnerReference for release '%s/%s', but got %d", rel.GetNamespace(), rel.GetName(), n)
		return ""
	}

	owningApp := rel.OwnerReferences[0]
	return fmt.Sprintf("%s/%s", rel.Namespace, owningApp.Name)
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
		runtime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
		return true
	}

	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return true
	}

	rel, err := c.releasesLister.Releases(ns).Get(name)
	if err != nil {
		runtime.HandleError(fmt.Errorf("failed to fetch release %s: %s", key, err))
		return true
	}

	appKey := c.getAssociatedApplicationKey(rel)
	if appKey != "" {
		c.appWorkqueue.Add(appKey)
	}
	return true
}

func (c *Controller) processNextAppWorkItem() bool {
	obj, shutdown := c.appWorkqueue.Get()

	if shutdown {
		return false
	}

	defer c.appWorkqueue.Done(obj)

	if key, ok := obj.(string); !ok {
		c.appWorkqueue.Forget(obj)
		runtime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
		return false
	} else {
		if err := c.syncOne(key); err != nil {
			runtime.HandleError(fmt.Errorf("error syncing: %q: %s", key, err.Error()))
			return false
		} else {
			c.appWorkqueue.Forget(obj)
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

	app, err := c.applicationsLister.Applications(ns).Get(name)
	if err != nil {
		return err
	}

	incumbent, contender, err := c.getWorkingReleasePair(app)
	if err != nil {
		return err
	}

	strategyExecutor, err := c.buildExecutor(incumbent, contender, c.recorder)
	if err != nil {
		return err
	}

	strategyExecutor.info("will start processing application")

	result, transitions, err := strategyExecutor.execute()
	if err != nil {
		return err
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
		return nil
	}

	strategyExecutor.info("strategy executed, patches to apply")
	for _, r := range result {
		name, gvk, b := r.PatchSpec()

		if client, err := c.clientForGroupVersionKind(gvk, ns); err != nil {
			return err
		} else if _, err = client.Patch(name, types.MergePatchType, b); err != nil {
			return err
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
	if !isScheduled(contenderRelease) {
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
		glog.Warningln(fmt.Errorf("object passed to enqueueInstallationTarget is _not_ a v1.InstallationTarget: %v", obj))
		return
	}

	releaseKey, err := c.getAssociatedReleaseKey(&it.ObjectMeta)
	if err != nil {
		glog.Warningln(err)
		return
	}

	c.releaseWorkqueue.AddRateLimited(releaseKey)
}

func (c *Controller) enqueueTrafficTarget(obj interface{}) {
	tt, ok := obj.(*v1.TrafficTarget)
	if !ok {
		glog.Warningln(fmt.Errorf("object passed to enqueueTrafficTarget is _not_ a v1.TrafficTarget: %v", obj))
		return
	}

	releaseKey, err := c.getAssociatedReleaseKey(&tt.ObjectMeta)
	if err != nil {
		glog.Warningln(err)
		return
	}

	c.releaseWorkqueue.AddRateLimited(releaseKey)
}

func (c *Controller) enqueueCapacityTarget(obj interface{}) {
	ct, ok := obj.(*v1.CapacityTarget)
	if !ok {
		glog.Warningln(fmt.Errorf("object passed to enqueueCapacityTarget is _not_ a v1.CapacityTarget: %v", obj))
		return
	}
	releaseKey, err := c.getAssociatedReleaseKey(&ct.ObjectMeta)
	if err != nil {
		glog.Warningln(err)
		return
	}

	c.releaseWorkqueue.AddRateLimited(releaseKey)
}

func (c *Controller) enqueueRelease(obj interface{}) {
	var (
		rel *v1.Release
		ok  bool
	)

	if _, ok = obj.(metav1.Object); !ok {
		_, ok = obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			runtime.HandleError(fmt.Errorf("neither a Meta object, nor a tombstone: %#v", obj))
			return
		}
		// TODO(btyler) work out Release end-of-life #24
		glog.V(5).Infof("trying to enqueue a deleted release. we don't know what to do with this yet: skipping. object: %#v", obj)
		return
	}

	rel, ok = obj.(*v1.Release)
	if !ok {
		runtime.HandleError(fmt.Errorf("enqueued something that isn't a release with enqueueRelease. %#v", obj))
		return
	}

	key, err := cache.MetaNamespaceKeyFunc(rel)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	glog.V(5).Infof("enqueued item %q", key)
	c.releaseWorkqueue.AddRateLimited(key)

	glog.V(5).Infof("inspecting release %s", shippercontroller.MetaKey(rel))
}
