package traffic

import (
	"fmt"
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
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
	targetutil "github.com/bookingcom/shipper/pkg/util/target"
	shipperworkqueue "github.com/bookingcom/shipper/pkg/workqueue"
)

const (
	AgentName = "traffic-controller"

	InProgress         = "InProgress"
	InternalError      = "InternalError"
	PodsNotInEndpoints = "PodsNotInEndpoints"
	PodsNotReady       = "PodsNotReady"

	TrafficTargetConditionChanged = "TrafficTargetConditionChanged"
)

// Controller is the controller implementation for TrafficTarget resources.
type Controller struct {
	shipperClient shipperclient.Interface
	kubeClient    kubernetes.Interface

	trafficTargetsLister listers.TrafficTargetLister
	trafficTargetsSynced cache.InformerSynced

	podsLister corelisters.PodLister
	podsSynced cache.InformerSynced

	servicesLister corelisters.ServiceLister
	servicesSynced cache.InformerSynced

	endpointsLister corelisters.EndpointsLister
	endpointsSynced cache.InformerSynced

	workqueue workqueue.RateLimitingInterface
	recorder  record.EventRecorder
}

// NewController returns a new TrafficTarget controller.
func NewController(
	kubeClient kubernetes.Interface,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	shipperClient shipperclient.Interface,
	shipperInformerFactory informers.SharedInformerFactory,
	recorder record.EventRecorder,
) *Controller {
	trafficTargetInformer := shipperInformerFactory.Shipper().V1alpha1().TrafficTargets()
	podsInformer := kubeInformerFactory.Core().V1().Pods()
	servicesInformer := kubeInformerFactory.Core().V1().Services()
	endpointsInformer := kubeInformerFactory.Core().V1().Endpoints()

	controller := &Controller{
		shipperClient: shipperClient,
		kubeClient:    kubeClient,

		trafficTargetsLister: trafficTargetInformer.Lister(),
		trafficTargetsSynced: trafficTargetInformer.Informer().HasSynced,

		podsLister: podsInformer.Lister(),
		podsSynced: podsInformer.Informer().HasSynced,

		servicesLister: servicesInformer.Lister(),
		servicesSynced: servicesInformer.Informer().HasSynced,

		endpointsLister: endpointsInformer.Lister(),
		endpointsSynced: endpointsInformer.Informer().HasSynced,

		workqueue: workqueue.NewNamedRateLimitingQueue(shipperworkqueue.NewDefaultControllerRateLimiter(), "traffic_controller_traffictargets"),
		recorder:  recorder,
	}

	trafficTargetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueAllTrafficTargets,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueAllTrafficTargets(new)
		},
		DeleteFunc: controller.enqueueAllTrafficTargets,
	})

	// an event on an Endpoints object enqueues all traffic targets for an
	// app, as a change in one of them might affect the weight in the others. For
	// Pods, we only enqueue the owning traffic target, and only for adds and
	// deletes, as any changes relevant for traffic will be reflected in the
	// Endpoints object anyway. In case a new or deleted pod does change traffic
	// shifting in any way, the update to the traffic target itself will trigger a
	// new evaluation of all traffic targets for an app.
	endpointsInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: filters.BelongsToApp,
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    controller.enqueueAllTrafficTargets,
			DeleteFunc: controller.enqueueAllTrafficTargets,
			UpdateFunc: func(oldObj, newObj interface{}) {
				controller.enqueueAllTrafficTargets(newObj)
			},
		},
	})

	podsInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: filters.BelongsToRelease,
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    controller.enqueueTrafficTargetFromPod,
			DeleteFunc: controller.enqueueTrafficTargetFromPod,
		},
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
		if _, err := c.shipperClient.ShipperV1alpha1().TrafficTargets(namespace).UpdateStatus(tt); err != nil {
			return shippererrors.NewKubeclientUpdateError(tt, err).
				WithShipperKind("TrafficTarget")
		}
	}

	return err
}

func (c *Controller) processTrafficTarget(tt *shipper.TrafficTarget) (*shipper.TrafficTarget, error) {
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

	var achievedTraffic uint32

	defer func() {
		var d diffutil.Diff

		tt.Status.Conditions, d = targetutil.SetTargetCondition(tt.Status.Conditions, operationalCond)
		diff.Append(d)

		tt.Status.Conditions, d = targetutil.SetTargetCondition(tt.Status.Conditions, readyCond)
		diff.Append(d)

		tt.Status.ObservedGeneration = tt.Generation
		tt.Status.AchievedTraffic = achievedTraffic

		if !diff.IsEmpty() {
			c.recorder.Event(tt, corev1.EventTypeNormal, TrafficTargetConditionChanged, diff.String())
		}
	}()

	appName, err := objectutil.GetApplicationLabel(tt)
	if err != nil {
		tt.Status.Conditions = targetutil.TransitionToNotOperational(
			diff, tt.Status.Conditions,
			InternalError, err.Error())
		return tt, err
	}

	releaseName, err := objectutil.GetReleaseLabel(tt)
	if err != nil {
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

	releaseWeights, err := buildReleaseWeights(allTTs)
	if err != nil {
		tt.Status.Conditions = targetutil.TransitionToNotOperational(
			diff, tt.Status.Conditions,
			InternalError, err.Error())
		return tt, err
	}

	appPods, endpoints, err := c.getClusterObjects(tt)
	if err != nil {
		operationalCond = targetutil.NewTargetCondition(
			shipper.TargetConditionTypeOperational,
			corev1.ConditionFalse,
			InternalError,
			err.Error(),
		)

		return tt, err
	}

	operationalCond = targetutil.NewTargetCondition(
		shipper.TargetConditionTypeOperational,
		corev1.ConditionTrue,
		"",
		"",
	)

	trafficStatus := buildTrafficShiftingStatus(
		appName, releaseName,
		releaseWeights,
		endpoints, appPods)

	// achievedTraffic is used by the defer at the top of this func
	achievedTraffic = trafficStatus.achievedTrafficWeight

	if trafficStatus.ready {
		readyCond = targetutil.NewTargetCondition(
			shipper.TargetConditionTypeReady,
			corev1.ConditionTrue,
			"",
			"",
		)

		return tt, nil
	}

	if trafficStatus.podsToShift != nil {
		// If we have pods to shift, our job can only be done after the
		// change is made and observed, so we definitely still in
		// progress.
		err := shiftPodLabels(c.kubeClient, trafficStatus.podsToShift)
		if err != nil {
			readyCond = targetutil.NewTargetCondition(
				shipper.TargetConditionTypeReady,
				corev1.ConditionFalse,
				InternalError,
				err.Error(),
			)

			return tt, err
		}

		readyCond = targetutil.NewTargetCondition(
			shipper.TargetConditionTypeReady,
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
		readyCond = targetutil.NewTargetCondition(
			shipper.TargetConditionTypeReady,
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
		readyCond = targetutil.NewTargetCondition(
			shipper.TargetConditionTypeReady,
			corev1.ConditionFalse,
			PodsNotInEndpoints,
			msg,
		)
	}

	return tt, nil
}

func (c *Controller) getClusterObjects(tt *shipper.TrafficTarget) ([]*corev1.Pod, *corev1.Endpoints, error) {
	appName, _ := objectutil.GetApplicationLabel(tt)
	appSelector := labels.Set{shipper.AppLabel: appName}.AsSelector()
	appPods, err := c.podsLister.Pods(tt.Namespace).List(appSelector)
	if err != nil {
		return nil, nil, shippererrors.NewKubeclientListError(
			corev1.SchemeGroupVersion.WithKind("Pod"),
			tt.Namespace, appSelector, err)
	}

	serviceSelector := labels.Set(map[string]string{
		shipper.AppLabel: appName,
		shipper.LBLabel:  shipper.LBForProduction,
	}).AsSelector()
	serviceGVK := corev1.SchemeGroupVersion.WithKind("Service")
	services, err := c.servicesLister.Services(tt.Namespace).List(serviceSelector)
	if err != nil {
		return nil, nil, shippererrors.NewKubeclientListError(
			serviceGVK, tt.Namespace, serviceSelector, err)
	}

	if len(services) != 1 {
		err := shippererrors.NewUnexpectedObjectCountFromSelectorError(
			serviceSelector, serviceGVK, 1, len(services))
		return nil, nil, err
	}

	svc := services[0]

	endpoints, err := c.endpointsLister.Endpoints(svc.Namespace).Get(svc.Name)
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
	appName, err := objectutil.GetApplicationLabel(kubeobj)
	if err != nil {
		runtime.HandleError(fmt.Errorf(
			"object %q does not belong to an application. FilterFunc not working?",
			objectutil.MetaKey(kubeobj)))
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

	release, err := objectutil.GetReleaseLabel(pod)
	if err != nil {
		runtime.HandleError(fmt.Errorf(
			"pod %q does not belong to a release. FilterFunc not working?",
			objectutil.MetaKey(pod)))
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
