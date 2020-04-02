package installation

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
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/klog"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperrepo "github.com/bookingcom/shipper/pkg/chart/repo"
	shipperclient "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shipperlisters "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1alpha1"
	shippercontroller "github.com/bookingcom/shipper/pkg/controller"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	diffutil "github.com/bookingcom/shipper/pkg/util/diff"
	"github.com/bookingcom/shipper/pkg/util/filters"
	targetutil "github.com/bookingcom/shipper/pkg/util/target"
	shipperworkqueue "github.com/bookingcom/shipper/pkg/workqueue"
)

type ChartFetcher func(i *Installer, name, version string) (*chart.Chart, error)

const (
	AgentName = "installation-controller"

	ChartError       = "ChartError"
	ClustersNotReady = "ClustersNotReady"
	InternalError    = "InternalError"
	UnknownError     = "UnknownError"

	InstallationTargetConditionChanged = "InstallationTargetConditionChanged"
)

// Controller is a Kubernetes controller that processes InstallationTarget
// objects.
type Controller struct {
	shipperClient shipperclient.Interface
	kubeClient    kubernetes.Interface

	installationTargetsLister shipperlisters.InstallationTargetLister
	installationTargetsSynced cache.InformerSynced

	dynamicClientBuilderFunc DynamicClientBuilderFunc

	workqueue workqueue.RateLimitingInterface

	chartFetcher shipperrepo.ChartFetcher

	recorder record.EventRecorder
}

// NewController returns a new Installation controller.
func NewController(
	kubeClient kubernetes.Interface,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	shipperClient shipperclient.Interface,
	shipperInformerFactory shipperinformers.SharedInformerFactory,
	dynamicClientBuilderFunc DynamicClientBuilderFunc,
	chartFetcher shipperrepo.ChartFetcher,
	recorder record.EventRecorder,
) *Controller {

	itInformer := shipperInformerFactory.Shipper().V1alpha1().InstallationTargets()

	controller := &Controller{
		shipperClient:             shipperClient,
		kubeClient:                kubeClient,
		installationTargetsLister: itInformer.Lister(),
		installationTargetsSynced: itInformer.Informer().HasSynced,
		dynamicClientBuilderFunc:  dynamicClientBuilderFunc,
		workqueue:                 workqueue.NewNamedRateLimitingQueue(shipperworkqueue.NewDefaultControllerRateLimiter(), "installation_controller_installationtargets"),
		chartFetcher:              chartFetcher,
		recorder:                  recorder,
	}

	itInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueInstallationTarget,
		UpdateFunc: func(oldObj, newObj interface{}) {
			controller.enqueueInstallationTarget(newObj)
		},
	})

	handler := cache.FilteringResourceEventHandler{
		FilterFunc: filters.BelongsToRelease,
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    controller.enqueueInstallationTargetFromObject,
			DeleteFunc: controller.enqueueInstallationTargetFromObject,
			UpdateFunc: func(oldObj, newObj interface{}) {
				controller.enqueueInstallationTargetFromObject(newObj)
			},
		},
	}
	kubeInformerFactory.Apps().V1().Deployments().Informer().AddEventHandler(handler)
	kubeInformerFactory.Core().V1().Services().Informer().AddEventHandler(handler)

	return controller
}

func (c *Controller) registerAppClusterEventHandlers(kubeInformerFactory kubeinformers.SharedInformerFactory, shipperInformerFactory shipperinformers.SharedInformerFactory, clusterName string) {
	handler := cache.FilteringResourceEventHandler{
		FilterFunc: filters.BelongsToRelease,
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    c.enqueueInstallationTargetFromObject,
			DeleteFunc: c.enqueueInstallationTargetFromObject,
			UpdateFunc: func(oldObj, newObj interface{}) {
				c.enqueueInstallationTargetFromObject(newObj)
			},
		},
	}
	kubeInformerFactory.Apps().V1().Deployments().Informer().AddEventHandler(handler)
	kubeInformerFactory.Core().V1().Services().Informer().AddEventHandler(handler)
}

func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	klog.V(2).Info("Starting Installation controller")
	defer klog.V(2).Info("Shutting down Installation controller")

	if !cache.WaitForCacheSync(stopCh, c.installationTargetsSynced) {
		runtime.HandleError(fmt.Errorf("failed to wait for caches to sync"))
		return
	}

	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	klog.V(4).Info("Started Installation controller")

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
		runtime.HandleError(fmt.Errorf("error syncing InstallationTarget%q (will retry: %t): %s", key, shouldRetry, err.Error()))
	}

	if shouldRetry {
		c.workqueue.AddRateLimited(key)

		return true
	}

	c.workqueue.Forget(obj)
	klog.V(4).Infof("Successfully synced InstallationTarget %q", key)

	return true
}

func (c *Controller) syncHandler(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return shippererrors.NewUnrecoverableError(err)
	}

	initialIT, err := c.installationTargetsLister.InstallationTargets(namespace).Get(name)
	if err != nil {
		if kerrors.IsNotFound(err) {
			klog.V(3).Infof("InstallationTarget %q has been deleted", key)
			return nil
		}

		return shippererrors.NewKubeclientGetError(namespace, name, err).
			WithShipperKind("InstallationTarget")
	}

	it, err := c.processInstallationTarget(initialIT.DeepCopy())

	if !reflect.DeepEqual(initialIT, it) {
		// NOTE(jgreff): we can't use .UpdateStatus() because we also
		// need to update .Spec.CanOverride
		_, err := c.shipperClient.ShipperV1alpha1().InstallationTargets(namespace).Update(it)
		if err != nil {
			return shippererrors.NewKubeclientUpdateError(it, err).
				WithShipperKind("InstallationTarget")
		}
	}

	return err
}

func (c *Controller) enqueueInstallationTarget(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	c.workqueue.Add(key)
}

func (c *Controller) enqueueInstallationTargetFromObject(obj interface{}) {
	kubeobj, ok := obj.(metav1.Object)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a metav1.Object: %#v", obj))
		return
	}

	// Using ReleaseLabel here instead of the full set of labels because we
	// can't guarantee that there isn't extra stuff there that was put
	// directly in the chart.
	// Also not using ObjectReference here because it would go over cluster
	// boundaries. While technically it's probably ok, I feel like it'd be
	// abusing the feature.
	rel, ok := kubeobj.GetLabels()[shipper.ReleaseLabel]
	if !ok {
		runtime.HandleError(fmt.Errorf(
			"object %q does not have label %s. FilterFunc not working?",
			shippercontroller.MetaKey(kubeobj), shipper.ReleaseLabel))
		return
	}

	it, err := c.getInstallationTargetForReleaseAndNamespace(rel, kubeobj.GetNamespace())
	if err != nil {
		runtime.HandleError(fmt.Errorf("cannot get installation target for release '%s/%s': %#v", kubeobj.GetNamespace(), rel, err))
		return
	}

	c.enqueueInstallationTarget(it)
}

func (c *Controller) getInstallationTargetForReleaseAndNamespace(release, namespace string) (*shipper.InstallationTarget, error) {
	selector := labels.Set{shipper.ReleaseLabel: release}.AsSelector()
	gvk := shipper.SchemeGroupVersion.WithKind("InstallationTarget")

	installationTargets, err := c.installationTargetsLister.InstallationTargets(namespace).List(selector)
	if err != nil {
		return nil, shippererrors.NewKubeclientListError(gvk, namespace, selector, err)
	}

	expected := 1
	if got := len(installationTargets); got != 1 {
		return nil, shippererrors.NewUnexpectedObjectCountFromSelectorError(
			selector, gvk, expected, got)
	}

	return installationTargets[0], nil
}

// processInstallationTarget attempts to install the related InstallationTarget on
// all target clusters.
func (c *Controller) processInstallationTarget(it *shipper.InstallationTarget) (*shipper.InstallationTarget, error) {
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

	defer func() {
		var d diffutil.Diff

		it.Status.Conditions, d = targetutil.SetTargetCondition(it.Status.Conditions, operationalCond)
		diff.Append(d)

		it.Status.Conditions, d = targetutil.SetTargetCondition(it.Status.Conditions, readyCond)
		diff.Append(d)

		if !diff.IsEmpty() {
			c.recorder.Event(it, corev1.EventTypeNormal, InstallationTargetConditionChanged, diff.String())
		}
	}()

	objects, err := FetchAndRenderChart(c.chartFetcher, it)
	if err != nil {
		operationalCond = targetutil.NewTargetCondition(
			shipper.TargetConditionTypeOperational,
			corev1.ConditionFalse,
			ChartError,
			err.Error())

		return it, err
	}

	operationalCond = targetutil.NewTargetCondition(
		shipper.TargetConditionTypeOperational,
		corev1.ConditionTrue,
		"",
		"")

	installer := NewInstaller(it, objects)
	installerErr := installer.install(c.kubeClient, c.dynamicClientBuilderFunc)
	if installerErr != nil {
		readyCond = targetutil.NewTargetCondition(
			shipper.TargetConditionTypeReady,
			corev1.ConditionFalse,
			reasonForReadyCondition(err),
			installerErr.Error())

		return it, err
	}

	it.Spec.CanOverride = false
	readyCond = targetutil.NewTargetCondition(
		shipper.TargetConditionTypeReady,
		corev1.ConditionTrue,
		"",
		"")

	return it, nil
}

func reasonForReadyCondition(err error) string {
	if shippererrors.IsKubeclientError(err) {
		return InternalError
	}

	if shippererrors.IsDecodeManifestError(err) || shippererrors.IsConvertUnstructuredError(err) || shippererrors.IsInvalidChartError(err) {
		return ChartError
	}

	return UnknownError
}
