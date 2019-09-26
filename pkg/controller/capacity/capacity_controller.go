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
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	clientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	informers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	listers "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/clusterclientstore"
	"github.com/bookingcom/shipper/pkg/conditions"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	"github.com/bookingcom/shipper/pkg/util/replicas"
)

const (
	AgentName   = "capacity-controller"
	SadPodLimit = 5

	// maxRetries is the number of times a CapacityTarget will be retried before we
	// drop it out of the workqueue. The number is chosen with the default rate
	// limiter in mind. This results in the following backoff times: 5ms, 10ms,
	// 20ms, 40ms, 80ms, 160ms, 320ms, 640ms, 1.3s, 2.6s, 5.1s, 10.2s.
	maxRetries = 11
)

// Controller is the controller implementation for CapacityTarget resources
type Controller struct {
	shipperclientset        clientset.Interface
	clusterClientStore      clusterClientStoreInterface
	capacityTargetsLister   listers.CapacityTargetLister
	capacityTargetsSynced   cache.InformerSynced
	releasesLister          listers.ReleaseLister
	releasesListerSynced    cache.InformerSynced
	capacityTargetWorkqueue workqueue.RateLimitingInterface
	recorder                record.EventRecorder
}

// NewController returns a new CapacityTarget controller.
func NewController(
	shipperclientset clientset.Interface,
	shipperInformerFactory informers.SharedInformerFactory,
	store clusterClientStoreInterface,
	recorder record.EventRecorder,
) *Controller {

	capacityTargetInformer := shipperInformerFactory.Shipper().V1alpha1().CapacityTargets()

	releaseInformer := shipperInformerFactory.Shipper().V1alpha1().Releases()

	controller := &Controller{
		shipperclientset:        shipperclientset,
		capacityTargetsLister:   capacityTargetInformer.Lister(),
		capacityTargetsSynced:   capacityTargetInformer.Informer().HasSynced,
		releasesLister:          releaseInformer.Lister(),
		releasesListerSynced:    releaseInformer.Informer().HasSynced,
		capacityTargetWorkqueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "capacity_controller_capacitytargets"),
		recorder:                recorder,
		clusterClientStore:      store,
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
	defer c.capacityTargetWorkqueue.ShutDown()

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
	obj, shutdown := c.capacityTargetWorkqueue.Get()
	if shutdown {
		return false
	}

	defer c.capacityTargetWorkqueue.Done(obj)

	var (
		key string
		ok  bool
	)

	if key, ok = obj.(string); !ok {
		c.capacityTargetWorkqueue.Forget(obj)
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
		if c.capacityTargetWorkqueue.NumRequeues(key) >= maxRetries {
			// Drop the CapacityTarget's key out of the workqueue and thus reset its
			// backoff. This limits the time a "broken" object can hog a worker.
			klog.Warningf("CapacityTarget %q has been retried too many times, dropping from the queue", key)
			c.capacityTargetWorkqueue.Forget(key)

			return true
		}

		c.capacityTargetWorkqueue.AddRateLimited(key)

		return true
	}

	klog.V(4).Infof("Successfully synced CapacityTarget %q", key)
	c.capacityTargetWorkqueue.Forget(obj)

	return true
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

	targetNamespace := ct.Namespace
	selector := labels.Set(ct.Labels).AsSelector()
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

		// all the below functions add conditions to the clusterStatus as they do
		// their business, hence we're passing them a pointer.
		targetDeployment, err := c.findTargetDeploymentForClusterSpec(clusterSpec, targetNamespace, selector, &clusterStatus)
		if err != nil {
			clusterErrors.Append(err)
		}

		var availableReplicas, achievedPercent int32

		if targetDeployment != nil {
			// Get the requested percentage of replicas from the capacity object. This is
			// only set by the scheduler.
			replicaCount := int32(replicas.CalculateDesiredReplicaCount(uint(clusterSpec.TotalReplicaCount), float64(clusterSpec.Percent)))

			// Patch the deployment if it doesn't match the cluster spec.
			if targetDeployment.Spec.Replicas == nil || replicaCount != *targetDeployment.Spec.Replicas {
				_, err = c.patchDeploymentWithReplicaCount(targetDeployment, clusterSpec.Name, replicaCount, &clusterStatus)
				if err != nil {
					clusterErrors.Append(err)
				}
			}

			availableReplicas = targetDeployment.Status.AvailableReplicas
			achievedPercent = c.calculatePercentageFromAmount(
				clusterSpec.TotalReplicaCount,
				availableReplicas,
			)

			report, err := c.getReport(targetDeployment, &clusterStatus)
			if err != nil {
				clusterErrors.Append(err)
			} else {
				clusterStatus.Reports = []shipper.ClusterCapacityReport{*report}
			}

			sadPods, clusterOk, err := c.getSadPods(targetDeployment, &clusterStatus)
			if err != nil {
				clusterErrors.Append(err)
			} else {
				clusterStatus.SadPods = sadPods
			}

			if clusterOk {
				clusterStatus.Conditions = conditions.SetCapacityCondition(
					clusterStatus.Conditions,
					shipper.ClusterConditionTypeReady,
					corev1.ConditionTrue,
					"", "")
				clusterStatus.Conditions = conditions.SetCapacityCondition(
					clusterStatus.Conditions,
					shipper.ClusterConditionTypeOperational,
					corev1.ConditionTrue,
					"",
					"")
			}
		}

		clusterStatus.AvailableReplicas = availableReplicas
		clusterStatus.AchievedPercent = achievedPercent

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

	c.capacityTargetWorkqueue.Add(key)
}

func (c *Controller) registerDeploymentEventHandlers(informerFactory kubeinformers.SharedInformerFactory, clusterName string) {
	informerFactory.Apps().V1().Deployments().Informer().AddEventHandler(c.NewDeploymentResourceEventHandler(clusterName))
}

func (c *Controller) subscribeToDeployments(informerFactory kubeinformers.SharedInformerFactory) {
	informerFactory.Apps().V1().Deployments().Informer()
	informerFactory.Core().V1().Pods().Informer()
}

type clusterClientStoreInterface interface {
	AddSubscriptionCallback(clusterclientstore.SubscriptionRegisterFunc)
	AddEventHandlerCallback(clusterclientstore.EventHandlerRegisterFunc)
	GetClient(string, string) (kubernetes.Interface, error)
	GetInformerFactory(string) (kubeinformers.SharedInformerFactory, error)
}

func (c *Controller) getSadPods(targetDeployment *appsv1.Deployment, clusterStatus *shipper.ClusterCapacityStatus) ([]shipper.PodStatus, bool, error) {
	podCount, sadPodsCount, sadPods, err := c.getSadPodsForDeploymentOnCluster(targetDeployment, clusterStatus.Name)
	if err != nil {
		clusterStatus.Conditions = conditions.SetCapacityCondition(
			clusterStatus.Conditions,
			shipper.ClusterConditionTypeOperational,
			corev1.ConditionFalse,
			conditions.ServerError,
			err.Error())

		return nil, false, err
	}

	if targetDeployment.Spec.Replicas == nil || int(*targetDeployment.Spec.Replicas) != podCount {
		clusterStatus.Conditions = conditions.SetCapacityCondition(
			clusterStatus.Conditions,
			shipper.ClusterConditionTypeReady,
			corev1.ConditionFalse,
			conditions.WrongPodCount,
			fmt.Sprintf("expected %d replicas but have %d", *targetDeployment.Spec.Replicas, int32(podCount)))

		return sadPods, false, nil
	}

	if sadPodsCount > 0 {
		clusterStatus.Conditions = conditions.SetCapacityCondition(
			clusterStatus.Conditions,
			shipper.ClusterConditionTypeReady,
			corev1.ConditionFalse,
			conditions.PodsNotReady,
			fmt.Sprintf("there are %d sad pods", sadPodsCount))
	}

	return sadPods, sadPodsCount == 0, nil
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

func (c *Controller) findTargetDeploymentForClusterSpec(clusterSpec shipper.ClusterCapacityTarget, targetNamespace string, selector labels.Selector, clusterStatus *shipper.ClusterCapacityStatus) (*appsv1.Deployment, error) {
	targetClusterInformer, err := c.clusterClientStore.GetInformerFactory(clusterSpec.Name)
	if err != nil {
		clusterStatus.Conditions = conditions.SetCapacityCondition(
			clusterStatus.Conditions,
			shipper.ClusterConditionTypeOperational,
			corev1.ConditionFalse,
			conditions.ServerError,
			err.Error(),
		)

		return nil, err
	}

	deploymentsList, err := targetClusterInformer.Apps().V1().Deployments().Lister().Deployments(targetNamespace).List(selector)
	if err != nil {
		clusterStatus.Conditions = conditions.SetCapacityCondition(
			clusterStatus.Conditions,
			shipper.ClusterConditionTypeOperational,
			corev1.ConditionFalse,
			conditions.ServerError,
			err.Error(),
		)

		return nil, shippererrors.NewKubeclientListError(
			appsv1.SchemeGroupVersion.WithKind("Deployment"),
			targetNamespace, selector, err)
	}

	if l := len(deploymentsList); l != 1 {
		err = shippererrors.NewTargetDeploymentCountError(
			clusterSpec.Name, targetNamespace, selector.String(), l)

		clusterStatus.Conditions = conditions.SetCapacityCondition(
			clusterStatus.Conditions,
			shipper.ClusterConditionTypeReady,
			corev1.ConditionFalse,
			conditions.MissingDeployment,
			err.Error(),
		)

		return nil, err
	}

	targetDeployment := deploymentsList[0]

	return targetDeployment, nil
}

func (c *Controller) patchDeploymentWithReplicaCount(targetDeployment *appsv1.Deployment, clusterName string, replicaCount int32, clusterStatus *shipper.ClusterCapacityStatus) (*appsv1.Deployment, error) {
	targetClusterClient, err := c.clusterClientStore.GetClient(clusterName, AgentName)
	if err != nil {
		clusterStatus.Conditions = conditions.SetCapacityCondition(
			clusterStatus.Conditions,
			shipper.ClusterConditionTypeOperational,
			corev1.ConditionFalse,
			conditions.ServerError,
			err.Error(),
		)

		return nil, err
	}

	patchString := fmt.Sprintf(`{"spec": {"replicas": %d}}`, replicaCount)

	updatedDeployment, err := targetClusterClient.AppsV1().Deployments(targetDeployment.Namespace).Patch(targetDeployment.Name, types.StrategicMergePatchType, []byte(patchString))
	if err != nil {
		clusterStatus.Conditions = conditions.SetCapacityCondition(
			clusterStatus.Conditions,
			shipper.ClusterConditionTypeOperational,
			corev1.ConditionFalse,
			conditions.ServerError,
			err.Error(),
		)

		return nil, shippererrors.NewKubeclientUpdateError(targetDeployment, err)
	}

	return updatedDeployment, nil
}
