package strategy

import (
	"fmt"
	"time"

	"github.com/golang/glog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	workqueue                 workqueue.RateLimitingInterface
	recorder                  record.EventRecorder
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
		workqueue:                 workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "strategy_controller_releases"),
		dynamicClientPool:         dynamicClientPool,
		recorder:                  recorder,
	}

	releaseInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueRelease,
		UpdateFunc: func(oldObj, newObj interface{}) {
			rel := newObj.(*v1.Release)
			if isWorkingOnStrategy(rel) {
				// We should enqueue only releases that have been modified AND
				// are in the middle of a strategy execution.
				controller.enqueueRelease(rel)
			}
		},
	})

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

func isWorkingOnStrategy(r *v1.Release) (workingOnStrategy bool) {
	switch r.Status.Phase {
	case
		v1.ReleasePhaseWaitingForCommand,
		v1.ReleasePhaseWaitingForStrategy:
		workingOnStrategy = true
	default:
		workingOnStrategy = false
	}

	return workingOnStrategy
}

func (c *Controller) contenderForRelease(r *v1.Release) (*v1.Release, error) {
	app := c.getAssociatedApplication(r)
	if app == nil {
		return nil, fmt.Errorf("could not find application associated with %q", shippercontroller.MetaKey(r))
	}

	history := app.Status.History
	if len(history) <= 1 {
		return nil, nil
	}

	expectedIncumbentIndex := len(app.Status.History) - 2
	incumbentRecord := history[expectedIncumbentIndex]
	// TODO(btyler) is this a reasonable decision? the strategy only knows how to operate with the latest two releases
	if incumbentRecord.Name != r.GetName() {
		return nil, fmt.Errorf("release %s isn't the penultimate record in history, so we shouldn't be touching it", shippercontroller.MetaKey(r))
	}

	contenderRecord := app.Status.History[len(app.Status.History)-1]
	contender, err := c.releasesLister.Releases(r.GetNamespace()).Get(contenderRecord.Name)
	if err != nil {
		return nil, err
	}

	return contender, nil
}

func isInstalled(r *v1.Release) bool {
	return r.Status.Phase == v1.ReleasePhaseInstalled
}

func (c *Controller) getAssociatedApplication(rel *v1.Release) *v1.Application {
	if n := len(rel.OwnerReferences); n != 1 {
		glog.Warningf("expected exactly one OwnerReference for release '%s/%s', but got %d", rel.GetNamespace(), rel.GetName(), n)
		return nil
	}

	owningApp := rel.OwnerReferences[0]

	app, err := c.applicationsLister.Applications(rel.Namespace).Get(owningApp.Name)
	if err != nil {
		// This target object will soon be GC-ed.
		return nil
	}

	return app
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
	defer c.workqueue.ShutDown()

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
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	glog.V(4).Info("Started Strategy controller")

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

	strategyExecutor, err := c.buildExecutor(ns, name, c.recorder)
	if err != nil {
		return err
	}

	strategyExecutor.info("will start processing release")

	result, err := strategyExecutor.execute()
	if err != nil {
		return err
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

func (c *Controller) buildExecutor(ns, name string, recorder record.EventRecorder) (*Executor, error) {
	release, err := c.releasesLister.Releases(ns).Get(name)
	if err != nil {
		return nil, err
	}

	if isInstalled(release) {
		// If the release we got is installed, it means it is
		// the incumbent, so we need to find the contender
		// ourselves. This happens because both the contender
		// and the incumbent end up in the workqueue.

		// Check if there is a contender release for given release.
		var contenderRelease *v1.Release
		contenderRelease, err = c.contenderForRelease(release)
		if err != nil {
			return nil, err
		}

		if contenderRelease == nil {
			return nil, fmt.Errorf("Release %s is already installed, and we couldn't find a contender for it.", shippercontroller.MetaKey(release))
		}

		if !isWorkingOnStrategy(contenderRelease) {
			return nil, NewNotWorkingOnStrategyError(shippercontroller.MetaKey(contenderRelease), shippercontroller.MetaKey(release))
		}

		glog.V(5).Infof("Found %s as a contender for %s, switching to work on the contender", shippercontroller.MetaKey(contenderRelease), shippercontroller.MetaKey(release))
		release = contenderRelease
	}

	contenderReleaseInfo, err := c.buildReleaseInfo(release)
	if err != nil {
		return nil, err
	}

	app := c.getAssociatedApplication(contenderReleaseInfo.release)
	if app == nil {
		return nil, fmt.Errorf("no application associated with release %s", shippercontroller.MetaKey(release))
	}

	history := app.Status.History
	if len(history) == 0 {
		return nil, fmt.Errorf(
			"zero release records in app %s/%s (owner of release %s) Status.History: will not execute strategy",
			app.GetNamespace(), app.GetName(), name,
		)
	}

	expectedContenderRecord := history[len(history)-1]
	if expectedContenderRecord.Name != release.GetName() {
		return nil, fmt.Errorf("contender %s is not the latest release in app history: will not execute strategy", shippercontroller.MetaKey(release))
	}

	strategy := *contenderReleaseInfo.release.Environment.Strategy

	// no incumbent, only this contender: a new application
	if len(history) == 1 {
		return &Executor{
			contender: contenderReleaseInfo,
			recorder:  recorder,
			strategy:  strategy,
		}, nil
	}

	incumbentRecord := history[len(history)-2]

	incumbentRelease, err := c.releasesLister.Releases(ns).Get(incumbentRecord.Name)
	if err != nil {
		return nil, err
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
	it := obj.(*v1.InstallationTarget)
	releaseKey, err := c.getAssociatedReleaseKey(&it.ObjectMeta)
	if err != nil {
		glog.Warningln(err)
		return
	}

	c.workqueue.AddRateLimited(releaseKey)
}

func (c *Controller) enqueueTrafficTarget(obj interface{}) {
	tt := obj.(*v1.TrafficTarget)
	releaseKey, err := c.getAssociatedReleaseKey(&tt.ObjectMeta)
	if err != nil {
		glog.Warningln(err)
		return
	}

	c.workqueue.AddRateLimited(releaseKey)
}

func (c *Controller) enqueueCapacityTarget(obj interface{}) {
	ct := obj.(*v1.CapacityTarget)
	releaseKey, err := c.getAssociatedReleaseKey(&ct.ObjectMeta)
	if err != nil {
		glog.Warningln(err)
		return
	}

	c.workqueue.AddRateLimited(releaseKey)
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
	c.workqueue.AddRateLimited(key)

	glog.V(5).Infof("inspecting release %s", shippercontroller.MetaKey(rel))
}
