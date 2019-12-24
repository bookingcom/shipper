package installation

import (
	"fmt"
	"reflect"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
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
	"github.com/bookingcom/shipper/pkg/clusterclientstore"
	shippercontroller "github.com/bookingcom/shipper/pkg/controller"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	clusterstatusutil "github.com/bookingcom/shipper/pkg/util/clusterstatus"
	diffutil "github.com/bookingcom/shipper/pkg/util/diff"
	"github.com/bookingcom/shipper/pkg/util/filters"
	installationutil "github.com/bookingcom/shipper/pkg/util/installation"
	targetutil "github.com/bookingcom/shipper/pkg/util/target"
	shipperworkqueue "github.com/bookingcom/shipper/pkg/workqueue"
)

type ChartFetcher func(i *Installer, name, version string) (*chart.Chart, error)

const (
	AgentName = "installation-controller"

	ChartError               = "ChartError"
	ClustersNotReady         = "ClustersNotReady"
	InternalError            = "InternalError"
	TargetClusterClientError = "TargetClusterClientError"
	UnknownError             = "UnknownError"

	InstallationTargetConditionChanged  = "InstallationTargetConditionChanged"
	ClusterInstallationConditionChanged = "ClusterInstallationConditionChanged"
)

// Controller is a Kubernetes controller that processes InstallationTarget
// objects.
type Controller struct {
	shipperclientset   shipperclient.Interface
	clusterClientStore clusterclientstore.Interface

	workqueue workqueue.RateLimitingInterface

	appLister                 shipperlisters.ApplicationLister
	appSynced                 cache.InformerSynced
	installationTargetsLister shipperlisters.InstallationTargetLister
	installationTargetsSynced cache.InformerSynced
	clusterLister             shipperlisters.ClusterLister
	clusterSynced             cache.InformerSynced
	releaseLister             shipperlisters.ReleaseLister
	releaseSynced             cache.InformerSynced
	dynamicClientBuilderFunc  DynamicClientBuilderFunc

	chartFetcher shipperrepo.ChartFetcher

	recorder record.EventRecorder
}

// NewController returns a new Installation controller.
func NewController(
	shipperclientset shipperclient.Interface,
	shipperInformerFactory shipperinformers.SharedInformerFactory,
	store clusterclientstore.Interface,
	dynamicClientBuilderFunc DynamicClientBuilderFunc,
	chartFetcher shipperrepo.ChartFetcher,
	recorder record.EventRecorder,
) *Controller {

	installationTargetInformer := shipperInformerFactory.Shipper().V1alpha1().InstallationTargets()
	clusterInformer := shipperInformerFactory.Shipper().V1alpha1().Clusters()
	releaseInformer := shipperInformerFactory.Shipper().V1alpha1().Releases()
	applicationInformer := shipperInformerFactory.Shipper().V1alpha1().Applications()

	controller := &Controller{
		appLister:                 applicationInformer.Lister(),
		appSynced:                 applicationInformer.Informer().HasSynced,
		shipperclientset:          shipperclientset,
		clusterClientStore:        store,
		clusterLister:             clusterInformer.Lister(),
		clusterSynced:             clusterInformer.Informer().HasSynced,
		releaseLister:             releaseInformer.Lister(),
		releaseSynced:             releaseInformer.Informer().HasSynced,
		installationTargetsLister: installationTargetInformer.Lister(),
		installationTargetsSynced: installationTargetInformer.Informer().HasSynced,
		dynamicClientBuilderFunc:  dynamicClientBuilderFunc,
		workqueue:                 workqueue.NewNamedRateLimitingQueue(shipperworkqueue.NewDefaultControllerRateLimiter(), "installation_controller_installationtargets"),
		chartFetcher:              chartFetcher,
		recorder:                  recorder,
	}

	installationTargetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueInstallationTarget,
		UpdateFunc: func(oldObj, newObj interface{}) {
			controller.enqueueInstallationTarget(newObj)
		},
	})

	store.AddSubscriptionCallback(controller.subscribeToAppClusterEvents)
	store.AddEventHandlerCallback(controller.registerAppClusterEventHandlers)

	return controller
}

func (c *Controller) registerAppClusterEventHandlers(informerFactory kubeinformers.SharedInformerFactory, clusterName string) {
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
	informerFactory.Apps().V1().Deployments().Informer().AddEventHandler(handler)
	informerFactory.Core().V1().Services().Informer().AddEventHandler(handler)
}

func (c *Controller) subscribeToAppClusterEvents(informerFactory kubeinformers.SharedInformerFactory) {
	informerFactory.Apps().V1().Deployments().Informer()
	informerFactory.Core().V1().Services().Informer()
}

func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	klog.V(2).Info("Starting Installation controller")
	defer klog.V(2).Info("Shutting down Installation controller")

	if !cache.WaitForCacheSync(stopCh, c.installationTargetsSynced, c.releaseSynced, c.appSynced, c.clusterSynced) {
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
		_, err := c.shipperclientset.ShipperV1alpha1().InstallationTargets(namespace).Update(it)
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
	defer c.reportConditionChange(it, InstallationTargetConditionChanged, diff)

	installer, err := NewInstaller(c.chartFetcher, it)
	if err != nil {
		it.Status.Conditions = targetutil.TransitionToNotOperational(
			diff, it.Status.Conditions,
			ChartError, err.Error())
		return it, err
	}

	it.Status.Conditions = targetutil.TransitionToOperational(diff, it.Status.Conditions)

	newClusterStatuses := make([]*shipper.ClusterInstallationStatus, 0, len(it.Spec.Clusters))
	clusterErrors := shippererrors.NewMultiError()

	curClusterStatuses := make(map[string]*shipper.ClusterInstallationStatus)
	for _, clusterStatus := range it.Status.Clusters {
		curClusterStatuses[clusterStatus.Name] = clusterStatus
	}

	for _, clusterName := range it.Spec.Clusters {
		clusterStatus, ok := curClusterStatuses[clusterName]
		if !ok {
			clusterStatus = &shipper.ClusterInstallationStatus{
				Name: clusterName,
			}
		}

		err := c.processInstallationTargetOnCluster(it, clusterName, clusterStatus, installer)
		if err != nil {
			clusterErrors.Append(err)
		}

		newClusterStatuses = append(newClusterStatuses, clusterStatus)
	}

	sort.Sort(byClusterName(newClusterStatuses))

	it.Status.Clusters = newClusterStatuses
	if !clusterErrors.Any() {
		it.Spec.CanOverride = false
	}

	clustersNotReady := []string{}
	for _, clusterStatus := range it.Status.Clusters {
		if !clusterstatusutil.IsClusterInstallationReady(clusterStatus.Conditions) {
			clustersNotReady = append(clustersNotReady, clusterStatus.Name)
		}
	}

	if len(clustersNotReady) == 0 {
		it.Status.Conditions = targetutil.TransitionToReady(diff, it.Status.Conditions)
	} else {
		it.Status.Conditions = targetutil.TransitionToNotReady(
			diff, it.Status.Conditions,
			ClustersNotReady, fmt.Sprintf("%v", clustersNotReady))
	}

	return it, clusterErrors.Flatten()
}

func (c *Controller) processInstallationTargetOnCluster(
	it *shipper.InstallationTarget,
	clusterName string,
	status *shipper.ClusterInstallationStatus,
	installer *Installer,
) error {
	diff := diffutil.NewMultiDiff()
	operationalCond := installationutil.NewClusterInstallationCondition(
		shipper.ClusterConditionTypeOperational,
		corev1.ConditionUnknown,
		"",
		"")
	readyCond := installationutil.NewClusterInstallationCondition(
		shipper.ClusterConditionTypeReady,
		corev1.ConditionUnknown,
		"",
		"")

	defer func() {
		diff.Append(installationutil.SetClusterInstallationCondition(status, *operationalCond))
		diff.Append(installationutil.SetClusterInstallationCondition(status, *readyCond))
		c.reportConditionChange(it, ClusterInstallationConditionChanged, diff)
	}()

	cluster, err := c.clusterLister.Get(clusterName)
	if err != nil {
		err = shippererrors.NewKubeclientGetError("", clusterName, err).
			WithShipperKind("Cluster")

		operationalCond = installationutil.NewClusterInstallationCondition(
			shipper.ClusterConditionTypeOperational,
			corev1.ConditionFalse,
			InternalError,
			err.Error(),
		)

		return err
	}

	client, restConfig, err := c.GetClusterAndConfig(clusterName)
	if err != nil {
		operationalCond = installationutil.NewClusterInstallationCondition(
			shipper.ClusterConditionTypeOperational,
			corev1.ConditionFalse,
			InternalError,
			err.Error(),
		)

		return err
	}

	operationalCond = installationutil.NewClusterInstallationCondition(
		shipper.ClusterConditionTypeOperational,
		corev1.ConditionTrue,
		"",
		"",
	)

	err = installer.install(cluster, client, restConfig, c.dynamicClientBuilderFunc)
	if err != nil {
		readyCond = installationutil.NewClusterInstallationCondition(
			shipper.ClusterConditionTypeReady,
			corev1.ConditionFalse,
			reasonForReadyCondition(err),
			err.Error(),
		)

		return err
	}

	readyCond = installationutil.NewClusterInstallationCondition(
		shipper.ClusterConditionTypeReady,
		corev1.ConditionTrue,
		"",
		"",
	)

	return nil
}

func (c *Controller) GetClusterAndConfig(clusterName string) (kubernetes.Interface, *rest.Config, error) {
	client, err := c.clusterClientStore.GetClient(clusterName, AgentName)
	if err != nil {
		return nil, nil, err
	}

	referenceConfig, err := c.clusterClientStore.GetConfig(clusterName)
	if err != nil {
		return nil, nil, err
	}

	// The client store is just like an informer cache: it's a shared pointer to a
	// read-only struct, so copy it before mutating.
	referenceCopy := rest.CopyConfig(referenceConfig)

	return client, referenceCopy, nil
}

func reasonForReadyCondition(err error) string {
	if shippererrors.IsKubeclientError(err) {
		return InternalError
	}

	if shippererrors.IsDecodeManifestError(err) || shippererrors.IsConvertUnstructuredError(err) || shippererrors.IsInvalidChartError(err) {
		return ChartError
	}

	if shippererrors.IsClusterClientStoreError(err) {
		return TargetClusterClientError
	}

	return UnknownError
}

func (c *Controller) reportConditionChange(ct *shipper.InstallationTarget, reason string, diff diffutil.Diff) {
	if !diff.IsEmpty() {
		c.recorder.Event(ct, corev1.EventTypeNormal, reason, diff.String())
	}
}
