/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package capacity

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"time"

	"github.com/golang/glog"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	clientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	informers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	listers "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1"
	"github.com/bookingcom/shipper/pkg/clusterclientstore"
	"github.com/bookingcom/shipper/pkg/conditions"
	shippercontroller "github.com/bookingcom/shipper/pkg/controller"
)

const (
	AgentName   = "capacity-controller"
	SadPodLimit = 5
)

// Controller is the controller implementation for CapacityTarget resources
type Controller struct {
	// shipperclientset is a clientset for our own API group
	shipperclientset clientset.Interface

	clusterClientStore clusterClientStoreInterface

	capacityTargetsLister listers.CapacityTargetLister
	capacityTargetsSynced cache.InformerSynced

	releasesLister       listers.ReleaseLister
	releasesListerSynced cache.InformerSynced

	// capacityTargetWorkqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	capacityTargetWorkqueue workqueue.RateLimitingInterface

	// deploymentWorkqueue is a rate-limited queue, similar to the capacityTargetWorkqueue
	deploymentWorkqueue workqueue.RateLimitingInterface

	// recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	recorder record.EventRecorder
}

// NewController returns a new CapacityTarget controller
func NewController(
	shipperclientset clientset.Interface,
	shipperInformerFactory informers.SharedInformerFactory,
	store clusterClientStoreInterface,
	recorder record.EventRecorder,
) *Controller {

	// obtain references to shared index informers for the CapacityTarget type
	capacityTargetInformer := shipperInformerFactory.Shipper().V1().CapacityTargets()

	releaseInformer := shipperInformerFactory.Shipper().V1().Releases()

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
	// Set up an event handler for when CapacityTarget resources change
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

	// Wait for the caches to be synced before starting workers
	if !cache.WaitForCacheSync(stopCh, c.capacityTargetsSynced, c.releasesListerSynced) {
		runtime.HandleError(fmt.Errorf("failed to wait for caches to sync"))
		return
	}

	// Launch workers to process CapacityTarget resources
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runCapacityTargetWorker, time.Second, stopCh)
		go wait.Until(c.runDeploymentWorker, time.Second, stopCh)
	}

	glog.V(4).Info("Started Capacity controller")

	<-stopCh
}

// runCapacityTargetWorker is a long-running function that will continually call the
// processNextCapacityTargetWorkItem function in order to read and process a message on the
// workqueue.
func (c *Controller) runCapacityTargetWorker() {
	for c.processNextCapacityTargetWorkItem() {
	}
}

// processNextCapacityTargetWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (c *Controller) processNextCapacityTargetWorkItem() bool {
	obj, shutdown := c.capacityTargetWorkqueue.Get()

	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer c.CapacityTargetWorkqueue.Done.
	err := func(obj interface{}) error {
		// We call Done here so the workqueue knows we have finished
		// processing this item. We also must remember to call Forget if we
		// do not want this work item being re-queued. For example, we do
		// not call Forget if a transient error occurs, instead the item is
		// put back on the workqueue and attempted again after a back-off
		// period.
		defer c.capacityTargetWorkqueue.Done(obj)
		var key string
		var ok bool
		// We expect strings to come off the workqueue. These are of the
		// form namespace/name. We do this as the delayed nature of the
		// workqueue means the items in the informer cache may actually be
		// more up to date that when the item was initially put onto the
		// workqueue.
		if key, ok = obj.(string); !ok {
			// As the item in the workqueue is actually invalid, we call
			// Forget here else we'd go into a loop of attempting to
			// process a work item that is invalid.
			c.capacityTargetWorkqueue.Forget(obj)
			runtime.HandleError(fmt.Errorf("expected string in capacity target workqueue but got %#v", obj))
			return nil
		}
		// Run the syncHandler, passing it the namespace/name string of the
		// CapacityTarget resource to be synced.
		if err := c.capacityTargetSyncHandler(key); err != nil {
			return fmt.Errorf("error syncing %q: %s", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.capacityTargetWorkqueue.Forget(obj)
		glog.Infof("Successfully synced '%s'", key)
		return nil
	}(obj)

	if err != nil {
		runtime.HandleError(err)
		return true
	}

	return true
}

// capacityTargetSyncHandler compares the actual state with the desired, and attempts to
// converge the two.
func (c *Controller) capacityTargetSyncHandler(key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	glog.Infof("Running syncHandler for %s:%s.", namespace, name)
	ct, err := c.capacityTargetsLister.CapacityTargets(namespace).Get(name)
	if err != nil {
		// The CapacityTarget resource may no longer exist, in which case we stop
		// processing.
		if errors.IsNotFound(err) {
			runtime.HandleError(fmt.Errorf("CapacityTarget %q in work queue no longer exists", key))
			return nil
		}

		return err
	}

	ct = ct.DeepCopy()
	targetNamespace := ct.Namespace
	selector := labels.Set(ct.Labels).AsSelector()

	for _, clusterSpec := range ct.Spec.Clusters {
		var clusterErr error
		var deploymentsList []*appsv1.Deployment
		var patchString string
		var clusterConditions []shipperv1.ClusterCapacityCondition
		var targetDeployment *appsv1.Deployment
		var replicaCount int32
		var targetClusterClient kubernetes.Interface
		var targetClusterInformer kubeinformers.SharedInformerFactory

		for _, e := range ct.Status.Clusters {
			if e.Name == clusterSpec.Name {
				clusterConditions = e.Conditions
			}
		}

		targetClusterClient, clusterErr = c.clusterClientStore.GetClient(clusterSpec.Name)
		if clusterErr == nil {
			clusterConditions = conditions.SetCapacityCondition(
				clusterConditions,
				shipperv1.ClusterConditionTypeOperational,
				corev1.ConditionTrue,
				"",
				"")
		} else {
			clusterConditions = conditions.SetCapacityCondition(
				clusterConditions,
				shipperv1.ClusterConditionTypeOperational,
				corev1.ConditionFalse,
				conditions.ServerError,
				clusterErr.Error(),
			)
			goto End
		}

		// Get the requested percentage of replicas from the capacity object
		// This is only set by the strategy controller
		replicaCount, clusterErr = c.convertPercentageToReplicaCountForCluster(ct, clusterSpec)
		if clusterErr != nil {
			switch clusterErr.(type) {
			case ReleaseIsGoneError:
				clusterConditions = conditions.SetCapacityCondition(
					clusterConditions,
					shipperv1.ClusterConditionTypeReady,
					corev1.ConditionFalse,
					conditions.MissingObjects,
					clusterErr.Error(),
				)
			case shippercontroller.MultipleOwnerReferencesError:
				clusterConditions = conditions.SetCapacityCondition(
					clusterConditions,
					shipperv1.ClusterConditionTypeReady,
					corev1.ConditionFalse,
					conditions.InvalidObjects,
					clusterErr.Error(),
				)
			default:
				// After investigating the code path of convertPercentageToReplicaCountForCluster(), the only
				// error it returns is a NotFound error when the release object associated with the capacity
				// target doesn't exist.
				clusterConditions = conditions.SetCapacityCondition(
					clusterConditions,
					shipperv1.ClusterConditionTypeReady,
					corev1.ConditionFalse,
					conditions.MissingObjects,
					clusterErr.Error(),
				)
			}
			goto End
		}

		targetClusterInformer, clusterErr = c.clusterClientStore.GetInformerFactory(clusterSpec.Name)
		if clusterErr != nil {
			clusterConditions = conditions.SetCapacityCondition(
				clusterConditions,
				shipperv1.ClusterConditionTypeOperational,
				corev1.ConditionFalse,
				conditions.ServerError,
				clusterErr.Error(),
			)
			goto End
		}

		deploymentsList, clusterErr = targetClusterInformer.Apps().V1().Deployments().Lister().Deployments(targetNamespace).List(selector)
		if clusterErr != nil {
			clusterConditions = conditions.SetCapacityCondition(
				clusterConditions,
				shipperv1.ClusterConditionTypeOperational,
				corev1.ConditionFalse,
				conditions.ServerError,
				clusterErr.Error(),
			)

			goto End
		}

		if l := len(deploymentsList); l != 1 {
			clusterErr = fmt.Errorf(
				"expected a deployment on cluster %s, namespace %s, with label %s, but %d deployments exist",
				clusterSpec.Name, targetNamespace, selector.String(), l)

			clusterConditions = conditions.SetCapacityCondition(
				clusterConditions,
				shipperv1.ClusterConditionTypeReady,
				corev1.ConditionFalse,
				conditions.MissingDeployment,
				clusterErr.Error(),
			)

			goto End
		} else if l > 1 {
			clusterErr = fmt.Errorf(
				"expected a deployment on cluster %s, namespace %s, with label %s, but %d deployments exist",
				clusterSpec.Name, targetNamespace, selector.String(), l)

			clusterConditions = conditions.SetCapacityCondition(
				clusterConditions,
				shipperv1.ClusterConditionTypeReady,
				corev1.ConditionFalse,
				conditions.TooManyDeployments,
				clusterErr.Error(),
			)

			goto End
		}

		targetDeployment = deploymentsList[0]
		patchString = fmt.Sprintf(`{"spec": {"replicas": %d}}`, replicaCount)

		_, clusterErr = targetClusterClient.AppsV1().Deployments(targetDeployment.Namespace).Patch(targetDeployment.Name, types.StrategicMergePatchType, []byte(patchString))
		if clusterErr != nil {
			clusterConditions = conditions.SetCapacityCondition(
				clusterConditions,
				shipperv1.ClusterConditionTypeOperational,
				corev1.ConditionFalse,
				conditions.ServerError,
				clusterErr.Error(),
			)

			goto End
		}

	End:
		if clusterErr != nil {
			c.recorder.Event(
				ct,
				corev1.EventTypeWarning,
				"FailedCapacityChange",
				clusterErr.Error())
		} else {
			clusterConditions = conditions.SetCapacityCondition(
				clusterConditions,
				shipperv1.ClusterConditionTypeOperational,
				corev1.ConditionTrue,
				"",
				"")

			c.recorder.Eventf(
				ct,
				corev1.EventTypeNormal,
				"CapacityChanged",
				"Scaled %q to %d replicas",
				shippercontroller.MetaKey(targetDeployment),
				replicaCount)
		}

		if !reflect.DeepEqual(clusterConditions, ct.Status.Clusters) {
			ct.Status.Clusters = setClusterConditions(ct.Status.Clusters, clusterSpec.Name, clusterConditions)
		}
	}

	_, err = c.shipperclientset.ShipperV1().CapacityTargets(namespace).Update(ct)
	return err
}

func setClusterConditions(
	clusters []shipperv1.ClusterCapacityStatus,
	name string,
	clusterConditions []shipperv1.ClusterCapacityCondition,
) []shipperv1.ClusterCapacityStatus {
	seen := false
	for i, c := range clusters {
		if c.Name == name {
			clusters[i].Conditions = clusterConditions
			seen = true
		}
	}

	if !seen {
		capacityStatus := shipperv1.ClusterCapacityStatus{
			Name:       name,
			Conditions: clusterConditions,
		}
		clusters = append(clusters, capacityStatus)
	}
	return clusters
}

// enqueueCapacityTarget takes a CapacityTarget resource and converts it into a namespace/name
// string which is then put onto the work queue. This method should *not* be
// passed resources of any type other than CapacityTarget.
func (c *Controller) enqueueCapacityTarget(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		runtime.HandleError(err)
		return
	}
	c.capacityTargetWorkqueue.AddRateLimited(key)
}

func (c Controller) convertPercentageToReplicaCountForCluster(capacityTarget *shipperv1.CapacityTarget, cluster shipperv1.ClusterCapacityTarget) (int32, error) {
	release, err := c.getReleaseForCapacityTarget(capacityTarget)
	if err != nil {
		return 0, err
	}

	totalReplicaCount, err := strconv.Atoi(release.Annotations[shipperv1.ReleaseReplicasAnnotation])
	if err != nil {
		return 0, err
	}

	percentage := cluster.Percent

	return c.calculateAmountFromPercentage(int32(totalReplicaCount), percentage), nil
}

func (c Controller) getReleaseForCapacityTarget(capacityTarget *shipperv1.CapacityTarget) (*shipperv1.Release, error) {
	if n := len(capacityTarget.OwnerReferences); n != 1 {
		return nil, shippercontroller.NewMultipleOwnerReferencesError(capacityTarget.GetName(), n)
	}

	owner := capacityTarget.OwnerReferences[0]

	release, err := c.releasesLister.Releases(capacityTarget.GetNamespace()).Get(owner.Name)
	if err != nil {
		return nil, err
	} else if release.GetUID() != owner.UID {
		return nil, NewReleaseIsGoneError(shippercontroller.MetaKey(capacityTarget), owner.UID, release.GetUID())
	}

	return release, nil
}

func (c Controller) calculateAmountFromPercentage(total, percentage int32) int32 {
	result := float64(percentage) / 100 * float64(total)

	return int32(math.Ceil(result))
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
