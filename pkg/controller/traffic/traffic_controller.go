package traffic

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperclient "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	informers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	listers "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/clusterclientstore"
	shippercontroller "github.com/bookingcom/shipper/pkg/controller"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	clusterstatusutil "github.com/bookingcom/shipper/pkg/util/clusterstatus"
	diffutil "github.com/bookingcom/shipper/pkg/util/diff"
	"github.com/bookingcom/shipper/pkg/util/filters"
	targetutil "github.com/bookingcom/shipper/pkg/util/target"
	trafficutil "github.com/bookingcom/shipper/pkg/util/traffic"
	shipperworkqueue "github.com/bookingcom/shipper/pkg/workqueue"
)

const (
	AgentName = "traffic-controller"

	ClustersNotReady   = "ClustersNotReady"
	InProgress         = "InProgress"
	InternalError      = "InternalError"
	PodsNotInEndpoints = "PodsNotInEndpoints"
	PodsNotReady       = "PodsNotReady"

	TrafficTargetConditionChanged  = "TrafficTargetConditionChanged"
	ClusterTrafficConditionChanged = "ClusterTrafficConditionChanged"
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
	store clusterclientstore.Interface,
	recorder record.EventRecorder,
) *Controller {

	// Obtain references to shared index informers for the TrafficTarget type.
	trafficTargetInformer := shipperInformerFactory.Shipper().V1alpha1().TrafficTargets()

	controller := &Controller{
		shipperclientset:   shipperclientset,
		clusterClientStore: store,

		trafficTargetsLister: trafficTargetInformer.Lister(),
		trafficTargetsSynced: trafficTargetInformer.Informer().HasSynced,
		workqueue:            workqueue.NewNamedRateLimitingQueue(shipperworkqueue.NewDefaultControllerRateLimiter(), "traffic_controller_traffictargets"),
		recorder:             recorder,
	}

	klog.Info("Setting up event handlers")
	trafficTargetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueAllTrafficTargets,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueAllTrafficTargets(new)
		},
		DeleteFunc: controller.enqueueAllTrafficTargets,
	})

	store.AddSubscriptionCallback(controller.subscribeToAppClusterEvents)
	store.AddEventHandlerCallback(controller.registerAppClusterEventHandlers)

	return controller
}

// registerAppClusterEventHandlers listens to events on both Endpoints and
// Pods. An event on an Endpoints object enqueues all traffic targets for an
// app, as a change in one of them might affect the weight in the others. For
// Pods, we only enqueue the owning traffic target, and only for adds and
// deletes, as any changes relevant for traffic will be reflected in the
// Endpoints object anyway. In case a new or deleted pod does change traffic
// shifting in any way, the update to the traffic target itself will trigger a
// new evaluation of all traffic targets for an app.
func (c *Controller) registerAppClusterEventHandlers(informerFactory kubeinformers.SharedInformerFactory, clusterName string) {
	informerFactory.Core().V1().Endpoints().Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: filters.BelongsToApp,
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    c.enqueueAllTrafficTargets,
			DeleteFunc: c.enqueueAllTrafficTargets,
			UpdateFunc: func(oldObj, newObj interface{}) {
				c.enqueueAllTrafficTargets(newObj)
			},
		},
	})

	informerFactory.Core().V1().Pods().Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: filters.BelongsToRelease,
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    c.enqueueTrafficTargetFromPod,
			DeleteFunc: c.enqueueTrafficTargetFromPod,
		},
	})
}

func (c *Controller) subscribeToAppClusterEvents(informerFactory kubeinformers.SharedInformerFactory) {
	informerFactory.Core().V1().Pods().Informer()
	informerFactory.Core().V1().Services().Informer()
	informerFactory.Core().V1().Endpoints().Informer()
}

// Run will set up the event handlers for types we are interested in, as well as
// syncing informer caches and starting workers. It will block until stopCh is
// closed, at which point it will shutdown the workqueue and wait for workers to
// finish processing their current work items.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	klog.V(2).Info("Starting Traffic controller")
	defer klog.V(2).Info("Shutting down Traffic controller")

	if ok := cache.WaitForCacheSync(stopCh, c.trafficTargetsSynced); !ok {
		runtime.HandleError(fmt.Errorf("failed to wait for caches to sync"))
		return
	}

	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	klog.V(4).Info("Started Traffic controller")

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
		c.workqueue.AddRateLimited(key)

		return true
	}

	c.workqueue.Forget(obj)
	klog.V(4).Infof("Successfully synced TrafficTarget %q", key)

	return true
}

func (c *Controller) syncHandler(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return shippererrors.NewUnrecoverableError(err)
	}

	initialTT, err := c.trafficTargetsLister.TrafficTargets(namespace).Get(name)
	if err != nil {
		if kerrors.IsNotFound(err) {
			klog.V(3).Infof("TrafficTarget %q has been deleted", key)
			return nil
		}

		return shippererrors.NewKubeclientGetError(namespace, name, err).
			WithShipperKind("TrafficTarget")
	}

	tt, err := c.processTrafficTarget(initialTT.DeepCopy())

	if !reflect.DeepEqual(initialTT, tt) {
		if _, err := c.shipperclientset.ShipperV1alpha1().TrafficTargets(namespace).UpdateStatus(tt); err != nil {
			return shippererrors.NewKubeclientUpdateError(tt, err).
				WithShipperKind("TrafficTarget")
		}
	}

	return err
}

func (c *Controller) processTrafficTarget(tt *shipper.TrafficTarget) (*shipper.TrafficTarget, error) {
	diff := diffutil.NewMultiDiff()
	defer c.reportConditionChange(tt, TrafficTargetConditionChanged, diff)

	appName, ok := tt.Labels[shipper.AppLabel]
	if !ok {
		err := shippererrors.NewMissingShipperLabelError(tt, shipper.AppLabel)
		tt.Status.Conditions = targetutil.TransitionToNotOperational(
			diff, tt.Status.Conditions,
			InternalError, err.Error())
		return tt, err
	}

	appSelector := labels.Set{shipper.AppLabel: appName}.AsSelector()
	allTTs, err := c.trafficTargetsLister.TrafficTargets(tt.Namespace).List(appSelector)
	if err != nil {
		err := shippererrors.NewKubeclientListError(
			shipper.SchemeGroupVersion.WithKind("TrafficTarget"),
			tt.Namespace, appSelector, err)
		tt.Status.Conditions = targetutil.TransitionToNotOperational(
			diff, tt.Status.Conditions,
			InternalError, err.Error())
		return tt, err
	}

	clusterReleaseWeights, err := buildClusterReleaseWeights(allTTs)
	if err != nil {
		tt.Status.Conditions = targetutil.TransitionToNotOperational(
			diff, tt.Status.Conditions,
			InternalError, err.Error())
		return tt, err
	}

	tt.Status.Conditions = targetutil.TransitionToOperational(diff, tt.Status.Conditions)

	clusterErrors := shippererrors.NewMultiError()
	newClusterStatuses := make([]*shipper.ClusterTrafficStatus, 0, len(tt.Spec.Clusters))

	// This algorithm assumes cluster names are unique
	curClusterStatuses := make(map[string]*shipper.ClusterTrafficStatus)
	for _, clusterStatus := range tt.Status.Clusters {
		curClusterStatuses[clusterStatus.Name] = clusterStatus
	}

	for _, clusterSpec := range tt.Spec.Clusters {
		clusterStatus, ok := curClusterStatuses[clusterSpec.Name]
		if !ok {
			clusterStatus = &shipper.ClusterTrafficStatus{
				Name: clusterSpec.Name,
			}
		}

		err := c.processTrafficTargetOnCluster(tt, &clusterSpec, clusterStatus, clusterReleaseWeights)
		if err != nil {
			clusterErrors.Append(err)
		}

		newClusterStatuses = append(newClusterStatuses, clusterStatus)
	}

	sort.Sort(byClusterName(newClusterStatuses))

	tt.Status.Clusters = newClusterStatuses
	tt.Status.ObservedGeneration = tt.Generation

	notReadyReasons := []string{}
	for _, clusterStatus := range tt.Status.Clusters {
		ready, reason := clusterstatusutil.IsClusterTrafficReady(clusterStatus.Conditions)
		if !ready {
			notReadyReasons = append(notReadyReasons,
				fmt.Sprintf("%s: %s", clusterStatus.Name, reason))
		}
	}

	if len(notReadyReasons) == 0 {
		tt.Status.Conditions = targetutil.TransitionToReady(diff, tt.Status.Conditions)
	} else {
		tt.Status.Conditions = targetutil.TransitionToNotReady(
			diff, tt.Status.Conditions,
			ClustersNotReady, strings.Join(notReadyReasons, "; "))
	}

	return tt, clusterErrors.Flatten()
}

func (c *Controller) processTrafficTargetOnCluster(
	tt *shipper.TrafficTarget,
	spec *shipper.ClusterTrafficTarget,
	status *shipper.ClusterTrafficStatus,
	clusterReleaseWeights clusterReleaseWeights,
) error {
	diff := diffutil.NewMultiDiff()
	operationalCond := trafficutil.NewClusterTrafficCondition(
		shipper.ClusterConditionTypeOperational,
		corev1.ConditionUnknown,
		"",
		"")
	readyCond := trafficutil.NewClusterTrafficCondition(
		shipper.ClusterConditionTypeReady,
		corev1.ConditionUnknown,
		"",
		"")

	var achievedTraffic uint32
	defer func() {
		status.AchievedTraffic = achievedTraffic

		diff.Append(trafficutil.SetClusterTrafficCondition(status, *operationalCond))
		diff.Append(trafficutil.SetClusterTrafficCondition(status, *readyCond))
		c.reportConditionChange(tt, ClusterTrafficConditionChanged, diff)
	}()

	clientset, err := c.clusterClientStore.GetClient(spec.Name, AgentName)
	if err != nil {
		operationalCond = trafficutil.NewClusterTrafficCondition(
			shipper.ClusterConditionTypeOperational,
			corev1.ConditionFalse,
			InternalError,
			err.Error(),
		)

		return err
	}

	appName := tt.Labels[shipper.AppLabel]
	releaseName := tt.Labels[shipper.ReleaseLabel]

	appPods, endpoints, err := c.getClusterObjects(spec.Name, tt.Namespace, appName)
	if err != nil {
		operationalCond = trafficutil.NewClusterTrafficCondition(
			shipper.ClusterConditionTypeOperational,
			corev1.ConditionFalse,
			InternalError,
			err.Error(),
		)

		return err
	}

	operationalCond = trafficutil.NewClusterTrafficCondition(
		shipper.ClusterConditionTypeOperational,
		corev1.ConditionTrue,
		"",
		"",
	)

	trafficStatus := buildTrafficShiftingStatus(
		spec.Name, appName, releaseName,
		clusterReleaseWeights,
		endpoints, appPods)

	// achievedTraffic is used by the defer at the top of this func
	achievedTraffic = trafficStatus.achievedTrafficWeight

	if trafficStatus.ready {
		readyCond = trafficutil.NewClusterTrafficCondition(
			shipper.ClusterConditionTypeReady,
			corev1.ConditionTrue,
			"",
			"",
		)

		return nil
	}

	if trafficStatus.podsToShift != nil {
		// If we have pods to shift, our job can only be done after the
		// change is made and observed, so we definitely still in
		// progress.
		err := shiftPodLabels(clientset, trafficStatus.podsToShift)
		if err != nil {
			readyCond = trafficutil.NewClusterTrafficCondition(
				shipper.ClusterConditionTypeReady,
				corev1.ConditionFalse,
				InternalError,
				err.Error(),
			)

			return err
		}

		readyCond = trafficutil.NewClusterTrafficCondition(
			shipper.ClusterConditionTypeReady,
			corev1.ConditionFalse,
			InProgress,
			"",
		)
	} else if trafficStatus.podsNotReady > 0 {
		// All the pods have been shifted, made it to endpoints, but
		// some aren't ready.
		msg := fmt.Sprintf(
			"%d/%d pods designated to receive traffic are not ready",
			trafficStatus.podsNotReady, trafficStatus.podsLabeled)
		readyCond = trafficutil.NewClusterTrafficCondition(
			shipper.ClusterConditionTypeReady,
			corev1.ConditionFalse,
			PodsNotReady,
			msg,
		)
	} else {
		// All the pods have been shifted, but not enough of them are
		// ready, and there are none not ready in endpoints, which
		// means that they haven't made it there yet, or that the
		// service selector does not match any pods.
		msg := fmt.Sprintf(
			"%d/%d pods designated to receive traffic are not yet in endpoints",
			trafficStatus.podsLabeled-trafficStatus.podsReady, trafficStatus.podsLabeled)
		readyCond = trafficutil.NewClusterTrafficCondition(
			shipper.ClusterConditionTypeReady,
			corev1.ConditionFalse,
			PodsNotInEndpoints,
			msg,
		)
	}

	return nil
}

func (c *Controller) getClusterObjects(cluster, ns, appName string) ([]*corev1.Pod, *corev1.Endpoints, error) {
	informerFactory, err := c.clusterClientStore.GetInformerFactory(cluster)
	if err != nil {
		return nil, nil, err
	}

	appSelector := labels.Set{shipper.AppLabel: appName}.AsSelector()
	appPods, err := informerFactory.Core().V1().Pods().Lister().
		Pods(ns).List(appSelector)
	if err != nil {
		return nil, nil, shippererrors.NewKubeclientListError(
			corev1.SchemeGroupVersion.WithKind("Pod"),
			ns, appSelector, err)
	}

	serviceSelector := labels.Set(map[string]string{
		shipper.AppLabel: appName,
		shipper.LBLabel:  shipper.LBForProduction,
	}).AsSelector()
	serviceGVK := corev1.SchemeGroupVersion.WithKind("Service")
	services, err := informerFactory.Core().V1().Services().Lister().
		Services(ns).List(serviceSelector)
	if err != nil {
		return nil, nil, shippererrors.NewKubeclientListError(
			serviceGVK, ns, serviceSelector, err)
	}

	if len(services) != 1 {
		err := shippererrors.NewUnexpectedObjectCountFromSelectorError(
			serviceSelector, serviceGVK, 1, len(services))
		return nil, nil, err
	}

	svc := services[0]

	endpoints, err := informerFactory.Core().V1().Endpoints().Lister().
		Endpoints(svc.Namespace).Get(svc.Name)
	if err != nil {
		return nil, nil, shippererrors.NewKubeclientGetError(svc.Namespace, svc.Name, err).
			WithCoreV1Kind("Endpoints")
	}

	return appPods, endpoints, nil
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

func (c *Controller) enqueueAllTrafficTargets(obj interface{}) {
	kubeobj, ok := obj.(metav1.Object)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a metav1.Object: %#v", obj))
		return
	}

	namespace := kubeobj.GetNamespace()
	appName, ok := kubeobj.GetLabels()[shipper.AppLabel]
	if !ok {
		runtime.HandleError(fmt.Errorf("object %q is missing label %s. FilterFunc not working?",
			shippercontroller.MetaKey(kubeobj), shipper.AppLabel))
		return
	}

	selector := labels.Set{shipper.AppLabel: appName}.AsSelector()
	trafficTargets, err := c.trafficTargetsLister.TrafficTargets(namespace).List(selector)
	if err != nil {
		runtime.HandleError(fmt.Errorf(
			"cannot list traffic targets for app '%s/%s': %s",
			namespace, appName, err))
		return
	}

	for _, tt := range trafficTargets {
		c.enqueueTrafficTarget(tt)
	}
}

func (c *Controller) enqueueTrafficTargetFromPod(obj interface{}) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a corev1.Pod: %#v", obj))
		return
	}

	release, ok := pod.GetLabels()[shipper.ReleaseLabel]
	if !ok {
		runtime.HandleError(fmt.Errorf(
			"object %q does not have label %s. FilterFunc not working?",
			shippercontroller.MetaKey(pod), shipper.ReleaseLabel))
		return
	}

	namespace := pod.GetNamespace()
	selector := labels.Set{shipper.ReleaseLabel: release}.AsSelector()
	gvk := shipper.SchemeGroupVersion.WithKind("TrafficTarget")
	trafficTargets, err := c.trafficTargetsLister.TrafficTargets(namespace).List(selector)
	if err != nil {
		runtime.HandleError(fmt.Errorf(
			"cannot list traffic targets for release '%s/%s': %s",
			namespace, release, err))
		return
	}

	expected := 1
	if got := len(trafficTargets); got != 1 {
		err := shippererrors.NewUnexpectedObjectCountFromSelectorError(
			selector, gvk, expected, got)
		runtime.HandleError(fmt.Errorf(
			"cannot get traffic target for release '%s/%s': %s",
			namespace, release, err))
		return
	}

	c.enqueueTrafficTarget(trafficTargets[0])
}

func (c *Controller) reportConditionChange(tt *shipper.TrafficTarget, reason string, diff diffutil.Diff) {
	if !diff.IsEmpty() {
		c.recorder.Event(tt, corev1.EventTypeNormal, reason, diff.String())
	}
}
