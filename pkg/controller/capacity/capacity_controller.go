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
	"time"

	"github.com/golang/glog"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const AgentName = "capacity-controller"

const (
	// SuccessSynced is used as part of the Event 'reason' when a ShipmentOrder is synced
	SuccessSynced = "Synced"

	// MessageResourceSynced is used as part of the 'Event' message when a ShipmentOrder is synced
	MessageResourceSynced = "CapacityTarget synced successfully"
)

// Controller is the controller implementation for CapacityTarget resources
type Controller struct {
	// kubeclientset is a standard kubernetes clientset
	kubeclientset kubernetes.Interface

	// shipperclientset is a clientset for our own API group
	shipperclientset clientset.Interface

	clusterClientStore *clusterclientstore.Store

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
	kubeclientset kubernetes.Interface,
	shipperclientset clientset.Interface,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	shipperInformerFactory informers.SharedInformerFactory,
	store *clusterclientstore.Store,
	recorder record.EventRecorder,
) *Controller {

	// obtain references to shared index informers for the CapacityTarget type
	capacityTargetInformer := shipperInformerFactory.Shipper().V1().CapacityTargets()

	releaseInformer := shipperInformerFactory.Shipper().V1().Releases()

	controller := &Controller{
		kubeclientset:           kubeclientset,
		shipperclientset:        shipperclientset,
		capacityTargetsLister:   capacityTargetInformer.Lister(),
		capacityTargetsSynced:   capacityTargetInformer.Informer().HasSynced,
		releasesLister:          releaseInformer.Lister(),
		releasesListerSynced:    releaseInformer.Informer().HasSynced,
		capacityTargetWorkqueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "CapacityTargets"),
		deploymentWorkqueue:     workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Deployments"),
		recorder:                recorder,
		clusterClientStore:      store,
	}

	glog.Info("Setting up event handlers")
	// Set up an event handler for when CapacityTarget resources change
	capacityTargetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			glog.Info("Got an add event!")
			controller.enqueueCapacityTarget(obj)
		},
		UpdateFunc: func(old, new interface{}) {
			glog.Info("Got an update event!")
			controller.enqueueCapacityTarget(new)
		},
		// the syncHandler needs to cope with the case where the object was deleted
		DeleteFunc: controller.enqueueCapacityTarget,
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

// processNextWorkCapacityTargetItem will read a single work item off the workqueue and
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
			return fmt.Errorf("error syncing '%s': %s", key, err.Error())
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
// converge the two. It then updates the Status block of the ShipmentOrder resource
// with the current status of the resource.
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
			runtime.HandleError(fmt.Errorf("CapacityTarget '%s' in work queue no longer exists", key))
			return nil
		}

		return err
	}

	for _, clusterSpec := range ct.Spec.Clusters {
		// Get the requested percentage of replicas from the capacity object
		// This is only set by the strategy controller
		var replicaCount int32
		replicaCount, err = c.convertPercentageToReplicaCountForCluster(ct, clusterSpec)
		if err != nil {
			return err
		}

		var targetClusterClient kubernetes.Interface
		targetClusterClient, err = c.clusterClientStore.GetClient(clusterSpec.Name)
		if err != nil {
			return err
		}

		targetNamespace := ct.Namespace

		selector := labels.Set(ct.Labels).AsSelector()

		var deploymentsList *appsv1.DeploymentList
		deploymentsList, err = targetClusterClient.AppsV1().Deployments(targetNamespace).List(metav1.ListOptions{LabelSelector: selector.String()})
		if err != nil {
			return err
		}

		if len(deploymentsList.Items) != 1 {
			return fmt.Errorf("Expected a deployment on cluster %s, namespace %s, with label %s, but %d deployments exist.", clusterSpec.Name, targetNamespace, selector.String(), len(deploymentsList.Items))
		}

		targetDeployment := deploymentsList.Items[0]
		patchString := fmt.Sprintf(`{"spec": {"replicas": %d}}`, replicaCount)
		_, err = targetClusterClient.AppsV1().Deployments(targetDeployment.Namespace).Patch(targetDeployment.Name, types.StrategicMergePatchType, []byte(patchString))
		if err != nil {
			return err
		}
	}

	if err != nil {
		return err
	}

	c.recorder.Event(ct, corev1.EventTypeNormal, SuccessSynced, MessageResourceSynced)
	return nil
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

	totalReplicaCount := release.Environment.Replicas
	percentage := cluster.Percent

	return c.calculateAmountFromPercentage(totalReplicaCount, percentage), nil
}

func (c Controller) getReleaseForCapacityTarget(capacityTarget *shipperv1.CapacityTarget) (*shipperv1.Release, error) {
	if n := len(capacityTarget.OwnerReferences); n != 1 {
		return nil, fmt.Errorf("expected exactly one owner for CapacityTarget %q, got %d", capacityTarget.GetName(), n)
	}

	owner := capacityTarget.OwnerReferences[0]

	release, err := c.releasesLister.Releases(capacityTarget.GetNamespace()).Get(owner.Name)
	if err != nil {
		return nil, err
	} else if release.GetUID() != owner.UID {
		return nil, fmt.Errorf(
			"the owner Release for CapacityTarget %q is gone; expected UID %s but got %s",
			capacityTarget.GetName(),
			owner.UID,
			release.GetUID(),
		)
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
}
