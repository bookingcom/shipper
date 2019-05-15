package capacity

import (
	"fmt"
	"sort"
	"time"

	"github.com/golang/glog"
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

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	clientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	informers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	listers "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/clusterclientstore"
	"github.com/bookingcom/shipper/pkg/conditions"
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
	deploymentWorkqueue     workqueue.RateLimitingInterface
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
		deploymentWorkqueue:     workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "capacity_controller_deployments"),
		recorder:                recorder,
		clusterClientStore:      store,
	}

	glog.Info("Setting up event handlers")
	capacityTargetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueCapacityTarget,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueCapacityTarget(new)
		},
	})

	store.AddSubscriptionCallback(controller.subscribe)
	store.AddEventHandlerCallback(controller.registerEventHandlers)

	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) {
	defer runtime.HandleCrash()
	defer c.capacityTargetWorkqueue.ShutDown()
	defer c.deploymentWorkqueue.ShutDown()

	glog.V(2).Info("Starting Capacity controller")
	defer glog.V(2).Info("Shutting down Capacity controller")

	if !cache.WaitForCacheSync(stopCh, c.capacityTargetsSynced, c.releasesListerSynced) {
		runtime.HandleError(fmt.Errorf("failed to wait for caches to sync"))
		return
	}

	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runCapacityTargetWorker, time.Second, stopCh)
		go wait.Until(c.runDeploymentWorker, time.Second, stopCh)
	}

	glog.V(4).Info("Started Capacity controller")

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
		runtime.HandleError(fmt.Errorf("invalid object key (will not retry): %#v", obj))
		return true
	}

	if shouldRetry := c.capacityTargetSyncHandler(key); shouldRetry {
		if c.capacityTargetWorkqueue.NumRequeues(key) >= maxRetries {
			// Drop the CapacityTarget's key out of the workqueue and thus reset its
			// backoff. This limits the time a "broken" object can hog a worker.
			glog.Warningf("CapacityTarget %q has been retried too many times, dropping from the queue", key)
			c.capacityTargetWorkqueue.Forget(key)

			return true
		}

		c.capacityTargetWorkqueue.AddRateLimited(key)

		return true
	}

	glog.V(4).Infof("Successfully synced CapacityTarget %q", key)
	c.capacityTargetWorkqueue.Forget(obj)

	return true
}

func (c *Controller) capacityTargetSyncHandler(key string) bool {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid object key (will not retry): %s", key))
		return false
	}

	ct, err := c.capacityTargetsLister.CapacityTargets(namespace).Get(name)
	if err != nil {
		if kerrors.IsNotFound(err) {
			glog.V(3).Infof("CapacityTarget %q has been deleted", key)
			return false
		}

		runtime.HandleError(fmt.Errorf("error syncing CapacityTarget %q (will retry): %s", key, err))
		return true
	}

	ct = ct.DeepCopy()

	targetNamespace := ct.Namespace
	selector := labels.Set(ct.Labels).AsSelector()

	newClusterStatuses := make([]shipper.ClusterCapacityStatus, 0, len(ct.Spec.Clusters))

	for _, clusterSpec := range ct.Spec.Clusters {

		clusterStatus := shipper.ClusterCapacityStatus{
			Name:    clusterSpec.Name,
			Reports: []shipper.ClusterCapacityReport{},
		}

		// all the below functions add conditions to the clusterStatus as they do
		// their business, hence we're passing them a pointer.
		targetDeployment, err := c.findTargetDeploymentForClusterSpec(clusterSpec, targetNamespace, selector, &clusterStatus)
		if err != nil {
			c.recordErrorEvent(ct, err)
			continue
		}

		// Get the requested percentage of replicas from the capacity object. This is
		// only set by the scheduler.
		replicaCount := int32(replicas.CalculateDesiredReplicaCount(uint(clusterSpec.TotalReplicaCount), float64(clusterSpec.Percent)))

		// Patch the deployment if it doesn't match the cluster spec.
		if targetDeployment.Spec.Replicas == nil || replicaCount != *targetDeployment.Spec.Replicas {
			_, err = c.patchDeploymentWithReplicaCount(targetDeployment, clusterSpec.Name, replicaCount, &clusterStatus)
			if err != nil {
				c.recordErrorEvent(ct, err)
				continue
			}
		}

		clusterStatus.AvailableReplicas = targetDeployment.Status.AvailableReplicas
		clusterStatus.AchievedPercent = c.calculatePercentageFromAmount(clusterSpec.TotalReplicaCount, clusterStatus.AvailableReplicas)

		report, err := c.getReport(targetDeployment, &clusterStatus)
		if err != nil {
			c.recordErrorEvent(ct, err)
		} else {
			clusterStatus.Reports = []shipper.ClusterCapacityReport{*report}
		}

		sadPods, err := c.getSadPods(targetDeployment, &clusterStatus)
		if err != nil {
			c.recordErrorEvent(ct, err)
		} else {
			clusterStatus.SadPods = sadPods
			if len(sadPods) == 0 {
				// If we've got here, the capacity target has no sad pods and there have been
				// no errors, so set conditions to true.
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

		newClusterStatuses = append(newClusterStatuses, clusterStatus)
	}

	sort.Sort(byClusterName(newClusterStatuses))

	ct.Status.Clusters = newClusterStatuses

	_, err = c.shipperclientset.ShipperV1alpha1().CapacityTargets(namespace).Update(ct)
	if err != nil {
		runtime.HandleError(fmt.Errorf("error syncing CapacityTarget %q (will retry): %s", key, err))
		return true
	}

	return false
}

func (c *Controller) enqueueCapacityTarget(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	c.capacityTargetWorkqueue.Add(key)
}

func (c *Controller) registerEventHandlers(informerFactory kubeinformers.SharedInformerFactory, clusterName string) {
	informerFactory.Apps().V1().Deployments().Informer().AddEventHandler(c.NewDeploymentResourceEventHandler(clusterName))
}

func (c *Controller) subscribe(informerFactory kubeinformers.SharedInformerFactory) {
	informerFactory.Apps().V1().Deployments().Informer()
	informerFactory.Core().V1().Pods().Informer()
}

type clusterClientStoreInterface interface {
	AddSubscriptionCallback(clusterclientstore.SubscriptionRegisterFunc)
	AddEventHandlerCallback(clusterclientstore.EventHandlerRegisterFunc)
	GetClient(string) (kubernetes.Interface, error)
	GetInformerFactory(string) (kubeinformers.SharedInformerFactory, error)
}

func (c *Controller) getSadPods(targetDeployment *appsv1.Deployment, clusterStatus *shipper.ClusterCapacityStatus) ([]shipper.PodStatus, error) {
	podCount, sadPodsCount, sadPods, err := c.getSadPodsForDeploymentOnCluster(targetDeployment, clusterStatus.Name)
	if err != nil {
		clusterStatus.Conditions = conditions.SetCapacityCondition(
			clusterStatus.Conditions,
			shipper.ClusterConditionTypeOperational,
			corev1.ConditionFalse,
			conditions.ServerError,
			err.Error())

		return nil, err
	}

	if targetDeployment.Spec.Replicas == nil || int(*targetDeployment.Spec.Replicas) != podCount {
		err = NewInvalidPodCountError(*targetDeployment.Spec.Replicas, int32(podCount))
		clusterStatus.Conditions = conditions.SetCapacityCondition(
			clusterStatus.Conditions,
			shipper.ClusterConditionTypeReady,
			corev1.ConditionFalse,
			conditions.WrongPodCount,
			err.Error())

		return nil, err
	}

	if sadPodsCount > 0 {
		clusterStatus.Conditions = conditions.SetCapacityCondition(
			clusterStatus.Conditions,
			shipper.ClusterConditionTypeReady,
			corev1.ConditionFalse,
			conditions.PodsNotReady,
			fmt.Sprintf("there are %d sad pods", sadPodsCount))
	}

	return sadPods, nil
}

func (c *Controller) getReport(targetDeployment *appsv1.Deployment, clusterStatus *shipper.ClusterCapacityStatus) (*shipper.ClusterCapacityReport, error) {
	targetClusterInformer, clusterErr := c.clusterClientStore.GetInformerFactory(clusterStatus.Name)
	if clusterErr != nil {
		// Not sure if each method should report operational conditions for
		// the cluster it is operating on.
		return nil, clusterErr
	}

	selector := labels.Set(targetDeployment.Spec.Template.Labels).AsSelector()
	podsList, clusterErr := targetClusterInformer.Core().V1().Pods().Lister().Pods(targetDeployment.Namespace).List(selector)
	if clusterErr != nil {
		return nil, clusterErr
	}

	report := buildReport(targetDeployment.Name, podsList)

	return report, nil
}

func (c *Controller) findTargetDeploymentForClusterSpec(clusterSpec shipper.ClusterCapacityTarget, targetNamespace string, selector labels.Selector, clusterStatus *shipper.ClusterCapacityStatus) (*appsv1.Deployment, error) {
	targetClusterInformer, clusterErr := c.clusterClientStore.GetInformerFactory(clusterSpec.Name)
	if clusterErr != nil {
		clusterStatus.Conditions = conditions.SetCapacityCondition(
			clusterStatus.Conditions,
			shipper.ClusterConditionTypeOperational,
			corev1.ConditionFalse,
			conditions.ServerError,
			clusterErr.Error(),
		)

		return nil, clusterErr
	}

	deploymentsList, clusterErr := targetClusterInformer.Apps().V1().Deployments().Lister().Deployments(targetNamespace).List(selector)
	if clusterErr != nil {
		clusterStatus.Conditions = conditions.SetCapacityCondition(
			clusterStatus.Conditions,
			shipper.ClusterConditionTypeOperational,
			corev1.ConditionFalse,
			conditions.ServerError,
			clusterErr.Error(),
		)

		return nil, clusterErr
	}

	if l := len(deploymentsList); l != 1 {
		clusterErr = fmt.Errorf(
			"expected exactly 1 deployment on cluster %s, namespace %s, with label %s, but %d deployments exist",
			clusterSpec.Name, targetNamespace, selector.String(), l)

		clusterStatus.Conditions = conditions.SetCapacityCondition(
			clusterStatus.Conditions,
			shipper.ClusterConditionTypeReady,
			corev1.ConditionFalse,
			conditions.MissingDeployment,
			clusterErr.Error(),
		)

		return nil, clusterErr
	}

	targetDeployment := deploymentsList[0]

	return targetDeployment, nil
}

func (c *Controller) recordErrorEvent(capacityTarget *shipper.CapacityTarget, err error) {
	c.recorder.Event(
		capacityTarget,
		corev1.EventTypeWarning,
		"FailedCapacityChange",
		err.Error())
}

func (c *Controller) patchDeploymentWithReplicaCount(targetDeployment *appsv1.Deployment, clusterName string, replicaCount int32, clusterStatus *shipper.ClusterCapacityStatus) (*appsv1.Deployment, error) {
	targetClusterClient, clusterErr := c.clusterClientStore.GetClient(clusterName)
	if clusterErr != nil {
		clusterStatus.Conditions = conditions.SetCapacityCondition(
			clusterStatus.Conditions,
			shipper.ClusterConditionTypeOperational,
			corev1.ConditionFalse,
			conditions.ServerError,
			clusterErr.Error(),
		)

		return nil, clusterErr
	}

	patchString := fmt.Sprintf(`{"spec": {"replicas": %d}}`, replicaCount)

	updatedDeployment, clusterErr := targetClusterClient.AppsV1().Deployments(targetDeployment.Namespace).Patch(targetDeployment.Name, types.StrategicMergePatchType, []byte(patchString))
	if clusterErr != nil {
		clusterStatus.Conditions = conditions.SetCapacityCondition(
			clusterStatus.Conditions,
			shipper.ClusterConditionTypeOperational,
			corev1.ConditionFalse,
			conditions.ServerError,
			clusterErr.Error(),
		)

		return nil, clusterErr
	}

	return updatedDeployment, nil
}
