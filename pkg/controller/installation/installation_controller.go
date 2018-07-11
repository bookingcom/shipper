package installation

import (
	"fmt"
	"sort"
	"time"

	"github.com/golang/glog"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	shipperV1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shipperChart "github.com/bookingcom/shipper/pkg/chart"
	shipper "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shipperInformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shipperListers "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1"
	"github.com/bookingcom/shipper/pkg/clusterclientstore"
	clientcache "github.com/bookingcom/shipper/pkg/clusterclientstore/cache"
	"github.com/bookingcom/shipper/pkg/conditions"
	shipperController "github.com/bookingcom/shipper/pkg/controller"
)

const (
	AgentName = "installation-controller"

	// maxRetries is the number of times an InstallationTarget will be retried
	// before we drop it out of the workqueue. The number is chosen with the
	// default rate limiter in mind. This results in the following backoff times:
	// 5ms, 10ms, 20ms, 40ms, 80ms, 160ms, 320ms, 640ms, 1.3s, 2.6s, 5.1s, 10.2s.
	maxRetries = 11
)

// Controller is a Kubernetes controller that processes InstallationTarget
// objects.
type Controller struct {
	shipperclientset shipper.Interface
	// the Kube clients for each of the target clusters
	clusterClientStore clusterclientstore.ClientProvider

	workqueue workqueue.RateLimitingInterface

	appLister                 shipperListers.ApplicationLister
	installationTargetsSynced cache.InformerSynced
	installationTargetsLister shipperListers.InstallationTargetLister
	clusterLister             shipperListers.ClusterLister
	releaseLister             shipperListers.ReleaseLister
	dynamicClientBuilderFunc  DynamicClientBuilderFunc
	chartFetchFunc            shipperChart.FetchFunc
	recorder                  record.EventRecorder
}

// NewController returns a new Installation controller.
func NewController(
	shipperclientset shipper.Interface,
	shipperInformerFactory shipperInformers.SharedInformerFactory,
	store clusterclientstore.ClientProvider,
	dynamicClientBuilderFunc DynamicClientBuilderFunc,
	chartFetchFunc shipperChart.FetchFunc,
	recorder record.EventRecorder,
) *Controller {

	// Management Cluster InstallationTarget informer
	installationTargetInformer := shipperInformerFactory.Shipper().V1().InstallationTargets()
	clusterInformer := shipperInformerFactory.Shipper().V1().Clusters()
	releaseInformer := shipperInformerFactory.Shipper().V1().Releases()
	applicationInformer := shipperInformerFactory.Shipper().V1().Applications()

	controller := &Controller{
		appLister:                 applicationInformer.Lister(),
		shipperclientset:          shipperclientset,
		clusterClientStore:        store,
		clusterLister:             clusterInformer.Lister(),
		releaseLister:             releaseInformer.Lister(),
		installationTargetsLister: installationTargetInformer.Lister(),
		installationTargetsSynced: installationTargetInformer.Informer().HasSynced,
		dynamicClientBuilderFunc:  dynamicClientBuilderFunc,
		workqueue:                 workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "installation_controller_installationtargets"),
		chartFetchFunc:            chartFetchFunc,
		recorder:                  recorder,
	}

	installationTargetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueInstallationTarget,
		UpdateFunc: func(oldObj, newObj interface{}) {
			controller.enqueueInstallationTarget(newObj)
		},
	})

	return controller
}

// Run starts Installation controller workers and blocks until stopCh is
// closed.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	glog.V(2).Info("Starting Installation controller")
	defer glog.V(2).Info("Shutting down Installation controller")

	if !cache.WaitForCacheSync(stopCh, c.installationTargetsSynced) {
		runtime.HandleError(fmt.Errorf("failed to wait for caches to sync"))
		return
	}

	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	glog.V(4).Info("Started Installation controller")

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
		runtime.HandleError(fmt.Errorf("invalid object key (will not retry): %#v", obj))
		return true
	}

	if shouldRetry := c.syncOne(key); shouldRetry {
		if c.workqueue.NumRequeues(key) >= maxRetries {
			// Drop the InstallationTarget's key out of the workqueue and thus reset its
			// backoff. This limits the time a "broken" object can hog a worker.
			glog.Warningf("CapacityTarget %q has been retried too many times, dropping from the queue", key)
			c.workqueue.Forget(key)

			return true
		}

		c.workqueue.AddRateLimited(key)

		return true
	}

	c.workqueue.Forget(obj)
	glog.V(4).Infof("Successfully synced InstallationTarget %q", key)

	return true
}

func (c *Controller) syncOne(key string) bool {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid object key (will not retry): %q", key))
		return false
	}

	it, err := c.installationTargetsLister.InstallationTargets(namespace).Get(name)
	if err != nil {
		if kerrors.IsNotFound(err) {
			glog.V(3).Infof("InstallationTarget %q has been deleted", key)
			return false
		}

		runtime.HandleError(fmt.Errorf("error syncing InstallationTarget %q (will retry): %s", key, err))
		return true
	}

	if err := c.processInstallation(it.DeepCopy()); err != nil {
		shouldNotRetry := shipperController.IsMultipleOwnerReferencesError(err) || shipperController.IsWrongOwnerReferenceError(err) || IsIncompleteReleaseError(err)

		if shouldNotRetry {
			runtime.HandleError(fmt.Errorf("error syncing InstallationTarget %q (will not retry): %s", key, err))
			return false
		}

		runtime.HandleError(fmt.Errorf("error syncing InstallationTarget %q (will retry): %s", key, err))
		return true
	}

	return false
}

func (c *Controller) enqueueInstallationTarget(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	c.workqueue.Add(key)
}

// processInstallation attempts to install the related release on all target clusters.
func (c *Controller) processInstallation(it *shipperV1.InstallationTarget) error {
	relNamespaceLister := c.releaseLister.Releases(it.Namespace)

	release, err := relNamespaceLister.ReleaseForInstallationTarget(it)
	if err != nil {
		return err
	}

	appName, ok := release.GetLabels()[shipperV1.AppLabel]
	if !ok {
		return NewIncompleteReleaseError(`couldn't find label %q in release %q`, shipperV1.AppLabel, release.Name)
	}

	contenderRel, err := relNamespaceLister.ContenderForApplication(appName)
	if err != nil {
		return err
	}

	if contenderRel.Name != release.Name {
		return NewNotContenderError(`release %q is not the contender %q`, release.Name, contenderRel.Name)
	}

	handler := NewInstaller(c.chartFetchFunc, release, it)

	// Build .status over based on the current .spec.clusters.
	newClusterStatuses := make([]*shipperV1.ClusterInstallationStatus, 0, len(it.Spec.Clusters))

	// Collect the existing conditions for clusters present in .spec.clusters in a map
	existingConditionsPerCluster := extractExistingConditionsPerCluster(it)

	// The strategy here is try our best to install as many objects as possible
	// in all target clusters. It is not the Installation Controller job to
	// reason about an application cluster status, so it just report that a
	// cluster might not be operational if operations on the application
	// cluster fail for any reason.
	for _, name := range it.Spec.Clusters {

		// IMPORTANT: Since we keep existing conditions from previous syncing
		// points (as in existingConditionsPerCluster[name]), one needs to
		// adjust all the dependent conditions. For example, whenever we
		// transition "Operational" to "False", "Ready" *MUST* be transitioned
		// to "Unknown" since we can't verify if it is actually "Ready".
		status := &shipperV1.ClusterInstallationStatus{
			Name:       name,
			Conditions: existingConditionsPerCluster[name],
		}
		newClusterStatuses = append(newClusterStatuses, status)

		var cluster *shipperV1.Cluster
		if cluster, err = c.clusterLister.Get(name); err != nil {
			status.Status = shipperV1.InstallationStatusFailed
			status.Message = err.Error()
			status.Conditions = conditions.SetInstallationCondition(status.Conditions, shipperV1.ClusterConditionTypeOperational, corev1.ConditionFalse, reasonForOperationalCondition(err), err.Error())
			status.Conditions = conditions.SetInstallationCondition(status.Conditions, shipperV1.ClusterConditionTypeReady, corev1.ConditionUnknown, reasonForReadyCondition(err), err.Error())
			glog.Warningf("Get Cluster %q for InstallationTarget %q: %s", name, shipperController.MetaKey(it), err)
			continue
		}

		var client kubernetes.Interface
		var restConfig *rest.Config
		client, restConfig, err = c.GetClusterAndConfig(name)
		if err != nil {
			status.Status = shipperV1.InstallationStatusFailed
			status.Message = err.Error()
			status.Conditions = conditions.SetInstallationCondition(status.Conditions, shipperV1.ClusterConditionTypeOperational, corev1.ConditionFalse, reasonForOperationalCondition(err), err.Error())
			status.Conditions = conditions.SetInstallationCondition(status.Conditions, shipperV1.ClusterConditionTypeReady, corev1.ConditionUnknown, reasonForReadyCondition(err), err.Error())
			glog.Warningf("Get config for Cluster %q for InstallationTarget %q: %s", name, shipperController.MetaKey(it), err)
			continue
		}

		// At this point, we got a hold in a connection to the target cluster,
		// so we assume it's operational until some other signal saying
		// otherwise arrives.
		status.Conditions = conditions.SetInstallationCondition(status.Conditions, shipperV1.ClusterConditionTypeOperational, corev1.ConditionTrue, "", "")

		if err = handler.installRelease(cluster, client, restConfig, c.dynamicClientBuilderFunc); err != nil {
			status.Status = shipperV1.InstallationStatusFailed
			status.Message = err.Error()
			status.Conditions = conditions.SetInstallationCondition(status.Conditions, shipperV1.ClusterConditionTypeReady, corev1.ConditionFalse, reasonForReadyCondition(err), err.Error())
			glog.Warningf("Install InstallationTarget %q for Cluster %q: %s", shipperController.MetaKey(it), name, err)
			continue
		}

		status.Conditions = conditions.SetInstallationCondition(status.Conditions, shipperV1.ClusterConditionTypeReady, corev1.ConditionTrue, "", "")
		status.Status = shipperV1.InstallationStatusInstalled
	}

	sort.Sort(byClusterName(newClusterStatuses))
	it.Status.Clusters = newClusterStatuses

	_, err = c.shipperclientset.ShipperV1().InstallationTargets(it.Namespace).Update(it)
	if err == nil {
		c.recorder.Eventf(
			it,
			corev1.EventTypeNormal,
			"InstallationStatusChanged",
			"Set %q status to %v",
			shipperController.MetaKey(it),
			newClusterStatuses,
		)
	} else {
		c.recorder.Event(
			it,
			corev1.EventTypeWarning,
			"FailedInstallationStatusChange",
			err.Error(),
		)
	}

	return err
}

// extractExistingConditionsPerCluster builds a map with values being a list of conditions.
func extractExistingConditionsPerCluster(it *shipperV1.InstallationTarget) map[string][]shipperV1.ClusterInstallationCondition {
	existingConditionsPerCluster := map[string][]shipperV1.ClusterInstallationCondition{}
	for _, name := range it.Spec.Clusters {
		for _, s := range it.Status.Clusters {
			if s.Name == name {
				existingConditionsPerCluster[name] = s.Conditions
			}
		}
	}
	return existingConditionsPerCluster
}

func (c *Controller) GetClusterAndConfig(clusterName string) (kubernetes.Interface, *rest.Config, error) {
	var client kubernetes.Interface
	var referenceConfig *rest.Config
	var err error

	if client, err = c.clusterClientStore.GetClient(clusterName); err != nil {
		return nil, nil, err
	}

	if referenceConfig, err = c.clusterClientStore.GetConfig(clusterName); err != nil {
		return nil, nil, err
	}

	// the client store is just like an informer cache: it's a shared pointer
	// to a read-only struct, so copy it before mutating
	referenceCopy := rest.CopyConfig(referenceConfig)

	return client, referenceCopy, nil
}

func reasonForOperationalCondition(err error) string {
	if err == clientcache.ErrClusterNotInStore || err == clientcache.ErrClusterNotReady {
		return conditions.TargetClusterClientError
	}
	return conditions.ServerError
}

func reasonForReadyCondition(err error) string {
	if IsCreateResourceError(err) || IsGetResourceError(err) {
		return conditions.ServerError
	}

	if IsDecodeManifestError(err) || IsConvertUnstructuredError(err) {
		return conditions.ChartError
	}

	if IsResourceClientError(err) {
		return conditions.ClientError
	}

	return conditions.UnknownError
}
