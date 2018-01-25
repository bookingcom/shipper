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
	"sync"
	"time"

	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	//"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	corev1informer "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	//shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shipper "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shipperscheme "github.com/bookingcom/shipper/pkg/client/clientset/versioned/scheme"
	informers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	listers "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1"
)

const controllerAgentName = "traffic-controller"

const (
	// SuccessSynced is used as part of the Event 'reason' when a TrafficTarget is synced
	SuccessSynced = "Synced"

	// MessageResourceSynced is used as part of the 'Event' message when a TrafficTarget is synced
	MessageResourceSynced = "TrafficTarget synced successfully"
)

// Controller is the controller implementation for TrafficTarget resources
type Controller struct {
	// kubeclientset is a standard kubernetes clientset
	kubeclientset kubernetes.Interface
	// shipperclientset is a clientset for our own API group
	shipperclientset shipper.Interface

	// the Kube clients for each of the target clusters
	clusterClients    map[string]kubernetes.Interface
	clusterClientsMut sync.RWMutex

	clusterPodInformers map[string]corev1informer.PodInformer

	clusterPodInformersMut sync.RWMutex

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
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	shipperInformerFactory informers.SharedInformerFactory) *Controller {

	// obtain references to shared index informers for the TrafficTarget type
	trafficTargetInformer := shipperInformerFactory.Shipper().V1().TrafficTargets()

	// obtain references to shared index informers for the Cluster type
	// clusterInformer := shipperInformerFactory.Shipper().V1().Clusters()

	// Create event broadcaster
	// Add sample-controller types to the default Kubernetes Scheme so Events can be
	// logged for sample-controller types.
	shipperscheme.AddToScheme(scheme.Scheme)
	glog.V(4).Info("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})

	controller := &Controller{
		kubeclientset:    kubeclientset,
		shipperclientset: shipperclientset,
		/*
			NOTE: we're hardcoding clients and informers for target clusters
			for the time being. These are already-configured clients (in
			cmd/traffic/main.go) for the local  minikube. Eventually these maps
			will be guarded by a mutex and updated dynamically as clusters
			come and go. For now, we're just using the local cluster to avoid
			investing time into credential management.
		*/
		clusterClients: map[string]kubernetes.Interface{
			"local": kubeclientset,
		},
		clusterPodInformers: map[string]corev1informer.PodInformer{
			"local": kubeInformerFactory.Core().V1().Pods(),
		},

		//clusterInformers map[string] kubeinformers.SharedInformerFactory
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

	/*
		clusterInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: controller.setCluster,
			UpdateFunc: func(old, new interface{}) {
				controller.setCluster(new)
			},
			// the syncHandler needs to cope with the case where the object was deleted
			DeleteFunc: controller.deleteCluster,
		})
	*/

	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	// Start the informer factories to begin populating the informer caches
	glog.Info("Starting TrafficTarget controller")

	// Wait for the caches to be synced before starting workers
	glog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.trafficTargetsSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	glog.Info("Starting workers")
	// Launch two workers to process TrafficTarget resources
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	glog.Info("Started workers")
	<-stopCh
	glog.Info("Shutting down workers")

	return nil
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
// - For each TT, query capacity in target cluster.
// - For each cluster, compute pod label %s according to TT percentage. Error or something if sum of TT percentages is != 100.
// - For each cluster, add/remove pod labels accordingly (if too many, remove until correct and visa versa)
func (c *Controller) syncHandler(key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, _, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	// Get the TrafficTarget resource with this namespace/name
	list, err := c.trafficTargetsLister.TrafficTargets(namespace).List(labels.Everything())
	if err != nil {
		return err
	}

	clusterReleaseTraffic := map[string]map[string]uint{}
	for _, tt := range list {
		release, ok := tt.Labels["release"]
		if !ok {
			return fmt.Errorf(
				"TrafficTarget '%s/%s' needs a 'release' label in order to select resources in the target clusters.",
				namespace, tt.Name,
			)
		}

		/*
			{
				cluster-1: {
					reviewsapi-1: 90,
					reviewsapi-2: 5,
					reviewsapi-3: 5,
				}
			}

		*/
		for _, cluster := range tt.Spec.Clusters {
			clusterTraffic, ok := clusterReleaseTraffic[cluster.Name]
			if !ok {
				clusterReleaseTraffic[cluster.Name] = map[string]uint{}
			}
			clusterTraffic[release] += cluster.TargetTraffic
		}
	}

	// build up a map of errors so we can report per-cluster status
	errs := map[string]error{}
	for cluster, releases := range clusterReleaseTraffic {
		var clusterTraffic uint = 0
		for _, traffic := range releases {
			clusterTraffic += traffic
		}

		if clusterTraffic != 100 {
			return fmt.Errorf("%s TrafficTargets: cluster traffic must sum to 100%. %q sums to %q", namespace, cluster, clusterTraffic)
		}

		err := c.syncClusterTraffic(cluster, releases)
		if err != nil {
			errs[cluster] = err
		}
	}

	// Finally, we update the status block of the TrafficTarget resource to reflect the
	// current state of the world

	// NEVER modify objects from the store. It's a read-only, local cache.
	// You can use DeepCopy() to make a deep copy of original object and modify this copy
	// Or create a copy manually for better performance
	/*
		ttCopy := tt.DeepCopy()
		ttCopy.Status = shipperv1.TrafficTargetStatus{
			Clusters: []shipperv1.ClusterTrafficStatus{
				{
					Name:            "local",
					AchievedTraffic: 22,
					Status:          "aaaagh",
				},
			},
		}
		// Until #38113 is merged, we must use Update instead of UpdateStatus to
		// update the Status block of the TrafficTarget resource. UpdateStatus will not
		// allow changes to the Spec of the resource, which is ideal for ensuring
		// nothing other than resource status has been updated.
		_, err = c.shipperclientset.ShipperV1().TrafficTargets(tt.Namespace).Update(ttCopy)

		if err != nil {
			return err
		}
	*/

	//c.recorder.Event(tt, corev1.EventTypeNormal, SuccessSynced, MessageResourceSynced)
	return nil
}

// syncClusterTraffic manipulates a single cluster to put it into the desired traffic state
func (c *Controller) syncClusterTraffic(cluster string, releases map[string]uint) error {
	c.clusterClientsMut.RLock()
	defer c.clusterClientsMut.RUnlock()
	// clientset
	_, ok := c.clusterClients[cluster]
	if !ok {
		return fmt.Errorf("No such cluster %q", cluster)
	}
	// check that service obj is in place and has the right bits
	// query for pods for each release that we had a TT for, hang on to them
	//
	//releasePodCounts := map[string]uint{}
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
