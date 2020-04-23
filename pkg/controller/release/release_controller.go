package release

import (
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperrepo "github.com/bookingcom/shipper/pkg/chart/repo"
	shipperclient "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shipperlisters "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/clusterclientstore"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	"github.com/bookingcom/shipper/pkg/util/conditions"
	"github.com/bookingcom/shipper/pkg/util/diff"
	diffutil "github.com/bookingcom/shipper/pkg/util/diff"
	objectutil "github.com/bookingcom/shipper/pkg/util/object"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
	rolloutblock "github.com/bookingcom/shipper/pkg/util/rolloutblock"
	shipperworkqueue "github.com/bookingcom/shipper/pkg/workqueue"
)

const (
	AgentName = "release-controller"

	ClustersChosen          = "ClustersChosen"
	InternalError           = "InternalError"
	StrategyExecutionFailed = "StrategyExecutionFailed"
)

// Controller is a Kubernetes controller whose role is to pick up a newly created
// release and progress it forward by scheduling the release on a set of
// selected clusters, creating a set of associated objects and executing the
// strategy.
type Controller struct {
	clientset shipperclient.Interface
	store     clusterclientstore.Interface

	releaseLister  shipperlisters.ReleaseLister
	releasesSynced cache.InformerSynced

	clusterLister  shipperlisters.ClusterLister
	clustersSynced cache.InformerSynced

	rolloutBlockLister shipperlisters.RolloutBlockLister
	rolloutBlockSynced cache.InformerSynced

	workqueue workqueue.RateLimitingInterface

	chartFetcher shipperrepo.ChartFetcher

	recorder record.EventRecorder
}

type releaseInfo struct {
	release            *shipper.Release
	installationTarget *shipper.InstallationTarget
	trafficTarget      *shipper.TrafficTarget
	capacityTarget     *shipper.CapacityTarget
}

type listers struct {
	installationTargetLister shipperlisters.InstallationTargetLister
	capacityTargetLister     shipperlisters.CapacityTargetLister
	trafficTargetLister      shipperlisters.TrafficTargetLister
}

func NewController(
	clientset shipperclient.Interface,
	store clusterclientstore.Interface,
	informerFactory shipperinformers.SharedInformerFactory,
	chartFetcher shipperrepo.ChartFetcher,
	recorder record.EventRecorder,
) *Controller {

	releaseInformer := informerFactory.Shipper().V1alpha1().Releases()
	clusterInformer := informerFactory.Shipper().V1alpha1().Clusters()
	rolloutBlockInformer := informerFactory.Shipper().V1alpha1().RolloutBlocks()

	controller := &Controller{
		clientset: clientset,
		store:     store,

		releaseLister:  releaseInformer.Lister(),
		releasesSynced: releaseInformer.Informer().HasSynced,

		clusterLister:  clusterInformer.Lister(),
		clustersSynced: clusterInformer.Informer().HasSynced,

		rolloutBlockLister: rolloutBlockInformer.Lister(),
		rolloutBlockSynced: rolloutBlockInformer.Informer().HasSynced,

		workqueue: workqueue.NewNamedRateLimitingQueue(
			shipperworkqueue.NewDefaultControllerRateLimiter(),
			"release_controller_releases",
		),

		chartFetcher: chartFetcher,

		recorder: recorder,
	}

	releaseInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: controller.enqueueReleaseAndNeighbours,
			UpdateFunc: func(oldObj, newObj interface{}) {
				controller.enqueueReleaseAndNeighbours(newObj)
			},
			DeleteFunc: controller.enqueueReleaseAndNeighbours,
		})

	rolloutBlockInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			DeleteFunc: controller.enqueueReleaseFromRolloutBlock,
		})

	eventHandler := cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueReleaseFromAssociatedObject,
		UpdateFunc: func(oldObj, newObj interface{}) {
			controller.enqueueReleaseFromAssociatedObject(newObj)
		},
		DeleteFunc: controller.enqueueReleaseFromAssociatedObject,
	}

	store.AddSubscriptionCallback(func(kubeInformerFactory kubeinformers.SharedInformerFactory, shipperInformerFactory shipperinformers.SharedInformerFactory) {
		shipperv1alpha1 := shipperInformerFactory.Shipper().V1alpha1()
		shipperv1alpha1.InstallationTargets().Informer()
		shipperv1alpha1.CapacityTargets().Informer()
		shipperv1alpha1.TrafficTargets().Informer()
	})

	store.AddEventHandlerCallback(func(kubeInformerFactory kubeinformers.SharedInformerFactory, shipperInformerFactory shipperinformers.SharedInformerFactory) {
		shipperv1alpha1 := shipperInformerFactory.Shipper().V1alpha1()
		shipperv1alpha1.InstallationTargets().Informer().AddEventHandler(eventHandler)
		shipperv1alpha1.CapacityTargets().Informer().AddEventHandler(eventHandler)
		shipperv1alpha1.TrafficTargets().Informer().AddEventHandler(eventHandler)
	})

	return controller
}

// Run starts Release Controller workers and waits until stopCh is closed.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	klog.V(2).Info("Starting Release controller")
	defer klog.V(2).Info("Shutting down Release controller")

	if ok := cache.WaitForCacheSync(
		stopCh,
		c.releasesSynced,
		c.clustersSynced,
		c.rolloutBlockSynced,
	); !ok {
		runtime.HandleError(fmt.Errorf("failed to wait for caches to sync"))
		return
	}

	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	klog.V(4).Info("Started Release controller")

	<-stopCh
}

func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem pops an element from the head of the workqueue and
// passes to the sync release handler. It returns bool indicating if the
// execution process should go on.
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
		runtime.HandleError(fmt.Errorf("invalid object key (will retry: false): %#v", obj))
		return true
	}

	shouldRetry := false
	err := c.syncHandler(key)

	if err != nil {
		shouldRetry = shippererrors.ShouldRetry(err)
		runtime.HandleError(fmt.Errorf("error syncing Release %q (will retry: %t): %s", key, shouldRetry, err.Error()))
	}

	if shouldRetry {
		c.workqueue.AddRateLimited(key)
		return true
	}

	klog.V(4).Infof("Successfully synced Release %q", key)
	c.workqueue.Forget(obj)

	return true
}

// syncHandler processes release keys one-by-one. This stage assigns a set of
// chosen clusters, creates required associated objects and executes the
// strategy.
func (c *Controller) syncHandler(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return shippererrors.NewUnrecoverableError(err)
	}

	initialRel, err := c.releaseLister.Releases(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.V(3).Infof("Release %q not found", key)
			return nil
		}

		return shippererrors.NewKubeclientGetError(namespace, name, err).
			WithShipperKind("Release")
	}

	if releaseutil.HasEmptyEnvironment(initialRel) {
		return nil
	}

	rel, err := c.processRelease(initialRel.DeepCopy())

	if !equality.Semantic.DeepEqual(initialRel, rel) {
		_, err := c.clientset.ShipperV1alpha1().Releases(namespace).
			Update(rel)
		if err != nil {
			return shippererrors.NewKubeclientUpdateError(rel, err).
				WithShipperKind("Release")
		}
	}

	return err
}

func (c *Controller) processRelease(rel *shipper.Release) (*shipper.Release, error) {
	diff := diffutil.NewMultiDiff()
	defer func() {
		if !diff.IsEmpty() {
			c.recorder.Event(rel, corev1.EventTypeNormal, "ReleaseConditionChanged", diff.String())
		}
	}()

	rolloutBlocked, events, err := rolloutblock.BlocksRollout(c.rolloutBlockLister, rel)
	for _, ev := range events {
		c.recorder.Event(rel, ev.Type, ev.Reason, ev.Message)
	}

	if rolloutBlocked {
		var msg string
		if err != nil {
			msg = err.Error()
		}

		condition := releaseutil.NewReleaseCondition(
			shipper.ReleaseConditionTypeBlocked,
			corev1.ConditionTrue,
			shipper.RolloutBlockReason,
			msg,
		)
		diff.Append(releaseutil.SetReleaseCondition(&rel.Status, *condition))

		return rel, err
	}

	condition := releaseutil.NewReleaseCondition(
		shipper.ReleaseConditionTypeBlocked,
		corev1.ConditionFalse,
		"",
		"",
	)
	diff.Append(releaseutil.SetReleaseCondition(&rel.Status, *condition))

	rel, clusterNames, err := c.chooseClusters(rel)
	if err != nil {
		reason := reasonForReleaseCondition(err)
		condition := releaseutil.NewReleaseCondition(
			shipper.ReleaseConditionTypeClustersChosen,
			corev1.ConditionFalse,
			reason,
			err.Error(),
		)
		diff.Append(releaseutil.SetReleaseCondition(&rel.Status, *condition))

		return rel, err
	}

	condition = releaseutil.NewReleaseCondition(
		shipper.ReleaseConditionTypeClustersChosen,
		corev1.ConditionTrue,
		ClustersChosen,
		strings.Join(clusterNames, ","),
	)
	diff.Append(releaseutil.SetReleaseCondition(&rel.Status, *condition))

	rel, err = c.executeStrategyOnClusters(rel, clusterNames, diff)
	if err != nil {
		releaseStrategyExecutedCond := releaseutil.NewReleaseCondition(
			shipper.ReleaseConditionTypeStrategyExecuted,
			corev1.ConditionFalse,
			StrategyExecutionFailed,
			err.Error(),
		)
		diff.Append(releaseutil.SetReleaseCondition(&rel.Status, *releaseStrategyExecutedCond))

		return rel, err
	}

	condition = releaseutil.NewReleaseCondition(
		shipper.ReleaseConditionTypeStrategyExecuted,
		corev1.ConditionTrue,
		"",
		"",
	)
	diff.Append(releaseutil.SetReleaseCondition(&rel.Status, *condition))

	return rel, nil
}

// NOTE(jgreff): executeStrategyOnClusters still refers to Schedule. this is
// going to be removed at the end of the migration of it, ct and tt to app
// clusters, as the old "scheduling" will become either choosing clusters or
// executing strategy, with nothing in between.
func (c *Controller) executeStrategyOnClusters(
	rel *shipper.Release,
	clusters []string,
	diff *diff.MultiDiff,
) (*shipper.Release, error) {
	prev, succ, err := c.getSiblingReleases(rel)
	if err != nil {
		return rel, err
	}

	isHead := succ == nil

	if !isHead && !releaseutil.IsLastStrategyStep(rel) {
		// If there is a successor to the current release, we have to
		// ensure it's spec points to the last strategy step.

		// In practice, this situation most likely means an external
		// modification to the current release object, which can
		// potentially cause some harmful consequences like: a
		// historical release gets activated.
		return rel, shippererrors.NewInconsistentReleaseTargetStep(
			objectutil.MetaKey(rel),
			rel.Spec.TargetStep,
			int32(len(rel.Spec.Environment.Strategy.Steps)-1),
		)
	}

	var strategy *shipper.RolloutStrategy
	var targetStep int32
	// A head release uses it's local spec-defined strategy, any other release
	// follows it's successor state, therefore looking into the forecoming spec.
	if isHead {
		strategy = rel.Spec.Environment.Strategy
		targetStep = rel.Spec.TargetStep
	} else {
		strategy = succ.Spec.Environment.Strategy
		targetStep = succ.Spec.TargetStep
	}

	executor, err := NewStrategyExecutor(strategy, targetStep)
	if err != nil {
		return rel, err
	}

	clusterConditions := make(map[string]conditions.StrategyConditionsMap)
	for _, clusterName := range clusters {
		clusterClientsets, err := c.store.GetApplicationClusterClientset(clusterName, AgentName)
		if err != nil {
			return rel, err
		}

		informerFactory := clusterClientsets.GetShipperInformerFactory()
		shipperv1alpha1 := informerFactory.Shipper().V1alpha1()
		listers := listers{
			installationTargetLister: shipperv1alpha1.InstallationTargets().Lister(),
			capacityTargetLister:     shipperv1alpha1.CapacityTargets().Lister(),
			trafficTargetLister:      shipperv1alpha1.TrafficTargets().Lister(),
		}

		clusterConditions[clusterName], err = c.executeReleaseStrategyForCluster(
			rel.DeepCopy(),
			prev, succ,
			clusterClientsets.GetShipperClient(),
			executor,
			listers)
		if err != nil {
			return rel, err
		}
	}

	isLastStep := int(targetStep) == len(strategy.Steps)-1
	stepComplete, strategyStatus := consolidateStrategyStatus(
		isHead, isLastStep, clusterConditions)

	rel.Status.Strategy = strategyStatus

	if stepComplete {
		prevStep := rel.Status.AchievedStep

		var achievedStep int32
		var achievedStepName string
		if isHead {
			achievedStep = targetStep
			achievedStepName = strategy.Steps[achievedStep].Name
		} else {
			achievedStep = int32(len(rel.Spec.Environment.Strategy.Steps)) - 1
			achievedStepName = rel.Spec.Environment.Strategy.Steps[achievedStep].Name
		}

		if prevStep == nil || achievedStep != prevStep.Step {
			rel.Status.AchievedStep = &shipper.AchievedStep{
				Step: achievedStep,
				Name: achievedStepName,
			}
			c.recorder.Eventf(
				rel,
				corev1.EventTypeNormal,
				"StrategyApplied",
				"step [%d] finished",
				achievedStep,
			)
		}

		if isLastStep {
			condition := releaseutil.NewReleaseCondition(
				shipper.ReleaseConditionTypeComplete,
				corev1.ConditionTrue,
				"",
				"",
			)
			diff.Append(releaseutil.SetReleaseCondition(&rel.Status, *condition))
		}
	}

	return rel, nil
}

func (c *Controller) executeReleaseStrategyForCluster(
	rel *shipper.Release,
	prev, succ *shipper.Release,
	appClusterClientset shipperclientset.Interface,
	executor *StrategyExecutor,
	listers listers,
) (conditions.StrategyConditionsMap, error) {
	var err error
	var relinfoPrev, relinfoSucc *releaseInfo

	scheduler := NewScheduler(
		appClusterClientset,
		listers,
		c.chartFetcher,
		c.recorder,
	)

	relinfo, err := scheduler.ScheduleRelease(rel)
	if err != nil {
		return nil, err
	}

	if prev != nil {
		relinfoPrev, err = c.buildReleaseInfo(prev, listers)
		if err != nil {
			return nil, err
		}
	}

	if succ != nil {
		relinfoSucc, err = c.buildReleaseInfo(succ, listers)
		if err != nil {
			return nil, err
		}
	}

	conditions, patches := executor.Execute(relinfoPrev, relinfo, relinfoSucc)

	for _, patch := range patches {
		namespace := relinfo.release.Namespace
		name, gvk, b := patch.PatchSpec()
		shipperv1alpha1 := appClusterClientset.ShipperV1alpha1()

		var err error
		switch gvk.Kind {
		case "CapacityTarget":
			_, err = shipperv1alpha1.CapacityTargets(namespace).Patch(name, types.MergePatchType, b)
		case "TrafficTarget":
			_, err = shipperv1alpha1.TrafficTargets(namespace).Patch(name, types.MergePatchType, b)
		default:
			err = fmt.Errorf(
				"invalid strategy patch. shipper doesn't know how to patch GVK %s",
				gvk.Kind)
			return nil, shippererrors.NewUnrecoverableError(err)
		}

		if err != nil {
			return nil, shippererrors.
				NewKubeclientPatchError(namespace, name, err).
				WithKind(gvk)
		}
	}

	return conditions, nil
}

func (c *Controller) chooseClusters(rel *shipper.Release) (*shipper.Release, []string, error) {
	releaseClusters := releaseutil.GetSelectedClusters(rel)
	if releaseClusters != nil {
		return rel, releaseClusters, nil
	}

	selector := labels.Everything()
	allClusters, err := c.clusterLister.List(selector)
	if err != nil {
		return rel, nil, shippererrors.NewKubeclientListError(
			shipper.SchemeGroupVersion.WithKind("Cluster"),
			"", selector, err)

	}

	selectedClusters, err := computeTargetClusters(rel, allClusters)
	if err != nil {
		return rel, nil, err
	}

	setReleaseClusters(rel, selectedClusters)

	return rel, releaseutil.GetSelectedClusters(rel), nil
}

// buildReleaseInfo returns a release and it's associated objects fetched from
// the lister interface. If some of them could not be found, it returns a
// corresponding error.
func (c *Controller) buildReleaseInfo(
	rel *shipper.Release,
	listers listers,
) (*releaseInfo, error) {
	ns := rel.Namespace
	name := rel.Name

	installationTarget, err := listers.installationTargetLister.InstallationTargets(ns).Get(name)
	if err != nil {
		return nil, shippererrors.NewKubeclientGetError(ns, name, err).
			WithShipperKind("InstallationTarget")
	}

	capacityTarget, err := listers.capacityTargetLister.CapacityTargets(ns).Get(name)
	if err != nil {
		return nil, shippererrors.NewKubeclientGetError(ns, name, err).
			WithShipperKind("CapacityTarget")
	}

	trafficTarget, err := listers.trafficTargetLister.TrafficTargets(ns).Get(name)
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

func (c *Controller) applicationReleases(rel *shipper.Release) ([]*shipper.Release, error) {
	appName, err := objectutil.GetApplicationLabel(rel)
	if err != nil {
		return nil, err
	}

	releases, err := c.releaseLister.Releases(rel.Namespace).ReleasesForApplication(appName)
	if err != nil {
		return nil, err
	}

	return releases, nil
}

func (c *Controller) enqueueReleaseAndNeighbours(obj interface{}) {
	rel, ok := obj.(*shipper.Release)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.Release: %#v", obj))
		return
	}
	if rel == nil {
		return
	}
	c.enqueueRelease(rel)
	releases, err := c.applicationReleases(rel)
	if err != nil {
		runtime.HandleError(fmt.Errorf("failed to list application releases for shipper.Release %#v: %s", rel, err))
		return
	}
	predecessor, ancestor, err := releaseutil.GetSiblingReleases(rel, releases)
	if err != nil {
		runtime.HandleError(err)
		return
	}
	if predecessor != nil {
		c.enqueueRelease(predecessor)
	}
	if ancestor != nil {
		c.enqueueRelease(ancestor)
	}
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

func (c *Controller) enqueueReleaseFromRolloutBlock(obj interface{}) {
	_, ok := obj.(*shipper.RolloutBlock)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a shipper.RolloutBlock: %#v", obj))
		return
	}

	// update condition for all releases, they are not blocked anymore
	releases, err := c.releaseLister.List(labels.Everything())
	if err != nil {
		runtime.HandleError(fmt.Errorf("error fetching releases: %s", err))
		return
	}

	for _, rel := range releases {
		c.enqueueRelease(rel)
	}
}

func (c *Controller) enqueueReleaseFromAssociatedObject(obj interface{}) {
	kubeobj, ok := obj.(metav1.Object)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a metav1.Object: %#v", obj))
		return
	}

	releaseName, err := objectutil.GetReleaseLabel(kubeobj)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	rel, err := c.releaseLister.Releases(kubeobj.GetNamespace()).Get(releaseName)
	if err != nil {
		if !errors.IsNotFound(err) {
			runtime.HandleError(err)
		}
	}

	c.enqueueReleaseAndNeighbours(rel)
}

func (c *Controller) getSiblingReleases(rel *shipper.Release) (*shipper.Release, *shipper.Release, error) {
	releases, err := c.applicationReleases(rel)
	if err != nil {
		return nil, nil, err
	}

	return releaseutil.GetSiblingReleases(rel, releases)
}
