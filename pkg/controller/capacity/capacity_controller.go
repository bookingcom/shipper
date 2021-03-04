package capacity

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
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
	clientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	informers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	listers "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/clusterclientstore"
	"github.com/bookingcom/shipper/pkg/controller/capacity/builder"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	capacityutil "github.com/bookingcom/shipper/pkg/util/capacity"
	clusterstatusutil "github.com/bookingcom/shipper/pkg/util/clusterstatus"
	diffutil "github.com/bookingcom/shipper/pkg/util/diff"
	"github.com/bookingcom/shipper/pkg/util/filters"
	"github.com/bookingcom/shipper/pkg/util/replicas"
	targetutil "github.com/bookingcom/shipper/pkg/util/target"
	shipperworkqueue "github.com/bookingcom/shipper/pkg/workqueue"
)

const (
	AgentName   = "capacity-controller"
	SadPodLimit = 5

	ClustersNotReady = "ClustersNotReady"
	InProgress       = "InProgress"
	InternalError    = "InternalError"
	PodsNotReady     = "PodsNotReady"
	DeploymentStuck  = "DeploymentStuck"

	CapacityTargetConditionChanged  = "CapacityTargetConditionChanged"
	ClusterCapacityConditionChanged = "ClusterCapacityConditionChanged"
)

// Controller is the controller implementation for CapacityTarget resources
type Controller struct {
	shipperclientset      clientset.Interface
	clusterClientStore    clusterclientstore.Interface
	capacityTargetsLister listers.CapacityTargetLister
	capacityTargetsSynced cache.InformerSynced
	releasesLister        listers.ReleaseLister
	releasesListerSynced  cache.InformerSynced
	workqueue             workqueue.RateLimitingInterface
	recorder              record.EventRecorder
}

// NewController returns a new CapacityTarget controller.
func NewController(
	shipperclientset clientset.Interface,
	shipperInformerFactory informers.SharedInformerFactory,
	store clusterclientstore.Interface,
	recorder record.EventRecorder,
) *Controller {

	capacityTargetInformer := shipperInformerFactory.Shipper().V1alpha1().CapacityTargets()

	releaseInformer := shipperInformerFactory.Shipper().V1alpha1().Releases()

	controller := &Controller{
		shipperclientset:      shipperclientset,
		capacityTargetsLister: capacityTargetInformer.Lister(),
		capacityTargetsSynced: capacityTargetInformer.Informer().HasSynced,
		releasesLister:        releaseInformer.Lister(),
		releasesListerSynced:  releaseInformer.Informer().HasSynced,
		workqueue:             workqueue.NewNamedRateLimitingQueue(shipperworkqueue.NewDefaultControllerRateLimiter(), "capacity_controller_capacitytargets"),
		recorder:              recorder,
		clusterClientStore:    store,
	}

	klog.Info("Setting up event handlers")
	capacityTargetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueCapacityTarget,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueCapacityTarget(new)
		},
	})

	store.AddSubscriptionCallback(controller.subscribeToDeployments)
	store.AddEventHandlerCallback(controller.registerDeploymentEventHandlers)

	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	klog.V(2).Info("Starting Capacity controller")
	defer klog.V(2).Info("Shutting down Capacity controller")

	if !cache.WaitForCacheSync(stopCh, c.capacityTargetsSynced, c.releasesListerSynced) {
		runtime.HandleError(fmt.Errorf("failed to wait for caches to sync"))
		return
	}

	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	klog.V(4).Info("Started Capacity controller")

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
		runtime.HandleError(fmt.Errorf("invalid object key (will retry: false): %#v", obj))
		return true
	}

	shouldRetry := false
	err := c.capacityTargetSyncHandler(key)

	if err != nil {
		shouldRetry = shippererrors.ShouldRetry(err)
		runtime.HandleError(fmt.Errorf("error syncing CapacityTarget %q (will retry: %t): %s", key, shouldRetry, err.Error()))
	}

	if shouldRetry {
		c.workqueue.AddRateLimited(key)

		return true
	}

	klog.V(4).Infof("Successfully synced CapacityTarget %q", key)
	c.workqueue.Forget(obj)

	return true
}

func (c *Controller) processCapacityTargetOnCluster(
	ct *shipper.CapacityTarget,
	spec *shipper.ClusterCapacityTarget,
	status *shipper.ClusterCapacityStatus,
) error {
	diff := diffutil.NewMultiDiff()
	operationalCond := capacityutil.NewClusterCapacityCondition(
		shipper.ClusterConditionTypeOperational,
		corev1.ConditionUnknown,
		"",
		"")
	readyCond := capacityutil.NewClusterCapacityCondition(
		shipper.ClusterConditionTypeReady,
		corev1.ConditionUnknown,
		"",
		"")

	var (
		availableReplicas int32
		sadPods           []shipper.PodStatus
		reports           []shipper.ClusterCapacityReport
	)

	defer func() {
		status.SadPods = sadPods
		status.Reports = reports
		status.AvailableReplicas = availableReplicas
		status.AchievedPercent = c.calculatePercentageFromAmount(
			spec.TotalReplicaCount, availableReplicas)

		diff.Append(capacityutil.SetClusterCapacityCondition(status, *operationalCond))
		diff.Append(capacityutil.SetClusterCapacityCondition(status, *readyCond))
		c.reportConditionChange(ct, ClusterCapacityConditionChanged, diff)
	}()

	appName := ct.Labels[shipper.AppLabel]
	release := ct.Labels[shipper.ReleaseLabel]
	deployment, pods, err := c.getClusterObjects(spec.Name, ct.Namespace, appName, release)
	if err != nil {
		operationalCond = capacityutil.NewClusterCapacityCondition(
			shipper.ClusterConditionTypeOperational,
			corev1.ConditionFalse,
			InternalError,
			err.Error())

		return err
	}

	operationalCond = capacityutil.NewClusterCapacityCondition(
		shipper.ClusterConditionTypeOperational,
		corev1.ConditionTrue,
		"",
		"")

	report := buildReport(deployment.Name, pods)

	// availableReplicas and reports will be used by the defer at the top
	// of this func
	availableReplicas = deployment.Status.AvailableReplicas
	reports = []shipper.ClusterCapacityReport{*report}

	desiredReplicas := int32(replicas.CalculateDesiredReplicaCount(uint(spec.TotalReplicaCount), float64(spec.Percent)))
	if deployment.Spec.Replicas == nil || desiredReplicas != *deployment.Spec.Replicas {
		_, err = c.patchDeploymentWithReplicaCount(deployment, spec.Name, desiredReplicas)
		if err != nil {
			readyCond = capacityutil.NewClusterCapacityCondition(
				shipper.ClusterConditionTypeReady,
				corev1.ConditionFalse,
				InternalError,
				err.Error(),
			)
			return err
		} else {
			readyCond = capacityutil.NewClusterCapacityCondition(
				shipper.ClusterConditionTypeReady,
				corev1.ConditionFalse,
				InProgress,
				"",
			)
			return shippererrors.NewCapacityInProgressError(ct.Name)
		}
	}

	// Deployment was successfully updated, but the update hasn't been
	// observed by the deployment controller yet, so our change is still in
	// flight, and we can't trust the status yet.
	if deployment.Generation > deployment.Status.ObservedGeneration {
		readyCond = capacityutil.NewClusterCapacityCondition(
			shipper.ClusterConditionTypeReady,
			corev1.ConditionFalse,
			InProgress,
			"",
		)

		return shippererrors.NewCapacityInProgressError(ct.Name)
	}

	// If the number of available replicas matches what we want, the
	// CapacityTarget is Ready and there's nothing left to check.
	if replicas.AchievedDesiredReplicaPercentage(spec.TotalReplicaCount, availableReplicas, spec.Percent) {
		readyCond = capacityutil.NewClusterCapacityCondition(
			shipper.ClusterConditionTypeReady,
			corev1.ConditionTrue,
			"",
			"",
		)

		return nil
	}

	// Not all pods are availble, so we know for sure this cluster isn't
	// ready. From here on out we just try to figure out why to give users
	// a good place to start looking.

	// sadPods will be used by the defer at the top of this func
	sadPods = c.getSadPods(pods)
	if len(sadPods) > SadPodLimit {
		sadPods = sadPods[:SadPodLimit]
	}

	replicaFailureCond := getDeploymentCondition(deployment.Status, appsv1.DeploymentReplicaFailure)
	progressingCond := getDeploymentCondition(deployment.Status, appsv1.DeploymentProgressing)

	var msg, reason string

	if replicaFailureCond != nil && replicaFailureCond.Status == corev1.ConditionTrue {
		// It is common for a Deployment to get stuck because of exceeded
		// quotas. Looking at the ReplicaFailure condition exposes that
		// condition, and potentially others too.
		reason = DeploymentStuck
		msg = replicaFailureCond.Message
	} else if progressingCond != nil && progressingCond.Status == corev1.ConditionFalse {
		// If the Deployment has a timeout defined, and exceeds it,
		// Progressing becomes False. Note that True doesn't *actually*
		// mean the rollout is still progressing, for our definition of
		// progressing.
		reason = DeploymentStuck
		msg = progressingCond.Message
	} else if l := len(sadPods); l > 0 {
		// We ran out of conditions to look at, but we have pods that
		// aren't Ready, so that's one reason to be concerned.
		summary := summarizeSadPods(sadPods)
		reason = PodsNotReady
		msg = fmt.Sprintf(
			"%d/%d: %s",
			l, desiredReplicas, summary,
		)
	} else {
		// None of the existing pods are non-Ready, and we presumably
		// didn't hit quota yet, so we're most likely still in
		// progress.
		reason = InProgress
	}

	readyCond = capacityutil.NewClusterCapacityCondition(
		shipper.ClusterConditionTypeReady,
		corev1.ConditionFalse,
		reason,
		msg,
	)

	if reason == InProgress {
		return shippererrors.NewCapacityInProgressError(ct.Name)
	}

	return nil
}

func (c *Controller) capacityTargetSyncHandler(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return shippererrors.NewUnrecoverableError(err)
	}

	initialCT, err := c.capacityTargetsLister.CapacityTargets(namespace).Get(name)
	if err != nil {
		if kerrors.IsNotFound(err) {
			klog.V(3).Infof("CapacityTarget %q has been deleted", key)
			return nil
		}

		return shippererrors.NewKubeclientGetError(namespace, name, err).
			WithShipperKind("CapacityTarget")
	}

	ct, err := c.processCapacityTarget(initialCT.DeepCopy())

	if !reflect.DeepEqual(initialCT, ct) {
		_, err := c.shipperclientset.ShipperV1alpha1().CapacityTargets(namespace).
			UpdateStatus(context.TODO(), ct, metav1.UpdateOptions{})
		if err != nil {
			return shippererrors.NewKubeclientUpdateError(ct, err).
				WithShipperKind("CapacityTarget")
		}
	}

	return err
}

func (c *Controller) processCapacityTarget(ct *shipper.CapacityTarget) (*shipper.CapacityTarget, error) {
	diff := diffutil.NewMultiDiff()
	defer c.reportConditionChange(ct, CapacityTargetConditionChanged, diff)

	// CapacityTarget is always Operational at the top level because it
	// doesn't depend on anything.
	ct.Status.Conditions = targetutil.TransitionToOperational(diff, ct.Status.Conditions)

	clusterErrors := shippererrors.NewMultiError()
	newClusterStatuses := make([]shipper.ClusterCapacityStatus, 0, len(ct.Spec.Clusters))

	// This algorithm assumes cluster names are unique
	curClusterStatuses := make(map[string]shipper.ClusterCapacityStatus)
	for _, clusterStatus := range ct.Status.Clusters {
		curClusterStatuses[clusterStatus.Name] = clusterStatus
	}

	for _, clusterSpec := range ct.Spec.Clusters {
		clusterStatus, ok := curClusterStatuses[clusterSpec.Name]
		if !ok {
			clusterStatus = shipper.ClusterCapacityStatus{
				Name: clusterSpec.Name,
			}
		}

		err := c.processCapacityTargetOnCluster(ct, &clusterSpec, &clusterStatus)
		if err != nil {
			clusterErrors.Append(err)
		}

		newClusterStatuses = append(newClusterStatuses, clusterStatus)
	}

	sort.Sort(byClusterName(newClusterStatuses))

	ct.Status.Clusters = newClusterStatuses
	ct.Status.ObservedGeneration = ct.Generation

	notReadyReasons := []string{}
	for _, clusterStatus := range ct.Status.Clusters {
		ready, reason := clusterstatusutil.IsClusterCapacityReady(clusterStatus.Conditions)
		if !ready {
			notReadyReasons = append(notReadyReasons,
				fmt.Sprintf("%s: %s", clusterStatus.Name, reason))
		}
	}

	if len(notReadyReasons) == 0 {
		ct.Status.Conditions = targetutil.TransitionToReady(diff, ct.Status.Conditions)
	} else {
		ct.Status.Conditions = targetutil.TransitionToNotReady(
			diff, ct.Status.Conditions,
			ClustersNotReady, strings.Join(notReadyReasons, "; "))
	}

	return ct, clusterErrors.Flatten()
}

func (c *Controller) enqueueCapacityTarget(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	c.workqueue.Add(key)
}

func (c *Controller) registerDeploymentEventHandlers(informerFactory kubeinformers.SharedInformerFactory, clusterName string) {
	handler := cache.FilteringResourceEventHandler{
		FilterFunc: filters.BelongsToRelease,
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    c.enqueueCapacityTargetFromDeployment,
			DeleteFunc: c.enqueueCapacityTargetFromDeployment,
			UpdateFunc: func(oldObj, newObj interface{}) {
				c.enqueueCapacityTargetFromDeployment(newObj)
			},
		},
	}
	informerFactory.Apps().V1().Deployments().Informer().AddEventHandler(handler)
}

func (c *Controller) subscribeToDeployments(informerFactory kubeinformers.SharedInformerFactory) {
	informerFactory.Apps().V1().Deployments().Informer()
	informerFactory.Core().V1().Pods().Informer()
}

func (c Controller) getClusterObjects(cluster, ns, appName, release string) (*appsv1.Deployment, []*corev1.Pod, error) {
	informerFactory, err := c.clusterClientStore.GetInformerFactory(cluster)
	if err != nil {
		return nil, nil, err
	}

	deploymentSelector := labels.Set{
		shipper.AppLabel:     appName,
		shipper.ReleaseLabel: release,
	}.AsSelector()
	deploymentGVK := corev1.SchemeGroupVersion.WithKind("Deployment")
	deployments, err := informerFactory.Apps().V1().Deployments().
		Lister().Deployments(ns).List(deploymentSelector)
	if err != nil {
		return nil, nil, shippererrors.NewKubeclientListError(
			deploymentGVK, ns, deploymentSelector, err)
	}

	if l := len(deployments); l != 1 {
		return nil, nil, shippererrors.NewUnexpectedObjectCountFromSelectorError(
			deploymentSelector, deploymentGVK, 1, l)
	}

	deployment := deployments[0]

	podSelector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil {
		return nil, nil, shippererrors.NewUnrecoverableError(fmt.Errorf("failed to transform label selector %v into a selector: %s", deployment.Spec.Selector, err))
	}

	pods, err := informerFactory.Core().V1().Pods().Lister().
		Pods(deployment.Namespace).List(podSelector)
	if err != nil {
		return nil, nil, shippererrors.NewKubeclientListError(
			corev1.SchemeGroupVersion.WithKind("Pod"),
			deployment.Namespace, podSelector, err)
	}

	return deployment, pods, nil
}

func (c *Controller) patchDeploymentWithReplicaCount(deployment *appsv1.Deployment, clusterName string, replicaCount int32) (*appsv1.Deployment, error) {
	targetClusterClient, err := c.clusterClientStore.GetClient(clusterName, AgentName)
	if err != nil {
		return nil, err
	}

	patch := []byte(fmt.Sprintf(`{"spec": {"replicas": %d}}`, replicaCount))

	updatedDeployment, err := targetClusterClient.AppsV1().
		Deployments(deployment.Namespace).
		Patch(context.TODO(), deployment.Name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return nil, shippererrors.NewKubeclientUpdateError(deployment, err)
	}

	return updatedDeployment, nil
}

func (c *Controller) reportConditionChange(ct *shipper.CapacityTarget, reason string, diff diffutil.Diff) {
	if !diff.IsEmpty() {
		c.recorder.Event(ct, corev1.EventTypeNormal, reason, diff.String())
	}
}

func buildReport(ownerName string, podsList []*corev1.Pod) *shipper.ClusterCapacityReport {
	sort.Slice(podsList, func(i, j int) bool {
		return podsList[i].Name < podsList[j].Name
	})

	reportBuilder := builder.NewReport(ownerName)

	for _, pod := range podsList {
		reportBuilder.AddPod(pod)
	}

	return reportBuilder.Build()
}

func getDeploymentCondition(
	status appsv1.DeploymentStatus,
	condType appsv1.DeploymentConditionType,
) *appsv1.DeploymentCondition {
	for _, cond := range status.Conditions {
		if cond.Type == condType {
			return &cond
		}
	}

	return nil
}
