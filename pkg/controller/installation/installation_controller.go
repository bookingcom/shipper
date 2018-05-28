package installation

import (
	"fmt"
	"sort"
	"time"

	"github.com/golang/glog"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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

const AgentName = "installation-controller"

// Controller is a Kubernetes controller that processes InstallationTarget
// objects.
type Controller struct {
	shipperclientset shipper.Interface
	// the Kube clients for each of the target clusters
	clusterClientStore clusterclientstore.ClientProvider

	workqueue                 workqueue.RateLimitingInterface
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

	controller := &Controller{
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

	err := func(obj interface{}) error {
		defer c.workqueue.Done(obj)
		var (
			key string
			ok  bool
		)
		if key, ok = obj.(string); !ok {
			// Do not attempt to process invalid objects again.
			c.workqueue.Forget(obj)
			runtime.HandleError(fmt.Errorf("invalid object key: %#v", obj))
			return nil
		}
		if err := c.syncOne(key); err != nil {
			return fmt.Errorf("error syncing %q: %s", key, err)
		}

		// Do not requeue this object because it's already processed.
		c.workqueue.Forget(obj)
		glog.Infof("Successfully synced %q", key)
		return nil
	}(obj)

	if err != nil {
		runtime.HandleError(err)
		return true
	}

	return true
}

func (c *Controller) syncOne(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	it, err := c.installationTargetsLister.InstallationTargets(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			runtime.HandleError(fmt.Errorf("InstallationTarget %q has been deleted", key))
			return nil
		}
		return err
	}

	return c.processInstallation(it.DeepCopy())
}

func (c *Controller) enqueueInstallationTarget(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		runtime.HandleError(err)
		return
	}
	c.workqueue.AddRateLimited(key)
}

// processInstallation attempts to install the related release on all target clusters.
func (c *Controller) processInstallation(it *shipperV1.InstallationTarget) error {
	if n := len(it.OwnerReferences); n != 1 {
		return fmt.Errorf("expected exactly one owner for InstallationTarget %q, got %d", it.GetName(), n)
	}

	owner := it.OwnerReferences[0]

	release, err := c.releaseLister.Releases(it.GetNamespace()).Get(owner.Name)
	if err != nil {
		return err
	} else if release.GetUID() != owner.UID {
		return fmt.Errorf(
			"the owner Release for InstallationTarget %q is gone; expected UID %s but got %s",
			shipperController.MetaKey(it),
			owner.UID,
			release.GetUID(),
		)
	}

	handler := NewInstaller(c.chartFetchFunc, release, it)

	// The strategy here is try our best to install as many objects as possible
	// in all target clusters. It is not the Installation Controller job to
	// reason about a target cluster status.
	clusterStatuses := make([]*shipperV1.ClusterInstallationStatus, 0, len(it.Spec.Clusters))
	for _, clusterName := range it.Spec.Clusters {
		var clusterConditions []shipperV1.ClusterInstallationCondition

		for _, x := range it.Status.Clusters {
			if x.Name == clusterName {
				clusterConditions = x.Conditions
			}
		}

		status := &shipperV1.ClusterInstallationStatus{
			Name:       clusterName,
			Conditions: clusterConditions,
		}

		clusterStatuses = append(clusterStatuses, status)

		var cluster *shipperV1.Cluster
		if cluster, err = c.clusterLister.Get(clusterName); err != nil {
			status.Conditions = conditions.SetInstallationCondition(
				status.Conditions,
				shipperV1.ClusterConditionTypeOperational,
				corev1.ConditionFalse,
				conditions.ServerError,
				err.Error())

			status.Status = shipperV1.InstallationStatusFailed
			status.Message = err.Error()
			glog.Warningf("Get Cluster %q for InstallationTarget %q: %s", clusterName, shipperController.MetaKey(it), err)
			continue
		}

		var client kubernetes.Interface
		var restConfig *rest.Config
		client, restConfig, err = c.GetClusterAndConfig(clusterName)
		if err != nil {
			reason := conditions.ServerError
			if err == clientcache.ErrClusterNotInStore || err == clientcache.ErrClusterNotReady {
				reason = conditions.TargetClusterClientError
			}
			status.Conditions = conditions.SetInstallationCondition(
				status.Conditions,
				shipperV1.ClusterConditionTypeOperational,
				corev1.ConditionFalse,
				reason,
				err.Error())

			status.Status = shipperV1.InstallationStatusFailed
			status.Message = err.Error()
			glog.Warningf("Get config for Cluster %q for InstallationTarget %q: %s", clusterName, shipperController.MetaKey(it), err)
			continue
		}

		// At this point, we got a hold in a connection to the target cluster,
		// so we assume it's operational until some other signal saying
		// otherwise arrives.
		status.Conditions = conditions.SetInstallationCondition(
			status.Conditions,
			shipperV1.ClusterConditionTypeOperational,
			corev1.ConditionTrue,
			"", "")

		if err = handler.installRelease(cluster, client, restConfig, c.dynamicClientBuilderFunc); err != nil {
			status.Conditions = conditions.SetInstallationCondition(
				status.Conditions,
				shipperV1.ClusterConditionTypeReady,
				corev1.ConditionFalse,
				conditions.ServerError,
				err.Error())

			status.Status = shipperV1.InstallationStatusFailed
			status.Message = err.Error()
			glog.Warningf("Install InstallationTarget %q for Cluster %q: %s", shipperController.MetaKey(it), clusterName, err)
			continue
		}

		status.Conditions = conditions.SetInstallationCondition(
			status.Conditions,
			shipperV1.ClusterConditionTypeOperational,
			corev1.ConditionTrue,
			"", "")

		status.Status = shipperV1.InstallationStatusInstalled
	}

	sort.Sort(byClusterName(clusterStatuses))
	it.Status.Clusters = clusterStatuses

	_, err = c.shipperclientset.ShipperV1().InstallationTargets(it.Namespace).Update(it)
	if err == nil {
		c.recorder.Eventf(
			it,
			corev1.EventTypeNormal,
			"InstallationStatusChanged",
			"Set %q status to %v",
			shipperController.MetaKey(it),
			clusterStatuses,
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
