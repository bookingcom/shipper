package traffic

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperclient "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	informers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	listers "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/clusterclientstore"
	"github.com/bookingcom/shipper/pkg/conditions"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
)

const AgentName = "traffic-controller"

const (
	// maxRetries is the number of times a TrafficTarget will be retried before we
	// drop it out of the workqueue. The number is chosen with the default rate
	// limiter in mind. This results in the following backoff times: 5ms, 10ms,
	// 20ms, 40ms, 80ms, 160ms, 320ms, 640ms, 1.3s, 2.6s, 5.1s, 10.2s.
	maxRetries = 11
)

// Controller is the controller implementation for TrafficTarget resources.
type Controller struct {
	shipperclientset     shipperclient.Interface
	clusterClientStore   clusterclientstore.Interface
	trafficTargetsLister listers.TrafficTargetLister
	trafficTargetsSynced cache.InformerSynced
	workqueue            workqueue.RateLimitingInterface
	recorder             record.EventRecorder
}

// NewController returns a new TrafficTarget controller.
func NewController(
	shipperclientset shipperclient.Interface,
	shipperInformerFactory informers.SharedInformerFactory,
	store *clusterclientstore.Store,
	recorder record.EventRecorder,
) *Controller {

	// Obtain references to shared index informers for the TrafficTarget type.
	trafficTargetInformer := shipperInformerFactory.Shipper().V1alpha1().TrafficTargets()

	controller := &Controller{
		shipperclientset:   shipperclientset,
		clusterClientStore: store,

		trafficTargetsLister: trafficTargetInformer.Lister(),
		trafficTargetsSynced: trafficTargetInformer.Informer().HasSynced,
		workqueue:            workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "traffic_controller_traffictargets"),
		recorder:             recorder,
	}

	glog.Info("Setting up event handlers")
	// Set up an event handler for when TrafficTarget resources change.
	trafficTargetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueTrafficTarget,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueTrafficTarget(new)
		},
		// The sync handler needs to cope with the case where the object was deleted.
		DeleteFunc: controller.enqueueTrafficTarget,
	})

	store.AddSubscriptionCallback(func(informerFactory kubeinformers.SharedInformerFactory) {
		informerFactory.Core().V1().Pods().Informer()
	})

	return controller
}

// Run will set up the event handlers for types we are interested in, as well as
// syncing informer caches and starting workers. It will block until stopCh is
// closed, at which point it will shutdown the workqueue and wait for workers to
// finish processing their current work items.
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
	err := c.syncHandler(key)

	if err != nil {
		shouldRetry = shippererrors.ShouldRetry(err)
		runtime.HandleError(fmt.Errorf("error syncing TrafficTarget %q (will retry: %t): %s", key, shouldRetry, err.Error()))
	}

	if shouldRetry {
		if c.workqueue.NumRequeues(key) >= maxRetries {
			// Drop the TrafficTarget's key out of the workqueue and thus reset its
			// backoff. This limits the time a "broken" object can hog a worker.
			glog.Warningf("TrafficTarget %q has been retried too many times, dropping from the queue", key)
			c.workqueue.Forget(key)

			return true
		}

		c.workqueue.AddRateLimited(key)

		return true
	}

	c.workqueue.Forget(obj)
	glog.V(4).Infof("Successfully synced TrafficTarget %q", key)

	return true
}

func (c *Controller) syncHandler(key string) error {
	namespace, ttName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return shippererrors.NewUnrecoverableError(err)
	}

	syncingTT, err := c.trafficTargetsLister.TrafficTargets(namespace).Get(ttName)
	if err != nil {
		if kerrors.IsNotFound(err) {
			glog.V(3).Infof("TrafficTarget %q has been deleted", key)
			return nil
		}

		return shippererrors.NewKubeclientGetError(namespace, ttName, err).
			WithShipperKind("TrafficTarget")
	}

	syncingReleaseName, ok := syncingTT.Labels[shipper.ReleaseLabel]
	if !ok {
		// TODO(asurikov): log an event.
		return shippererrors.NewMissingShipperLabelError(syncingTT, shipper.ReleaseLabel)
	}

	appName, ok := syncingTT.Labels[shipper.AppLabel]
	if !ok {
		// TODO(asurikov): log an event.
		return shippererrors.NewMissingShipperLabelError(syncingTT, shipper.AppLabel)
	}

	appSelector := labels.Set{shipper.AppLabel: appName}.AsSelector()
	list, err := c.trafficTargetsLister.TrafficTargets(namespace).List(appSelector)
	if err != nil {
		return shippererrors.NewKubeclientListError(
			shipper.SchemeGroupVersion.WithKind("TrafficTarget"),
			namespace, appSelector, err)
	}

	shifter, err := newPodLabelShifter(appName, namespace, list)
	if err != nil {
		// TODO(asurikov): log an event.
		return err
	}

	var statuses []*shipper.ClusterTrafficStatus
	for _, cluster := range shifter.Clusters() {
		var achievedReleaseWeight uint32
		var achievedWeights map[string]uint32
		var clientset kubernetes.Interface
		var clusterConditions []shipper.ClusterTrafficCondition
		var errs []error
		var informerFactory kubeinformers.SharedInformerFactory

		for _, e := range syncingTT.Status.Clusters {
			if e.Name == cluster {
				clusterConditions = e.Conditions
			}
		}

		clusterStatus := &shipper.ClusterTrafficStatus{
			Name:       cluster,
			Conditions: clusterConditions,
		}

		statuses = append(statuses, clusterStatus)

		clientset, err = c.clusterClientStore.GetClient(cluster, AgentName)
		if err == nil {
			clusterStatus.Conditions = conditions.SetTrafficCondition(
				clusterStatus.Conditions,
				shipper.ClusterConditionTypeOperational,
				corev1.ConditionTrue,
				"", "")
		} else {
			clusterStatus.Conditions = conditions.SetTrafficCondition(
				clusterStatus.Conditions,
				shipper.ClusterConditionTypeOperational,
				corev1.ConditionFalse,
				conditions.ServerError,
				err.Error())

			clusterStatus.Status = err.Error()
			continue
		}
		informerFactory, err = c.clusterClientStore.GetInformerFactory(cluster)
		if err == nil {
			clusterStatus.Conditions = conditions.SetTrafficCondition(
				clusterStatus.Conditions,
				shipper.ClusterConditionTypeOperational,
				corev1.ConditionTrue,
				"", "")

		} else {
			clusterStatus.Conditions = conditions.SetTrafficCondition(
				clusterStatus.Conditions,
				shipper.ClusterConditionTypeOperational,
				corev1.ConditionFalse,
				conditions.ServerError,
				err.Error())

			clusterStatus.Status = err.Error()
			continue
		}

		achievedWeights, errs, err =
			shifter.SyncCluster(cluster, clientset, informerFactory.Core().V1().Pods())

		if err != nil {
			switch err.(type) {
			case shippererrors.TargetClusterServiceError:
				clusterStatus.Conditions = conditions.SetTrafficCondition(
					clusterStatus.Conditions,
					shipper.ClusterConditionTypeReady,
					corev1.ConditionFalse,
					conditions.MissingService,
					err.Error())
			case shippererrors.KubeclientError:
				clusterStatus.Conditions = conditions.SetTrafficCondition(
					clusterStatus.Conditions,
					shipper.ClusterConditionTypeReady,
					corev1.ConditionFalse,
					conditions.ServerError,
					err.Error())
			case shippererrors.TargetClusterMathError:
				clusterStatus.Conditions = conditions.SetTrafficCondition(
					clusterStatus.Conditions,
					shipper.ClusterConditionTypeReady,
					corev1.ConditionFalse,
					conditions.InternalError,
					err.Error())
			default:
				clusterStatus.Conditions = conditions.SetTrafficCondition(
					clusterStatus.Conditions,
					shipper.ClusterConditionTypeReady,
					corev1.ConditionFalse,
					conditions.UnknownError,
					err.Error())
			}
		} else {
			// If the resulting map is missing the release we're working on, there's a
			// significant bug in our code.
			achievedReleaseWeight = achievedWeights[syncingReleaseName]
			clusterStatus.AchievedTraffic = achievedReleaseWeight
			if len(errs) == 0 {
				clusterStatus.Conditions = conditions.SetTrafficCondition(
					clusterStatus.Conditions,
					shipper.ClusterConditionTypeReady,
					corev1.ConditionTrue,
					"", "")

				clusterStatus.Status = "Synced"
			} else {
				results := make([]string, 0, len(errs))
				for _, err := range errs {
					results = append(results, err.Error())
				}
				sort.Strings(results)
				clusterStatus.Status = strings.Join(results, ",")

				clusterStatus.Conditions = conditions.SetTrafficCondition(
					clusterStatus.Conditions,
					shipper.ClusterConditionTypeReady,
					corev1.ConditionFalse,
					conditions.PodsNotReady,
					clusterStatus.Status)
			}
		}
	}

	// At this point 'statuses' has an entry for every cluster touched by any
	// traffic target object for this application. This might be the same as the
	// syncing TT, but it could also be a superset. If it's a superset, we don't
	// want to put a bunch of extra statuses in the syncingTT status output; this
	// is confusing and breaks the strategy controller's checks (which guard
	// against unexpected statuses).
	specClusters := map[string]struct{}{}
	for _, specCluster := range syncingTT.Spec.Clusters {
		specClusters[specCluster.Name] = struct{}{}
	}

	filteredStatuses := make([]*shipper.ClusterTrafficStatus, 0, len(statuses))
	for _, statusCluster := range statuses {
		_, ok := specClusters[statusCluster.Name]
		if ok {
			filteredStatuses = append(filteredStatuses, statusCluster)
		}
	}

	ttCopy := syncingTT.DeepCopy()
	ttCopy.Status = shipper.TrafficTargetStatus{
		Clusters: filteredStatuses,
	}

	_, err = c.shipperclientset.ShipperV1alpha1().TrafficTargets(namespace).Update(ttCopy)
	if err != nil {
		return shippererrors.NewKubeclientUpdateError(ttCopy, err).
			WithShipperKind("TrafficTarget")
	}

	// TODO(btyler): don't record "success" if it wasn't a total success: this
	// should include some information about how many clusters worked and how many
	// did not.
	c.recorder.Event(syncingTT, corev1.EventTypeNormal, "Synced", "TrafficTarget synced successfully")

	return nil
}

// enqueueTrafficTarget takes a TrafficTarget resource and converts it into a
// namespace/name string which is then put onto the work queue. This method
// should *not* be passed resources of any type other than TrafficTarget.
func (c *Controller) enqueueTrafficTarget(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	c.workqueue.Add(key)
}
