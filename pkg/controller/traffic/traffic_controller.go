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

package traffic

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shipper "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	informers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	listers "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1"
	"github.com/bookingcom/shipper/pkg/clusterclientstore"
)

const AgentName = "traffic-controller"

const (
	// SuccessSynced is used as part of the Event 'reason' when a TrafficTarget is synced
	SuccessSynced = "Synced"

	// MessageResourceSynced is used as part of the 'Event' message when a TrafficTarget is synced
	MessageResourceSynced = "TrafficTarget synced successfully"
)

// Controller is the controller implementation for TrafficTarget resources
type Controller struct {
	// shipperclientset is a clientset for our own API group
	shipperclientset shipper.Interface

	// the Kube clients for each of the target clusters
	clusterClientStore *clusterclientstore.Store

	trafficTargetsLister listers.TrafficTargetLister
	trafficTargetsSynced cache.InformerSynced

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueue workqueue.RateLimitingInterface

	// recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	recorder record.EventRecorder
}

// NewController returns a new TrafficTarget controller
func NewController(
	kubeclientset kubernetes.Interface,
	shipperclientset shipper.Interface,
	shipperInformerFactory informers.SharedInformerFactory,
	store *clusterclientstore.Store,
	recorder record.EventRecorder,
) *Controller {

	// obtain references to shared index informers for the TrafficTarget type
	trafficTargetInformer := shipperInformerFactory.Shipper().V1().TrafficTargets()

	controller := &Controller{
		shipperclientset:   shipperclientset,
		clusterClientStore: store,

		trafficTargetsLister: trafficTargetInformer.Lister(),
		trafficTargetsSynced: trafficTargetInformer.Informer().HasSynced,
		workqueue:            workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "TrafficTargets"),
		recorder:             recorder,
	}

	glog.Info("Setting up event handlers")
	// Set up an event handler for when TrafficTarget resources change
	trafficTargetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueTrafficTarget,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueTrafficTarget(new)
		},
		// the syncHandler needs to cope with the case where the object was deleted
		DeleteFunc: controller.enqueueTrafficTarget,
	})

	store.AddSubscriptionCallback(func(informerFactory kubeinformers.SharedInformerFactory) {
		informerFactory.Core().V1().Pods().Informer()
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

	glog.V(2).Info("Starting Traffic controller")
	defer glog.V(2).Info("Shutting down Traffic controller")

	if ok := cache.WaitForCacheSync(stopCh, c.trafficTargetsSynced); !ok {
		runtime.HandleError(fmt.Errorf("failed to wait for caches to sync"))
		return
	}

	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	glog.V(4).Info("Started Traffic controller")

	<-stopCh
}

// runWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (c *Controller) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()

	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer c.workqueue.Done.
	err := func(obj interface{}) error {
		// We call Done here so the workqueue knows we have finished
		// processing this item. We also must remember to call Forget if we
		// do not want this work item being re-queued. For example, we do
		// not call Forget if a transient error occurs, instead the item is
		// put back on the workqueue and attempted again after a back-off
		// period.
		defer c.workqueue.Done(obj)
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
			c.workqueue.Forget(obj)
			runtime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		// Run the syncHandler, passing it the namespace/name string of the
		// TrafficTarget resource to be synced.
		if err := c.syncHandler(key); err != nil {
			return fmt.Errorf("error syncing '%s': %s", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.workqueue.Forget(obj)
		glog.Infof("Successfully synced '%s'", key)
		return nil
	}(obj)

	if err != nil {
		runtime.HandleError(err)
		return true
	}

	return true
}

// Any time a TrafficTarget resource is modified, we should:
// - Get all TTs in the namespace
// - For each TT, get desired weight in target clusters.
// - For each cluster, compute pod label %s according to TT weights by release.
// - For each cluster, add/remove pod labels accordingly (if too many, remove until correct and visa versa)
func (c *Controller) syncHandler(key string) error {
	namespace, ttName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	syncingTT, err := c.trafficTargetsLister.TrafficTargets(namespace).Get(ttName)
	if err != nil {
		return err
	}

	// NOTE(btyler) - this will need fixing if we allow multiple applications
	// per namespace. in that case we should get all the objects with the same
	// 'app' label, or something similar. Maybe by chart name?
	list, err := c.trafficTargetsLister.TrafficTargets(namespace).List(labels.Everything())
	if err != nil {
		return err
	}

	shifter, err := newPodLabelShifter(namespace, list)
	if err != nil {
		return err
	}

	statuses := []*shipperv1.ClusterTrafficStatus{}
	for _, cluster := range shifter.Clusters() {
		clusterStatus := &shipperv1.ClusterTrafficStatus{
			Name: cluster,
		}

		statuses = append(statuses, clusterStatus)

		var clientset kubernetes.Interface
		clientset, err = c.clusterClientStore.GetClient(cluster)
		if err != nil {
			clusterStatus.Status = err.Error()
			break
		}
		var informerFactory kubeinformers.SharedInformerFactory
		informerFactory, err = c.clusterClientStore.GetInformerFactory(cluster)
		if err != nil {
			clusterStatus.Status = err.Error()
			break
		}

		errs := shifter.SyncCluster(cluster, clientset, informerFactory.Core().V1().Pods())
		if len(errs) == 0 {
			clusterStatus.Status = "Synced"
		} else {
			results := make([]string, 0, len(errs))
			for _, err := range errs {
				results = append(results, err.Error())
			}
			sort.Strings(results)
			clusterStatus.Status = strings.Join(results, ",")
		}
	}

	// NEVER modify objects from the store. It's a read-only, local cache.
	// You can use DeepCopy() to make a deep copy of original object and modify this copy
	// Or create a copy manually for better performance
	ttCopy := syncingTT.DeepCopy()
	ttCopy.Status = shipperv1.TrafficTargetStatus{
		Clusters: statuses,
	}
	// Until #38113 is merged, we must use Update instead of UpdateStatus to
	// update the Status block of the TrafficTarget resource. UpdateStatus will not
	// allow changes to the Spec of the resource, which is ideal for ensuring
	// nothing other than resource status has been updated.
	_, err = c.shipperclientset.ShipperV1().TrafficTargets(namespace).Update(ttCopy)
	if err != nil {
		return err
	}
	//TODO(btyler) don't record "success" if it wasn't a total success: this should include some information about how many clusters worked and how many did not
	c.recorder.Event(syncingTT, corev1.EventTypeNormal, SuccessSynced, MessageResourceSynced)
	return nil
}

// enqueueTrafficTarget takes a TrafficTarget resource and converts it into a namespace/name
// string which is then put onto the work queue. This method should *not* be
// passed resources of any type other than TrafficTarget.
func (c *Controller) enqueueTrafficTarget(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		runtime.HandleError(err)
		return
	}
	c.workqueue.AddRateLimited(key)
}
