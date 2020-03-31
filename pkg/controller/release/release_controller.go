package release

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/labels"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperrepo "github.com/bookingcom/shipper/pkg/chart/repo"
	shipperclient "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shipperlisters "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/clusterclientstore"
	"github.com/bookingcom/shipper/pkg/controller"
	shippercontroller "github.com/bookingcom/shipper/pkg/controller"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	diffutil "github.com/bookingcom/shipper/pkg/util/diff"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
	rolloutblock "github.com/bookingcom/shipper/pkg/util/rolloutblock"
	shipperworkqueue "github.com/bookingcom/shipper/pkg/workqueue"
)

const (
	AgentName = "release-controller"

	ClustersNotReady        = "ClustersNotReady"
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

	installationTargetLister  shipperlisters.InstallationTargetLister
	installationTargetsSynced cache.InformerSynced

	trafficTargetLister  shipperlisters.TrafficTargetLister
	trafficTargetsSynced cache.InformerSynced

	capacityTargetLister  shipperlisters.CapacityTargetLister
	capacityTargetsSynced cache.InformerSynced

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

type ReleaseStrategyStateTransition struct {
	State    string
	Previous shipper.StrategyState
	New      shipper.StrategyState
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
	installationTargetInformer := informerFactory.Shipper().V1alpha1().InstallationTargets()
	trafficTargetInformer := informerFactory.Shipper().V1alpha1().TrafficTargets()
	capacityTargetInformer := informerFactory.Shipper().V1alpha1().CapacityTargets()
	rolloutBlockInformer := informerFactory.Shipper().V1alpha1().RolloutBlocks()

	klog.Info("Building a release controller")

	controller := &Controller{
		clientset: clientset,
		store:     store,

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

		workqueue: workqueue.NewNamedRateLimitingQueue(
			shipperworkqueue.NewDefaultControllerRateLimiter(),
			"release_controller_releases",
		),

		chartFetcher: chartFetcher,

		recorder: recorder,
	}

	klog.Info("Setting up event handlers")

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

	installationTargetInformer.Informer().AddEventHandler(eventHandler)
	capacityTargetInformer.Informer().AddEventHandler(eventHandler)
	trafficTargetInformer.Informer().AddEventHandler(eventHandler)

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
	}

	klog.V(4).Info("Started Release controller")

	<-stopCh
}

func (c *Controller) runReleaseWorker() {
	for c.processNextWorkItem() {
	}
}

// processNextReleaseWorkItem pops an element from the head of the workqueue and
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
	err := c.syncOneReleaseHandler(key)

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

// syncOneReleaseHandler processes release keys one-by-one. This stage progresses
// the release through a scheduler: assigns a set of chosen clusters, creates
// required associated objects and marks the release as scheduled.
func (c *Controller) syncOneReleaseHandler(key string) error {
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

	rel, patches, err := c.processRelease(initialRel.DeepCopy())

	if !equality.Semantic.DeepEqual(initialRel, rel) {
		_, err := c.clientset.ShipperV1alpha1().Releases(namespace).
			Update(rel)
		if err != nil {
			return shippererrors.NewKubeclientUpdateError(rel, err).
				WithShipperKind("Release")
		}
	}

	for _, patch := range patches {
		if err := c.applyPatch(namespace, patch); err != nil {
			return err
		}
	}

	return err
}

func (c *Controller) processRelease(rel *shipper.Release) (*shipper.Release, []StrategyPatch, error) {
	diff := diffutil.NewMultiDiff()
	defer func() {
		if !diff.IsEmpty() {
			c.recorder.Event(rel, corev1.EventTypeNormal, "ReleaseConditionChanged", diff.String())
		}
	}()

	scheduler := NewScheduler(
		c.clientset,
		c.installationTargetLister,
		c.capacityTargetLister,
		c.trafficTargetLister,
		c.chartFetcher,
		c.recorder,
	)

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

		return rel, nil, err
	}

	condition := releaseutil.NewReleaseCondition(
		shipper.ReleaseConditionTypeBlocked,
		corev1.ConditionFalse,
		"",
		"",
	)
	diff.Append(releaseutil.SetReleaseCondition(&rel.Status, *condition))

	clusterAnnotation, ok := rel.Annotations[shipper.ReleaseClustersAnnotation]
	if !ok || len(clusterAnnotation) == 0 {
		selector := labels.Everything()
		allClusters, err := c.clusterLister.List(selector)
		if err != nil {
			err = shippererrors.NewKubeclientListError(
				shipper.SchemeGroupVersion.WithKind("Cluster"),
				"", selector, err)

			reason := reasonForReleaseCondition(err)
			condition := releaseutil.NewReleaseCondition(
				shipper.ReleaseConditionTypeScheduled,
				corev1.ConditionFalse,
				reason,
				err.Error(),
			)
			diff.Append(releaseutil.SetReleaseCondition(&rel.Status, *condition))

			return rel, nil, err
		}

		selectedClusters, err := computeTargetClusters(rel, allClusters)
		if err != nil {
			reason := reasonForReleaseCondition(err)
			condition := releaseutil.NewReleaseCondition(
				shipper.ReleaseConditionTypeScheduled,
				corev1.ConditionFalse,
				reason,
				err.Error(),
			)
			diff.Append(releaseutil.SetReleaseCondition(&rel.Status, *condition))

			return rel, nil, err
		}

		setReleaseClusters(rel, selectedClusters)

		c.recorder.Eventf(
			rel,
			corev1.EventTypeNormal,
			"ClustersSelected",
			"Set clusters for %q to %v",
			shippercontroller.MetaKey(rel),
			selectedClusters,
		)
	}

	klog.V(4).Infof("Scheduling release %q", controller.MetaKey(rel))
	relinfo, err := scheduler.ScheduleRelease(rel.DeepCopy())
	if err != nil {
		reason := reasonForReleaseCondition(err)
		condition := releaseutil.NewReleaseCondition(
			shipper.ReleaseConditionTypeScheduled,
			corev1.ConditionFalse,
			reason,
			err.Error(),
		)
		diff.Append(releaseutil.SetReleaseCondition(&rel.Status, *condition))

		return rel, nil, err
	}

	rel = relinfo.release

	condition = releaseutil.NewReleaseCondition(
		shipper.ReleaseConditionTypeScheduled,
		corev1.ConditionTrue,
		"",
		"",
	)
	diff.Append(releaseutil.SetReleaseCondition(&rel.Status, *condition))

	execRel, patches, err := c.executeReleaseStrategy(relinfo, diff)
	if err != nil {
		releaseStrategyExecutedCond := releaseutil.NewReleaseCondition(
			shipper.ReleaseConditionTypeStrategyExecuted,
			corev1.ConditionFalse,
			StrategyExecutionFailed,
			err.Error(),
		)
		diff.Append(releaseutil.SetReleaseCondition(&rel.Status, *releaseStrategyExecutedCond))

		return rel, patches, err
	}

	return execRel, patches, nil
}

func (c *Controller) executeReleaseStrategy(relinfo *releaseInfo, diff *diffutil.MultiDiff) (*shipper.Release, []StrategyPatch, error) {
	rel := relinfo.release.DeepCopy()

	releases, err := c.applicationReleases(rel)
	if err != nil {
		return nil, nil, err
	}
	prev, succ, err := releaseutil.GetSiblingReleases(rel, releases)
	if err != nil {
		return nil, nil, err
	}

	var relinfoPrev, relinfoSucc *releaseInfo
	if prev != nil {
		relinfoPrev, err = c.buildReleaseInfo(prev)
		if err != nil {
			return nil, nil, err
		}
	}
	if succ != nil {
		// If there is a successor to the current release, we have to ensure
		// it's spec points to the last strategy step.
		if !releaseutil.IsLastStrategyStep(relinfo.release) {
			// In practice, this situation most likely means an
			// external modification to the current release object,
			// which can potentially cause some harmful consequences
			// like: a historical release gets activated.
			return nil, nil, shippererrors.NewInconsistentReleaseTargetStep(
				controller.MetaKey(relinfo.release),
				relinfo.release.Spec.TargetStep,
				int32(len(relinfo.release.Spec.Environment.Strategy.Steps)-1),
			)
		}

		relinfoSucc, err = c.buildReleaseInfo(succ)
		if err != nil {
			return nil, nil, err
		}
	}

	isHead := succ == nil
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

	// Looks like a malformed input. Informing about a problem and bailing out.
	if targetStep >= int32(len(strategy.Steps)) {
		err := fmt.Errorf("no step %d in strategy for Release %q",
			targetStep, controller.MetaKey(rel))
		return nil, nil, shippererrors.NewUnrecoverableError(err)
	}

	executor := NewStrategyExecutor(strategy, targetStep)

	complete, patches, trans := executor.Execute(relinfoPrev, relinfo, relinfoSucc)

	if len(patches) == 0 {
		klog.V(4).Infof("Strategy verified for release %q, nothing to patch", controller.MetaKey(rel))
	} else {
		klog.V(4).Infof("Strategy has been executed for release %q, applying patches", controller.MetaKey(rel))
	}

	condition := releaseutil.NewReleaseCondition(
		shipper.ReleaseConditionTypeStrategyExecuted,
		corev1.ConditionTrue,
		"",
		"",
	)
	diff.Append(releaseutil.SetReleaseCondition(&rel.Status, *condition))

	isLastStep := int(targetStep) == len(strategy.Steps)-1
	prevStep := rel.Status.AchievedStep

	if complete {
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

	for _, t := range trans {
		c.recorder.Eventf(
			rel,
			corev1.EventTypeNormal,
			"ReleaseStateTransitioned",
			"Release %q had its state %q transitioned to %q",
			shippercontroller.MetaKey(rel),
			t.State,
			t.New,
		)
	}

	return rel, patches, nil
}

func (c *Controller) applyPatch(namespace string, patch StrategyPatch) error {
	name, gvk, b := patch.PatchSpec()

	var err error
	switch gvk.Kind {
	case "Release":
		_, err = c.clientset.ShipperV1alpha1().Releases(namespace).Patch(name, types.MergePatchType, b)
	case "InstallationTarget":
		_, err = c.clientset.ShipperV1alpha1().InstallationTargets(namespace).Patch(name, types.MergePatchType, b)
	case "CapacityTarget":
		_, err = c.clientset.ShipperV1alpha1().CapacityTargets(namespace).Patch(name, types.MergePatchType, b)
	case "TrafficTarget":
		_, err = c.clientset.ShipperV1alpha1().TrafficTargets(namespace).Patch(name, types.MergePatchType, b)
	default:
		return shippererrors.NewUnrecoverableError(fmt.Errorf("error syncing Release %q (will not retry): unknown GVK resource name: %s", name, gvk.Kind))
	}
	if err != nil {
		return shippererrors.NewKubeclientPatchError(namespace, name, err).WithKind(gvk)
	}

	return nil
}

// getAssociatedReleaseKey returns an owner reference release name for an
// associated object in the format:
// <namespace> / <release name>
func (c *Controller) getAssociatedReleaseName(obj metav1.Object) (string, error) {
	references := obj.GetOwnerReferences()
	if n := len(references); n != 1 {
		return "", shippererrors.NewMultipleOwnerReferencesError(obj.GetName(), n)
	}

	owner := references[0]

	return owner.Name, nil
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

func (c *Controller) applicationReleases(rel *shipper.Release) ([]*shipper.Release, error) {
	appName, err := releaseutil.ApplicationNameForRelease(rel)
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

	releaseName, err := c.getAssociatedReleaseName(kubeobj)
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
	case shippererrors.RolloutBlockError:
		return "RolloutBlock"
	case shippererrors.ChartRepoInternalError:
		return "ChartRepoInternal"
	case shippererrors.NoCachedChartRepoIndexError:
		return "NoCachedChartRepoIndex"
	}

	if shippererrors.IsKubeclientError(err) {
		return "FailedAPICall"
	}

	return fmt.Sprintf("unknown error %T! tell Shipper devs to classify it", err)
}

func setReleaseClusters(rel *shipper.Release, clusters []*shipper.Cluster) {
	clusterNames := make([]string, 0, len(clusters))
	memo := make(map[string]struct{})
	for _, cluster := range clusters {
		if _, ok := memo[cluster.Name]; ok {
			continue
		}
		clusterNames = append(clusterNames, cluster.Name)
		memo[cluster.Name] = struct{}{}
	}
	sort.Strings(clusterNames)
	rel.Annotations[shipper.ReleaseClustersAnnotation] = strings.Join(clusterNames, ",")
}
