package janitor

import (
	"fmt"
	"sort"
	"time"

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
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shipperlisters "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/clusterclientstore"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	objectutil "github.com/bookingcom/shipper/pkg/util/object"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
	shipperworkqueue "github.com/bookingcom/shipper/pkg/workqueue"
)

const (
	AgentName = "janitor-controller"
)

type Controller struct {
	clientset shipperclient.Interface
	store     clusterclientstore.Interface

	releaseLister  shipperlisters.ReleaseLister
	releasesSynced cache.InformerSynced

	clusterLister  shipperlisters.ClusterLister
	clustersSynced cache.InformerSynced

	workqueue workqueue.RateLimitingInterface

	recorder record.EventRecorder
}

func NewController(
	clientset shipperclient.Interface,
	store clusterclientstore.Interface,
	informerFactory shipperinformers.SharedInformerFactory,
	recorder record.EventRecorder,
) *Controller {
	shipperv1alpha1 := informerFactory.Shipper().V1alpha1()
	releaseInformer := shipperv1alpha1.Releases()
	clusterInformer := shipperv1alpha1.Clusters()

	controller := &Controller{
		clientset: clientset,
		store:     store,

		releaseLister:  releaseInformer.Lister(),
		releasesSynced: releaseInformer.Informer().HasSynced,

		clusterLister:  clusterInformer.Lister(),
		clustersSynced: clusterInformer.Informer().HasSynced,

		workqueue: workqueue.NewNamedRateLimitingQueue(
			shipperworkqueue.NewDefaultControllerRateLimiter(),
			"janitor_controller",
		),

		recorder: recorder,
	}

	releaseInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueue,
		UpdateFunc: func(oldObj, newObj interface{}) {
			controller.enqueue(newObj)
		},
		DeleteFunc: controller.enqueue,
	})

	store.AddSubscriptionCallback(func(kubeInformerFactory kubeinformers.SharedInformerFactory, shipperInformerFactory shipperinformers.SharedInformerFactory) {
		shipperv1alpha1 := shipperInformerFactory.Shipper().V1alpha1()
		shipperv1alpha1.InstallationTargets().Informer()
		shipperv1alpha1.CapacityTargets().Informer()
		shipperv1alpha1.TrafficTargets().Informer()
	})

	store.AddEventHandlerCallback(func(kubeInformerFactory kubeinformers.SharedInformerFactory, shipperInformerFactory shipperinformers.SharedInformerFactory) {
		eventHandler := cache.ResourceEventHandlerFuncs{
			AddFunc:    controller.enqueue,
			DeleteFunc: controller.enqueue,
		}

		shipperv1alpha1 := shipperInformerFactory.Shipper().V1alpha1()
		shipperv1alpha1.InstallationTargets().Informer().AddEventHandler(eventHandler)
		shipperv1alpha1.CapacityTargets().Informer().AddEventHandler(eventHandler)
		shipperv1alpha1.TrafficTargets().Informer().AddEventHandler(eventHandler)
	})

	return controller
}

func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	klog.V(2).Info("Starting Janitor controller")
	defer klog.V(2).Info("Shutting down Janitor controller")

	if ok := cache.WaitForCacheSync(
		stopCh,
		c.releasesSynced,
		c.clustersSynced,
	); !ok {
		runtime.HandleError(fmt.Errorf("failed to wait for caches to sync"))
		return
	}

	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	klog.V(4).Info("Started Janitor controller")

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
		runtime.HandleError(fmt.Errorf("error running garbage collection for Release %q (will retry: %t): %s", key, shouldRetry, err.Error()))
	}

	if shouldRetry {
		c.workqueue.AddRateLimited(key)
		return true
	}

	c.workqueue.Forget(obj)

	return true
}

func (c *Controller) syncHandler(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return shippererrors.NewUnrecoverableError(err)
	}

	selector := labels.Everything()
	clusters, err := c.clusterLister.List(selector)
	if err != nil {
		return shippererrors.NewKubeclientListError(
			shipper.SchemeGroupVersion.WithKind("Cluster"),
			"", selector, err)
	}

	var clustersToGarbageCollect []string

	rel, err := c.releaseLister.Releases(namespace).Get(name)
	if err != nil {
		if !kerrors.IsNotFound(err) {
			return shippererrors.NewKubeclientGetError(namespace, name, err).
				WithShipperKind("Release")
		}

		for _, cluster := range clusters {
			clustersToGarbageCollect = append(clustersToGarbageCollect, cluster.Name)
		}
	} else {
		// The release still exists, so janitor can skip garbage
		// collecting in the clusters the release needs to be installed
		// in.
		selectedClusters := releaseutil.GetSelectedClusters(rel)

		for _, cluster := range clusters {
			n := sort.SearchStrings(selectedClusters, cluster.Name)
			if n > 0 {
				clustersToGarbageCollect = append(clustersToGarbageCollect, cluster.Name)
			}
		}
	}

	for _, cluster := range clustersToGarbageCollect {
		err = c.garbageCollectFromCluster(namespace, name, cluster)
		if err != nil {
			return err
		}

		klog.V(4).Infof("Successfully removed orphaned objects for Release %q in cluster %s", key, cluster)
	}

	return nil
}

func (c *Controller) garbageCollectFromCluster(namespace, name, cluster string) error {
	clusterClientsets, err := c.store.GetApplicationClusterClientset(cluster, AgentName)
	if err != nil {
		return err
	}

	err = c.garbageCollectInstallationTarget(clusterClientsets, namespace, name)
	if err != nil {
		return err
	}

	err = c.garbageCollectCapacityTarget(clusterClientsets, namespace, name)
	if err != nil {
		return err
	}

	err = c.garbageCollectTrafficTarget(clusterClientsets, namespace, name)
	if err != nil {
		return err
	}

	return nil
}

func (c *Controller) garbageCollectInstallationTarget(
	clusterClientsets clusterclientstore.ClientsetInterface,
	namespace, name string,
) error {
	_, err := clusterClientsets.GetShipperInformerFactory().
		Shipper().V1alpha1().InstallationTargets().Lister().
		InstallationTargets(namespace).Get(name)
	if err != nil {
		if kerrors.IsNotFound(err) {
			return nil
		}

		return shippererrors.
			NewKubeclientGetError(namespace, name, err).
			WithShipperKind("InstallationTarget")
	}

	err = clusterClientsets.GetShipperClient().ShipperV1alpha1().
		InstallationTargets(namespace).Delete(name, &metav1.DeleteOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			return nil
		}

		return shippererrors.
			NewKubeclientDeleteError(namespace, name, err).
			WithShipperKind("InstallationTarget")
	}

	return nil
}

func (c *Controller) garbageCollectCapacityTarget(
	clusterClientsets clusterclientstore.ClientsetInterface,
	namespace, name string,
) error {
	_, err := clusterClientsets.GetShipperInformerFactory().
		Shipper().V1alpha1().CapacityTargets().Lister().
		CapacityTargets(namespace).Get(name)
	if err != nil {
		if kerrors.IsNotFound(err) {
			return nil
		}

		return shippererrors.
			NewKubeclientGetError(namespace, name, err).
			WithShipperKind("CapacityTarget")
	}

	err = clusterClientsets.GetShipperClient().ShipperV1alpha1().
		CapacityTargets(namespace).Delete(name, &metav1.DeleteOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			return nil
		}

		return shippererrors.
			NewKubeclientDeleteError(namespace, name, err).
			WithShipperKind("CapacityTarget")
	}

	return nil
}

func (c *Controller) garbageCollectTrafficTarget(
	clusterClientsets clusterclientstore.ClientsetInterface,
	namespace, name string,
) error {
	_, err := clusterClientsets.GetShipperInformerFactory().
		Shipper().V1alpha1().TrafficTargets().Lister().
		TrafficTargets(namespace).Get(name)
	if err != nil {
		if kerrors.IsNotFound(err) {
			return nil
		}

		return shippererrors.
			NewKubeclientGetError(namespace, name, err).
			WithShipperKind("TrafficTarget")
	}

	err = clusterClientsets.GetShipperClient().ShipperV1alpha1().
		TrafficTargets(namespace).Delete(name, &metav1.DeleteOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			return nil
		}

		return shippererrors.
			NewKubeclientDeleteError(namespace, name, err).
			WithShipperKind("TrafficTarget")
	}

	return nil
}

func (c *Controller) enqueue(obj interface{}) {
	kubeobj, ok := obj.(metav1.Object)
	if !ok {
		runtime.HandleError(fmt.Errorf("not a metav1.Object: %#v", obj))
		return
	}

	releaseName, err := objectutil.GetReleaseLabel(kubeobj)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	key := fmt.Sprintf("%s/%s", kubeobj.GetNamespace(), releaseName)

	c.workqueue.Add(key)
}
