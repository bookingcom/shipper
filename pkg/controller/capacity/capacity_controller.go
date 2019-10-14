package capacity

import (
	"fmt"
	"sort"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
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
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	capacityutil "github.com/bookingcom/shipper/pkg/util/capacity"
	diffutil "github.com/bookingcom/shipper/pkg/util/diff"
	"github.com/bookingcom/shipper/pkg/util/filters"
	"github.com/bookingcom/shipper/pkg/util/replicas"
	shipperworkqueue "github.com/bookingcom/shipper/pkg/workqueue"
)

const (
	AgentName   = "capacity-controller"
	SadPodLimit = 5
)

const (
	ServerError       = "ServerError"
	WrongPodCount     = "WrongPodCount"
	PodsNotReady      = "PodsNotReady"
	MissingDeployment = "MissingDeployment"
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
		go wait.Until(c.runCapacityTargetWorker, time.Second, stopCh)
	}

	klog.V(4).Info("Started Capacity controller")

	<-stopCh
}

func (c *Controller) runCapacityTargetWorker() {
	for c.processNextCapacityTargetWorkItem() {
	}
}

func (c *Controller) processNextCapacityTargetWorkItem() bool {
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

func (c *Controller) processCapacityTargetOnCluster(ct *shipper.CapacityTarget, spec *shipper.ClusterCapacityTarget, status *shipper.ClusterCapacityStatus) error {
	selector := labels.Set(ct.Labels).AsSelector()

	targetDeployment, err := c.findTargetDeploymentForClusterSpec(ct, *spec, ct.Namespace, selector, status)
	if err != nil {
		return err
	}

	var availableReplicas, achievedPercent int32
	defer func() {
		status.AchievedPercent = achievedPercent
		status.AvailableReplicas = availableReplicas
	}()

	diff := diffutil.NewMultiDiff()
	defer func() {
		c.reportClusterCapacityConditionChange(ct, diff)
	}()

	if targetDeployment == nil {
		return nil
	}

	rCnt := int32(replicas.CalculateDesiredReplicaCount(uint(spec.TotalReplicaCount), float64(spec.Percent)))

	if targetDeployment.Spec.Replicas == nil || rCnt != *targetDeployment.Spec.Replicas {
		_, err = c.patchDeploymentWithReplicaCount(ct, targetDeployment, spec.Name, rCnt, status)
		if err != nil {
			return err
		}
	}

	availableReplicas = targetDeployment.Status.AvailableReplicas
	achievedPercent = c.calculatePercentageFromAmount(
		spec.TotalReplicaCount,
		availableReplicas,
	)

	report, err := c.getReport(targetDeployment, status)
	if err != nil {
		return err
	}
	status.Reports = []shipper.ClusterCapacityReport{*report}

	podCount, sadPodCount, sadPods, err := c.getSadPodsForDeploymentOnCluster(targetDeployment, status.Name)
	if err != nil {
		cond := capacityutil.NewClusterCapacityCondition(
			shipper.ClusterConditionTypeOperational,
			corev1.ConditionFalse,
			ServerError,
			err.Error(),
		)
		diff.Append(capacityutil.SetClusterCapacityCondition(status, *cond))
		return err
	}

	if targetDeployment.Spec.Replicas == nil || int(*targetDeployment.Spec.Replicas) != podCount {
		cond := capacityutil.NewClusterCapacityCondition(
			shipper.ClusterConditionTypeReady,
			corev1.ConditionFalse,
			WrongPodCount,
			fmt.Sprintf("expected %d replicas but have %d", *targetDeployment.Spec.Replicas, podCount),
		)
		diff.Append(capacityutil.SetClusterCapacityCondition(status, *cond))
		return err
	}

	if sadPodCount > 0 {
		cond := capacityutil.NewClusterCapacityCondition(
			shipper.ClusterConditionTypeReady,
			corev1.ConditionFalse,
			PodsNotReady,
			fmt.Sprintf("there are %d sad pods", sadPodCount),
		)
		diff.Append(capacityutil.SetClusterCapacityCondition(status, *cond))
	}

	status.SadPods = sadPods

	if sadPodCount == 0 {
		condReady := capacityutil.NewClusterCapacityCondition(
			shipper.ClusterConditionTypeReady,
			corev1.ConditionTrue,
			"",
			"")
		diff.Append(capacityutil.SetClusterCapacityCondition(status, *condReady))

		condOperational := capacityutil.NewClusterCapacityCondition(
			shipper.ClusterConditionTypeOperational,
			corev1.ConditionTrue,
			"",
			"")
		diff.Append(capacityutil.SetClusterCapacityCondition(status, *condOperational))
	}

	return nil
}

func (c *Controller) capacityTargetSyncHandler(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return shippererrors.NewUnrecoverableError(err)
	}

	ct, err := c.capacityTargetsLister.CapacityTargets(namespace).Get(name)
	if err != nil {
		if kerrors.IsNotFound(err) {
			klog.V(3).Infof("CapacityTarget %q has been deleted", key)
			return nil
		}

		return shippererrors.NewKubeclientGetError(namespace, name, err).
			WithShipperKind("CapacityTarget")
	}

	ct = ct.DeepCopy()
	clusterErrors := shippererrors.NewMultiError()
	newClusterStatuses := make([]shipper.ClusterCapacityStatus, 0, len(ct.Spec.Clusters))

	// This algorithm assumes cluster names are unique
	curClusterStatuses := make(map[string]shipper.ClusterCapacityStatus)
	for _, clusterStatus := range ct.Status.Clusters {
		curClusterStatuses[clusterStatus.Name] = clusterStatus
	}

	for _, clusterSpec := range ct.Spec.Clusters {
		if _, ok := curClusterStatuses[clusterSpec.Name]; !ok {
			curClusterStatuses[clusterSpec.Name] = shipper.ClusterCapacityStatus{
				Name:    clusterSpec.Name,
				Reports: []shipper.ClusterCapacityReport{},
			}
		}

		clusterStatus := curClusterStatuses[clusterSpec.Name]

		if err := c.processCapacityTargetOnCluster(ct, &clusterSpec, &clusterStatus); err != nil {
			clusterErrors.Append(err)
			continue
		}

		newClusterStatuses = append(newClusterStatuses, clusterStatus)
	}

	if clusterErrors.Any() {
		for _, err := range clusterErrors.Errors {
			if shippererrors.ShouldBroadcast(err) {
				c.recorder.Event(
					ct,
					corev1.EventTypeWarning,
					"FailedCapacityChange",
					err.Error())
			}
		}
	}

	sort.Sort(byClusterName(newClusterStatuses))

	ct.Status.Clusters = newClusterStatuses

	_, err = c.shipperclientset.ShipperV1alpha1().CapacityTargets(namespace).Update(ct)
	if err != nil {
		clusterErrors.Append(shippererrors.NewKubeclientUpdateError(ct, err))
	}

	return clusterErrors.Flatten()
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

func (c *Controller) getReport(targetDeployment *appsv1.Deployment, clusterStatus *shipper.ClusterCapacityStatus) (*shipper.ClusterCapacityReport, error) {
	targetClusterInformer, err := c.clusterClientStore.GetInformerFactory(clusterStatus.Name)
	if err != nil {
		// Not sure if each method should report operational conditions for
		// the cluster it is operating on.
		return nil, err
	}

	selector := labels.Set(targetDeployment.Spec.Template.Labels).AsSelector()
	podsList, err := targetClusterInformer.Core().V1().Pods().Lister().Pods(targetDeployment.Namespace).List(selector)
	if err != nil {
		return nil, shippererrors.NewKubeclientListError(
			corev1.SchemeGroupVersion.WithKind("Pod"),
			targetDeployment.Namespace, selector, err)
	}

	report := buildReport(targetDeployment.Name, podsList)

	return report, nil
}

func (c *Controller) findTargetDeploymentForClusterSpec(ct *shipper.CapacityTarget, clusterSpec shipper.ClusterCapacityTarget, targetNamespace string, selector labels.Selector, clusterStatus *shipper.ClusterCapacityStatus) (*appsv1.Deployment, error) {
	diff := diffutil.NewMultiDiff()
	defer func() {
		c.reportClusterCapacityConditionChange(ct, diff)
	}()
	targetClusterInformer, err := c.clusterClientStore.GetInformerFactory(clusterSpec.Name)
	if err != nil {
		cond := capacityutil.NewClusterCapacityCondition(
			shipper.ClusterConditionTypeOperational,
			corev1.ConditionFalse,
			ServerError,
			err.Error())
		diff.Append(capacityutil.SetClusterCapacityCondition(clusterStatus, *cond))

		return nil, err
	}

	deploymentsList, err := targetClusterInformer.Apps().V1().Deployments().Lister().Deployments(targetNamespace).List(selector)
	if err != nil {
		cond := capacityutil.NewClusterCapacityCondition(
			shipper.ClusterConditionTypeOperational,
			corev1.ConditionFalse,
			ServerError,
			err.Error())
		diff.Append(capacityutil.SetClusterCapacityCondition(clusterStatus, *cond))

		return nil, shippererrors.NewKubeclientListError(
			appsv1.SchemeGroupVersion.WithKind("Deployment"),
			targetNamespace, selector, err)
	}

	if l := len(deploymentsList); l != 1 {
		err = shippererrors.NewTargetDeploymentCountError(
			clusterSpec.Name, targetNamespace, selector.String(), l)

		cond := capacityutil.NewClusterCapacityCondition(
			shipper.ClusterConditionTypeReady,
			corev1.ConditionFalse,
			MissingDeployment,
			err.Error())
		diff.Append(capacityutil.SetClusterCapacityCondition(clusterStatus, *cond))

		return nil, err
	}

	targetDeployment := deploymentsList[0]

	return targetDeployment, nil
}

func (c *Controller) patchDeploymentWithReplicaCount(ct *shipper.CapacityTarget, targetDeployment *appsv1.Deployment, clusterName string, replicaCount int32, clusterStatus *shipper.ClusterCapacityStatus) (*appsv1.Deployment, error) {
	diff := diffutil.NewMultiDiff()
	defer func() {
		c.reportClusterCapacityConditionChange(ct, diff)
	}()
	targetClusterClient, err := c.clusterClientStore.GetClient(clusterName, AgentName)
	if err != nil {
		cond := capacityutil.NewClusterCapacityCondition(
			shipper.ClusterConditionTypeOperational,
			corev1.ConditionFalse,
			ServerError,
			err.Error(),
		)
		diff.Append(capacityutil.SetClusterCapacityCondition(clusterStatus, *cond))

		return nil, err
	}

	patchString := fmt.Sprintf(`{"spec": {"replicas": %d}}`, replicaCount)

	updatedDeployment, err := targetClusterClient.AppsV1().Deployments(targetDeployment.Namespace).Patch(targetDeployment.Name, types.StrategicMergePatchType, []byte(patchString))
	if err != nil {
		cond := capacityutil.NewClusterCapacityCondition(
			shipper.ClusterConditionTypeOperational,
			corev1.ConditionFalse,
			ServerError,
			err.Error(),
		)
		diff.Append(capacityutil.SetClusterCapacityCondition(clusterStatus, *cond))

		return nil, shippererrors.NewKubeclientUpdateError(targetDeployment, err)
	}

	return updatedDeployment, nil
}

func (c *Controller) reportClusterCapacityConditionChange(ct *shipper.CapacityTarget, diff diffutil.Diff) {
	if !diff.IsEmpty() {
		c.recorder.Event(ct, corev1.EventTypeNormal, "ClusterCapacityConditionChanged", diff.String())
	}
}
