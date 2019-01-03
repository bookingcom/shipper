package release

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

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/chart"
	shipperclient "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shipperlisters "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1alpha1"
	shippercontroller "github.com/bookingcom/shipper/pkg/controller"
	"github.com/bookingcom/shipper/pkg/controller/schedulecontroller"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

const (
	AgentName = "release-controller"

	maxRetries = 11

	WillRetry = true
	NoRetry   = false
)

type ReleaseController struct {
	clientset shipperclient.Interface

	appLister  shipperlisters.ApplicationLister
	capLister  shipperlisters.CapacityTargetLister
	clusLister shipperlisters.ClusterLister
	instLister shipperlisters.InstallationTargetLister
	relLister  shipperlisters.ReleaseLister
	trafLister shipperlisters.TrafficTargetLister

	appSynced  cache.InformerSynced
	capSynced  cache.InformerSynced
	clusSynced cache.InformerSynced
	instSynced cache.InformerSynced
	relSynced  cache.InformerSynced
	trafSynced cache.InformerSynced

	dynamicClientPool dynamic.ClientPool

	relQueue workqueue.RateLimitingInterface
	appQueue workqueue.RateLimitingInterface

	chartFetchFunc chart.FetchFunc

	recorder record.EventRecorder
}

func NewReleaseController(
	clientset shipperclient.Interface,
	informerFactory shipperinformers.SharedInformerFactory,
	chartFetchFunc chart.FetchFunc,
	dynamicClientPool dynamic.ClientPool,
	recorder record.EventRecorder,
) *ReleaseController {

	appInformer := informerFactory.Shipper().V1alpha1().Applications()
	capInformer := informerFactory.Shipper().V1alpha1().CapacityTargets()
	clusInformer := informerFactory.Shipper().V1alpha1().Clusters()
	instInformer := informerFactory.Shipper().V1alpha1().InstallationTargets()
	relInformer := informerFactory.Shipper().V1alpha1().Releases()
	trafInformer := informerFactory.Shipper().V1alpha1().TrafficTargets()

	rc := &ReleaseController{
		clientset: clientset,

		appLister:  informerFactory.Shipper().V1alpha1().Applications().Lister(),
		capLister:  informerFactory.Shipper().V1alpha1().CapacityTargets().Lister(),
		clusLister: informerFactory.Shipper().V1alpha1().Clusters().Lister(),
		instLister: informerFactory.Shipper().V1alpha1().InstallationTargets().Lister(),
		relLister:  informerFactory.Shipper().V1alpha1().Releases().Lister(),
		trafLister: informerFactory.Shipper().V1alpha1().TrafficTargets().Lister(),

		appSynced:  appInformer.Informer().HasSynced,
		capSynced:  capInformer.Informer().HasSynced,
		clusSynced: clusInformer.Informer().HasSynced,
		instSynced: instInformer.Informer().HasSynced,
		relSynced:  relInformer.Informer().HasSynced,
		trafSynced: trafInformer.Informer().HasSynced,

		relQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "release_controller_release_queue"),
		appQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "release_controller_app_queue"),

		dynamicClientPool: dynamicClientPool,
		chartFetchFunc:    chartFetchFunc,
		recorder:          recorder,
	}

	glog.V(1).Info("Setting up event handlers")

	relInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: rc.enqueueRelease,
			UpdateFunc: func(oldObj, newObj interface{}) {
				rc.enqueueRelease(newObj)
			},
		},
	)

	instInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: rc.enqueueInstallationTarget,
			UpdateFunc: func(oldObj, newObj interface{}) {
				rc.enqueueInstallationTarget(newObj)
			},
		},
	)

	capInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: rc.enqueueCapacityTarget,
			UpdateFunc: func(oldObj, newObj interface{}) {
				rc.enqueueCapacityTarget(newObj)
			},
		},
	)

	trafInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: rc.enqueueTrafficTarget,
			UpdateFunc: func(oldObj, newObj interface{}) {
				rc.enqueueTrafficTarget(newObj)
			},
		},
	)

	return rc
}

func (rc *ReleaseController) Run(threadiness int, stopCh <-chan struct{}) {
	defer runtime.HandleCrash()
	defer rc.relQueue.ShutDown()
	defer rc.appQueue.ShutDown()

	glog.V(2).Info("Starting Release controller")
	defer glog.V(2).Info("Shutting down Release controller")

	ok := cache.WaitForCacheSync(
		stopCh,
		rc.appSynced,
		rc.capSynced,
		rc.clusSynced,
		rc.instSynced,
		rc.relSynced,
		rc.trafSynced,
	)
	if !ok {
		runtime.HandleError(fmt.Errorf("Failed to wait for caches to sync"))
		return
	}

	for i := 0; i < threadiness; i++ {
		go wait.Until(rc.runReleaseWorker, time.Second, stopCh)
		go wait.Until(rc.runAppWorker, time.Second, stopCh)
	}

	glog.V(4).Info("Started Release controller")

	<-stopCh
}

func (rc *ReleaseController) runReleaseWorker() {
	for rc.processNextReleaseWorkItem() {
	}
}

func (rc *ReleaseController) runAppWorker() {
	for rc.processNextAppWorkItem() {
	}
}

func (rc *ReleaseController) processNextReleaseWorkItem() bool {
	obj, quit := rc.relQueue.Get()
	if quit {
		return false
	}

	defer rc.relQueue.Done(obj)
	defer rc.relQueue.Forget(obj)

	key, ok := obj.(string)
	if !ok {
		fmt.Println("bad key")
		runtime.HandleError(fmt.Errorf("Invalid object key (will not retry): %#v", obj))
		return true
	}

	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		fmt.Println("bad namespace")
		runtime.HandleError(fmt.Errorf("Invalid object key (will not retry): %q", key))
		return true
	}

	rel, err := rc.relLister.Releases(ns).Get(name)
	if err != nil {
		fmt.Println("bad release")
		runtime.HandleError(fmt.Errorf("Error syncing Release %q (will not retry): %s", key, err))
		return true
	}

	if releaseutil.IsEmpty(rel) {
		fmt.Println("empty release env")
		glog.V(1).Infof("Release %q has an empty Envieonment, bailing out", key)
		return true
	}

	if releaseutil.ReleaseScheduled(rel) {
		fmt.Println("already scheduled")
		glog.V(4).Infof("Release %q has already been scheduled, ignoring", key)
		return true
	}

	if wantRetry, err := rc.scheduleRelease(rel); err != nil {
		if wantRetry {
			if rc.relQueue.NumRequeues(key) >= maxRetries {
				glog.Warningf("Release %q has been retried too many times, dropping from the queue", key)
				// do I need it here even though defer will clean it up?
				rc.relQueue.Forget(key)
				fmt.Println("reenqueue")
				return true
			}
			rc.relQueue.AddRateLimited(key)
		}
		fmt.Println("error, no retry")
		return true
	}

	fmt.Println("I am here!")

	// Strategy controller path

	appKey, err := rc.getAssociatedApplicationKey(rel)
	if err != nil {
		runtime.HandleError(fmt.Errorf("Error syncing Release %q (will not retry): %s", key, err))
		return true
	}

	glog.V(4).Infof("Successfully synced Release %q", key)
	rc.appQueue.Add(appKey)

	return true
}

func (rc *ReleaseController) scheduleRelease(rel *shipper.Release) (bool, error) {
	if releaseutil.ReleaseScheduled(rel) {
		return NoRetry, fmt.Errorf("Release %q has already been scheduled", rel.Name)
	}

	scheduler := NewScheduler(
		rel,
		rc.clientset,
		rc.clusLister,
		rc.instLister,
		rc.capLister,
		rc.trafLister,
		rc.chartFetchFunc,
		rc.recorder,
	)
	if err := scheduler.ScheduleRelease(); err != nil {
		rc.recorder.Eventf(
			scheduler.Release,
			corev1.EventTypeWarning,
			"FailedReleaseScheduling",
			err.Error(),
		)
		reason, shouldRetry := schedulecontroller.ClassifyError(err)
		cond := releaseutil.NewReleaseCondition(
			shipper.ReleaseConditionTypeScheduled,
			corev1.ConditionFalse,
			reason,
			err.Error(),
		)

		releaseutil.SetReleaseCondition(&rel.Status, *cond)

		if _, err := rc.clientset.ShipperV1alpha1().Releases(rel.Namespace).Update(rel); err != nil {
			runtime.HandleError(fmt.Errorf("Error updating Release %q with condition (will retry): %s", rel.Name, err))
			return WillRetry, err
		}

		if shouldRetry {
			runtime.HandleError(fmt.Errorf("Error syncing Release %q (will retry): %s", rel.Name, err))
			return WillRetry, err
		}

		runtime.HandleError(fmt.Errorf("Error syncing Release %q (will not retry): %s", rel.Name, err))
		return NoRetry, err
	}

	return NoRetry, nil
}

func (rc *ReleaseController) processNextAppWorkItem() bool {
	obj, quit := rc.appQueue.Get()
	if quit {
		return false
	}

	defer rc.appQueue.Done(obj)

	key, ok := obj.(string)
	if !ok {
		rc.appQueue.Forget(obj)
		runtime.HandleError(fmt.Errorf("Invalid object key (will not retry): %#v", obj))
		return true
	}

	if wantRetry, err := rc.syncAppHandler(key); err != nil {
		if wantRetry {
			if rc.appQueue.NumRequeues(key) >= maxRetries {
				glog.Warningf("Application %q has been retried too many times, dropping from the queue", key)
				rc.appQueue.Forget(key)
				return true
			}
			rc.appQueue.AddRateLimited(key)
		}
		return true
	}
	rc.appQueue.Forget(obj)

	return true
}

func (rc *ReleaseController) syncAppHandler(key string) (bool, error) {
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("Invalid object key (will not retry): %q", key))
		return NoRetry, err
	}

	app, err := rc.appLister.Applications(ns).Get(name)
	if err != nil {
		runtime.HandleError(fmt.Errorf("Error syncing Application %s (will retry): %s", key, err))
		return WillRetry, err
	}

	incumbent, contender, err := rc.getWorkingReleasePair(app)
	if err != nil {
		runtime.HandleError(fmt.Errorf("Error syncing Application: %q (will retry): %s", key, err))
		return WillRetry, err
	}

	strategyExecutor, err := rc.buildExecutor(incumbent, contender, rc.recorder)
	if err != nil {
		runtime.HandleError(fmt.Errorf("Error syncing Application %q (will retry): %s", key, err))
		return WillRetry, err
	}

	strategyExecutor.info("Will start processing application")

	res, transitions, err := strategyExecutor.execute()
	if err != nil {
		runtime.HandleError(fmt.Errorf("Error syncing Application %q (will not retry): %s", key, err))
		return NoRetry, err
	}

	for _, t := range transitions {
		rc.recorder.Eventf(
			strategyExecutor.contender.release,
			corev1.EventTypeNormal,
			"ReleaseStateTransitioned",
			"Release %q had its state %q transitioned to %q",
			shippercontroller.MetaKey(strategyExecutor.contender.release),
			t.State, t.New,
		)
	}

	if len(res) == 0 {
		strategyExecutor.info("Strategy has been verified, nothing to patch")
		return NoRetry, nil
	}

	strategyExecutor.info("Strategy has been executed, patches to be applied")
	for _, r := range res {
		name, gvk, b := r.PatchSpec()
		client, err := rc.clientForGroupVersionKind(gvk, ns)
		if err != nil {
			runtime.HandleError(fmt.Errorf("Error syncing Application %q (will not retry): %s", key, err))
			return NoRetry, err
		}

		if _, err := client.Patch(name, types.MergePatchType, b); err != nil {

			runtime.HandleError(fmt.Errorf("Error syncing Application %q (will retry): %s", key, err))
			return WillRetry, err
		}
	}

	return NoRetry, nil
}

func (rc *ReleaseController) clientForGroupVersionKind(
	gvk schema.GroupVersionKind,
	ns string,
) (dynamic.ResourceInterface, error) {
	client, err := rc.dynamicClientPool.ClientForGroupVersionKind(gvk)
	if err != nil {
		return nil, err
	}

	var resource *metav1.APIResource
	gv := gvk.GroupVersion().String()

	resources, err := rc.clientset.Discovery().ServerResourcesForGroupVersion(gv)
	if err != nil {
		return nil, err
	}

	for _, r := range resources.APIResources {
		if r.Kind == gvk.Kind {
			resource = &r
			break
		}
	}

	if resource == nil {
		return nil, fmt.Errorf("Could not find the specified resource %q", gvk)
	}

	return client.Resource(resource, ns), nil
}

func (rc *ReleaseController) buildExecutor(incumbent, contender *shipper.Release, recorder record.EventRecorder) (*Executor, error) {
	if !releaseutil.ReleaseScheduled(contender) {
		return nil, NewNotWorkingOnStrategyError(shippercontroller.MetaKey(contender))
	}
	contenderInfo, err := rc.buildReleaseInfo(contender)
	if err != nil {
		return nil, err
	}

	strategy := *contenderInfo.release.Spec.Environment.Strategy

	if incumbent == nil {
		return &Executor{
			contender: contenderInfo,
			recorder:  recorder,
			strategy:  strategy,
		}, nil
	}

	incumbentInfo, err := rc.buildReleaseInfo(incumbent)
	if err != nil {
		return nil, err
	}

	return &Executor{
		contender: contenderInfo,
		incumbent: incumbentInfo,
		recorder:  recorder,
		strategy:  strategy,
	}, nil
}

func (rc *ReleaseController) getWorkingReleasePair(app *shipper.Application) (*shipper.Release, *shipper.Release, error) {
	releases, err := rc.sortedReleasesForApp(app)
	if err != nil {
		return nil, nil, err
	}

	if len(releases) == 0 {
		return nil, nil, fmt.Errorf("No release records in app %q: will not execute strategy", shippercontroller.MetaKey(app))
	}

	var contender *shipper.Release
	for i := len(releases) - 1; i >= 0; i-- {
		if releaseutil.ReleaseScheduled(releases[i]) {
			contender = releases[i]
			break
		}
	}

	if contender == nil {
		return nil, nil, fmt.Errorf("Couldn't find a contender for Application %q", shippercontroller.MetaKey(app))
	}

	var incumbent *shipper.Release
	for i := len(releases); i >= 0; i-- {
		if releaseutil.ReleaseComplete(releases[i]) && contender != releases[i] {
			incumbent = releases[i]
			break
		}
	}

	return incumbent, contender, nil
}

func (rc *ReleaseController) getAssociatedApplicationKey(rel *shipper.Release) (string, error) {
	if n := len(rel.OwnerReferences); n != 1 {
		return "", shippercontroller.NewMultipleOwnerReferencesError(shippercontroller.MetaKey(rel), n)
	}

	owningApp := rel.OwnerReferences[0]

	return fmt.Sprintf("%s/%s", rel.Namespace, owningApp.Name), nil
}

func (rc *ReleaseController) buildReleaseInfo(rel *shipper.Release) (*releaseInfo, error) {
	it, err := rc.instLister.InstallationTargets(rel.Namespace).Get(rel.Name)
	if err != nil {
		return nil, NewRetrievingInstallationTargetForReleaseError(shippercontroller.MetaKey(rel), err)
	}

	ct, err := rc.capLister.CapacityTargets(rel.Namespace).Get(rel.Name)
	if err != nil {
		return nil, NewRetrievingCapacityTargetForReleaseError(shippercontroller.MetaKey(rel), err)
	}

	tt, err := rc.trafLister.TrafficTargets(rel.Namespace).Get(rel.Name)
	if err != nil {
		return nil, NewRetrievingTrafficTargetForReleaseError(shippercontroller.MetaKey(rel), err)
	}

	return &releaseInfo{
		release:            rel,
		installationTarget: it,
		capacityTarget:     ct,
		trafficTarget:      tt,
	}, nil

}

func (rc *ReleaseController) sortedReleasesForApp(app *shipper.Application) ([]*shipper.Release, error) {
	selector := labels.Set{
		shipper.AppLabel: app.GetName(),
	}.AsSelector()

	releases, err := rc.relLister.Releases(app.GetNamespace()).List(selector)
	if err != nil {
		return nil, err
	}

	sorted, err := shippercontroller.SortReleasesByGeneration(releases)
	if err != nil {
		return nil, err
	}

	return sorted, nil
}

func (rc *ReleaseController) enqueueInstallationTarget(obj interface{}) {
	it, ok := obj.(*shipper.InstallationTarget)
	if !ok {
		runtime.HandleError(fmt.Errorf("Not a shipper.InstallationTarget: %#v", obj))
		return
	}

	relKey, err := rc.getAssociatedReleaseKey(&it.ObjectMeta)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	rc.relQueue.Add(relKey)
}

func (rc *ReleaseController) enqueueCapacityTarget(obj interface{}) {
	ct, ok := obj.(*shipper.CapacityTarget)
	if !ok {
		runtime.HandleError(fmt.Errorf("Not a shipper.CapacityTarget: %#v", obj))
	}

	relKey, err := rc.getAssociatedReleaseKey(&ct.ObjectMeta)
	if err != nil {
		runtime.HandleError(err)
	}

	rc.relQueue.Add(relKey)
}

func (rc *ReleaseController) enqueueTrafficTarget(obj interface{}) {
	tt, ok := obj.(*shipper.TrafficTarget)
	if !ok {
		runtime.HandleError(fmt.Errorf("Not a shipper.TrafficTarget: %#v", obj))
		return
	}

	releaseKey, err := rc.getAssociatedReleaseKey(&tt.ObjectMeta)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	rc.relQueue.Add(releaseKey)
}

func (rc *ReleaseController) enqueueRelease(obj interface{}) {
	rel, ok := obj.(*shipper.Release)
	if !ok {
		runtime.HandleError(fmt.Errorf("Not a shipper.Release: %#v", obj))
		return
	}

	relKey, err := cache.MetaNamespaceKeyFunc(rel)
	if err != nil {
		runtime.HandleError(err)
	}

	rc.relQueue.Add(relKey)
}

func (rc *ReleaseController) getAssociatedReleaseKey(obj *metav1.ObjectMeta) (string, error) {
	if n := len(obj.OwnerReferences); n != 1 {
		return "", shippercontroller.NewMultipleOwnerReferencesError(obj.Name, n)
	}

	owner := obj.OwnerReferences[0]

	return fmt.Sprintf("%s/%s", obj.Namespace, owner.Name), nil
}
