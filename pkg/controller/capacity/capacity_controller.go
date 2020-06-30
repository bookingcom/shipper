package capacity

import (
	"fmt"
	"reflect"
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
	"k8s.io/client-go/kubernetes"
	appslisters "k8s.io/client-go/listers/apps/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperclient "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	informers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	listers "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	diffutil "github.com/bookingcom/shipper/pkg/util/diff"
	"github.com/bookingcom/shipper/pkg/util/filters"
	objectutil "github.com/bookingcom/shipper/pkg/util/object"
	"github.com/bookingcom/shipper/pkg/util/replicas"
	targetutil "github.com/bookingcom/shipper/pkg/util/target"
	shipperworkqueue "github.com/bookingcom/shipper/pkg/workqueue"
)

const (
	AgentName   = "capacity-controller"
	SadPodLimit = 5

	InProgress      = "InProgress"
	InternalError   = "InternalError"
	PodsNotReady    = "PodsNotReady"
	DeploymentStuck = "DeploymentStuck"

	CapacityTargetConditionChanged = "CapacityTargetConditionChanged"
)

// Controller is the controller implementation for CapacityTarget resources
type Controller struct {
	shipperClient shipperclient.Interface
	kubeClient    kubernetes.Interface

	capacityTargetsLister listers.CapacityTargetLister
	capacityTargetsSynced cache.InformerSynced

	deploymentsLister appslisters.DeploymentLister
	deploymentsSynced cache.InformerSynced

	podsLister corelisters.PodLister
	podsSynced cache.InformerSynced

	workqueue workqueue.RateLimitingInterface

	recorder record.EventRecorder
}

// NewController returns a new CapacityTarget controller.
func NewController(
	kubeClient kubernetes.Interface,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	shipperClient shipperclient.Interface,
	shipperInformerFactory informers.SharedInformerFactory,
	recorder record.EventRecorder,
) *Controller {
	capacityTargetInformer := shipperInformerFactory.Shipper().V1alpha1().CapacityTargets()
	deploymentsInformer := kubeInformerFactory.Apps().V1().Deployments()
	podsInformer := kubeInformerFactory.Core().V1().Pods()

	controller := &Controller{
		shipperClient: shipperClient,
		kubeClient:    kubeClient,

		capacityTargetsLister: capacityTargetInformer.Lister(),
		capacityTargetsSynced: capacityTargetInformer.Informer().HasSynced,

		deploymentsLister: deploymentsInformer.Lister(),
		deploymentsSynced: deploymentsInformer.Informer().HasSynced,

		podsLister: podsInformer.Lister(),
		podsSynced: podsInformer.Informer().HasSynced,

		workqueue: workqueue.NewNamedRateLimitingQueue(
			shipperworkqueue.NewDefaultControllerRateLimiter(),
			"capacity_controller_capacitytargets",
		),

		recorder: recorder,
	}

	capacityTargetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueCapacityTarget,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueCapacityTarget(new)
		},
	})

	deploymentsInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: filters.BelongsToRelease,
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    controller.enqueueCapacityTargetFromDeployment,
			DeleteFunc: controller.enqueueCapacityTargetFromDeployment,
			UpdateFunc: func(oldObj, newObj interface{}) {
				controller.enqueueCapacityTargetFromDeployment(newObj)
			},
		},
	})

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

	if !cache.WaitForCacheSync(
		stopCh,
		c.capacityTargetsSynced,
		c.deploymentsSynced,
		c.podsSynced,
	) {
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
		_, err := c.shipperClient.ShipperV1alpha1().CapacityTargets(namespace).
			UpdateStatus(ct)
		if err != nil {
			return shippererrors.NewKubeclientUpdateError(ct, err).
				WithShipperKind("CapacityTarget")
		}
	}

	return err
}

func (c *Controller) processCapacityTarget(ct *shipper.CapacityTarget) (*shipper.CapacityTarget, error) {
	diff := diffutil.NewMultiDiff()
	operationalCond := targetutil.NewTargetCondition(
		shipper.TargetConditionTypeOperational,
		corev1.ConditionUnknown,
		"",
		"")
	readyCond := targetutil.NewTargetCondition(
		shipper.TargetConditionTypeReady,
		corev1.ConditionUnknown,
		"",
		"")

	var (
		availableReplicas int32
		sadPods           []shipper.PodStatus
	)

	defer func() {
		var d diffutil.Diff

		ct.Status.Conditions, d = targetutil.SetTargetCondition(ct.Status.Conditions, operationalCond)
		diff.Append(d)

		ct.Status.Conditions, d = targetutil.SetTargetCondition(ct.Status.Conditions, readyCond)
		diff.Append(d)

		ct.Status.ObservedGeneration = ct.Generation
		ct.Status.SadPods = sadPods
		ct.Status.AvailableReplicas = availableReplicas
		ct.Status.AchievedPercent = c.calculatePercentageFromAmount(
			ct.Spec.TotalReplicaCount, availableReplicas)

		if !diff.IsEmpty() {
			c.recorder.Event(ct, corev1.EventTypeNormal, CapacityTargetConditionChanged, diff.String())
		}
	}()

	deployment, pods, err := c.getClusterObjects(ct)
	if err != nil {
		operationalCond = targetutil.NewTargetCondition(
			shipper.TargetConditionTypeOperational,
			corev1.ConditionFalse,
			InternalError,
			err.Error())

		return ct, err
	}

	operationalCond = targetutil.NewTargetCondition(
		shipper.TargetConditionTypeOperational,
		corev1.ConditionTrue,
		"",
		"")

	// availableReplicas will be used by the defer at the top of this func
	availableReplicas = deployment.Status.AvailableReplicas

	desiredReplicas := int32(replicas.CalculateDesiredReplicaCount(uint(ct.Spec.TotalReplicaCount), float64(ct.Spec.Percent)))
	if deployment.Spec.Replicas == nil || desiredReplicas != *deployment.Spec.Replicas {
		_, err = c.patchDeploymentWithReplicaCount(deployment, desiredReplicas)
		if err != nil {
			readyCond = targetutil.NewTargetCondition(
				shipper.TargetConditionTypeReady,
				corev1.ConditionFalse,
				InternalError,
				err.Error(),
			)

			return ct, err
		} else {
			readyCond = targetutil.NewTargetCondition(
				shipper.TargetConditionTypeReady,
				corev1.ConditionFalse,
				InProgress,
				"",
			)

			return ct, shippererrors.NewCapacityInProgressError(ct.Name)
		}
	}

	// Deployment was successfully updated, but the update hasn't been
	// observed by the deployment controller yet, so our change is still in
	// flight, and we can't trust the status yet.
	if deployment.Generation > deployment.Status.ObservedGeneration {
		readyCond = targetutil.NewTargetCondition(
			shipper.TargetConditionTypeReady,
			corev1.ConditionFalse,
			InProgress,
			"",
		)

		return ct, shippererrors.NewCapacityInProgressError(ct.Name)
	}

	// If the number of available replicas matches what we want, the
	// CapacityTarget is Ready and there's nothing left to check.
	if replicas.AchievedDesiredReplicaPercentage(ct.Spec.TotalReplicaCount, availableReplicas, ct.Spec.Percent) {
		readyCond = targetutil.NewTargetCondition(
			shipper.TargetConditionTypeReady,
			corev1.ConditionTrue,
			"",
			"",
		)

		return ct, nil
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

	readyCond = targetutil.NewTargetCondition(
		shipper.TargetConditionTypeReady,
		corev1.ConditionFalse,
		reason,
		msg,
	)

	if reason == InProgress {
		return ct, shippererrors.NewCapacityInProgressError(ct.Name)
	}

	return ct, nil
}

func (c *Controller) enqueueCapacityTarget(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	c.workqueue.Add(key)
}

func (c Controller) getClusterObjects(ct *shipper.CapacityTarget) (*appsv1.Deployment, []*corev1.Pod, error) {
	appName, err := objectutil.GetApplicationLabel(ct)
	if err != nil {
		return nil, nil, err
	}

	releaseName, err := objectutil.GetReleaseLabel(ct)
	if err != nil {
		return nil, nil, err
	}

	deploymentSelector := labels.Set{
		shipper.AppLabel:     appName,
		shipper.ReleaseLabel: releaseName,
	}.AsSelector()
	deploymentGVK := corev1.SchemeGroupVersion.WithKind("Deployment")
	deployments, err := c.deploymentsLister.
		Deployments(ct.Namespace).List(deploymentSelector)
	if err != nil {
		return nil, nil, shippererrors.NewKubeclientListError(
			deploymentGVK, ct.Namespace, deploymentSelector, err)
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

	pods, err := c.podsLister.
		Pods(deployment.Namespace).List(podSelector)
	if err != nil {
		return nil, nil, shippererrors.NewKubeclientListError(
			corev1.SchemeGroupVersion.WithKind("Pod"),
			deployment.Namespace, podSelector, err)
	}

	return deployment, pods, nil
}

func (c *Controller) patchDeploymentWithReplicaCount(
	deployment *appsv1.Deployment,
	replicaCount int32,
) (*appsv1.Deployment, error) {
	patch := []byte(fmt.Sprintf(`{"spec": {"replicas": %d}}`, replicaCount))

	updatedDeployment, err := c.kubeClient.AppsV1().
		Deployments(deployment.Namespace).
		Patch(deployment.Name, types.StrategicMergePatchType, patch)
	if err != nil {
		return nil, shippererrors.NewKubeclientUpdateError(deployment, err)
	}

	return updatedDeployment, nil
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
