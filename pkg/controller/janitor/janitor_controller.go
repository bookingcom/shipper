package janitor

import (
	"fmt"
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	"github.com/bookingcom/shipper/pkg/util/anchor"
	"github.com/bookingcom/shipper/pkg/util/filters"
	shipperworkqueue "github.com/bookingcom/shipper/pkg/workqueue"
)

const (
	AgentName             = "janitor-controller"
	InstallationTargetUID = "InstallationTargetUID"
)

type Controller struct {
	shipperClientset   shipperclient.Interface
	workqueue          workqueue.RateLimitingInterface
	clusterClientStore clusterclientstore.Interface
	recorder           record.EventRecorder

	itLister shipperlisters.InstallationTargetLister
	itSynced cache.InformerSynced
}

func NewController(
	shipperclientset shipperclient.Interface,
	shipperInformerFactory shipperinformers.SharedInformerFactory,
	store clusterclientstore.Interface,
	recorder record.EventRecorder,
) *Controller {

	itInformer := shipperInformerFactory.Shipper().V1alpha1().InstallationTargets()

	controller := &Controller{
		recorder:           recorder,
		workqueue:          workqueue.NewNamedRateLimitingQueue(shipperworkqueue.NewDefaultControllerRateLimiter(), "janitor_controller_installationtargets"),
		clusterClientStore: store,
		shipperClientset:   shipperclientset,
		itLister:           itInformer.Lister(),
		itSynced:           itInformer.Informer().HasSynced,
	}

	// Here we register the event handler for the deletion of InstallationTarget
	// objects. This delete handler enqueues an item in the workqueue with all
	// the information required to remove the anchor object from all the clusters
	// the installation controller has installed the application.
	itInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: func(obj interface{}) {
			if key, err := cache.MetaNamespaceKeyFunc(obj); err != nil {
				runtime.HandleError(err)
				return
			} else if namespace, name, err := cache.SplitMetaNamespaceKey(key); err != nil {
				runtime.HandleError(err)
				return
			} else {
				it := obj.(*shipper.InstallationTarget)
				wi := &InstallationTargetWorkItem{
					ObjectMeta: *it.ObjectMeta.DeepCopy(),
					Key:        key,
					Namespace:  namespace,
					Name:       name,
					AnchorName: anchor.CreateAnchorName(it),
					Clusters:   it.Spec.Clusters,
				}
				controller.workqueue.Add(wi)
			}
		},
	})

	store.AddSubscriptionCallback(controller.subscribeToAppClusterEvents)
	store.AddEventHandlerCallback(controller.registerAppClusterEventHandlers)

	return controller
}

func (c *Controller) registerAppClusterEventHandlers(informerFactory kubeinformers.SharedInformerFactory, clusterName string) {
	handler := cache.FilteringResourceEventHandler{
		FilterFunc: filters.BelongsToInstallationTarget,
		Handler: cache.ResourceEventHandlerFuncs{
			UpdateFunc: func(old, new interface{}) {
				c.enqueueConfigMap(new, clusterName)
			},
		},
	}

	informerFactory.Core().V1().ConfigMaps().Informer().AddEventHandler(handler)
}

func (c *Controller) subscribeToAppClusterEvents(informerFactory kubeinformers.SharedInformerFactory) {
	informerFactory.Core().V1().ConfigMaps().Informer()
}

type WorkItem interface {
	GetKey() string
}

type InstallationTargetWorkItem struct {
	ObjectMeta metav1.ObjectMeta
	Key        string
	Namespace  string
	Name       string
	AnchorName string
	Clusters   []string
}

func (i InstallationTargetWorkItem) GetKey() string {
	return i.Key
}

type AnchorWorkItem struct {
	ObjectMeta            metav1.ObjectMeta
	Key                   string
	InstallationTargetUID string
	ReleaseName           string
	ClusterName           string
	Namespace             string
	Name                  string
}

func (i AnchorWorkItem) GetKey() string {
	return i.Key
}

func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	klog.V(2).Info("Starting Janitor controller")
	defer klog.V(2).Info("Shutting down Janitor controller")

	if ok := cache.WaitForCacheSync(stopCh, c.itSynced); !ok {
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
	defer c.workqueue.Done(obj)

	if shutdown {
		return false
	}

	if workItem, ok := obj.(WorkItem); !ok {
		c.workqueue.Forget(obj)
		runtime.HandleError(fmt.Errorf("skipping since object it isn't a WorkItem, is a %q", reflect.TypeOf(obj)))
	} else {
		// If any of the sync methods return an error we should retry.
		var err error
		switch wi := workItem.(type) {
		case *AnchorWorkItem:
			err = c.syncAnchor(wi)
		case *InstallationTargetWorkItem:
			err = c.syncInstallationTarget(wi)
		default:
			// Ask the workqueue to forget about this item, since we don't know
			// how to handle it.
			runtime.HandleError(fmt.Errorf("don't know how to handle %q", reflect.TypeOf(obj)))
			c.workqueue.Forget(obj)
		}

		shouldRetry := false
		key := workItem.GetKey()

		if err != nil {
			shouldRetry = shippererrors.ShouldRetry(err)
			runtime.HandleError(fmt.Errorf("error syncing %q (will retry: %t): %s", key, shouldRetry, err.Error()))
		}

		if shouldRetry {
			c.workqueue.AddRateLimited(obj)

			return true
		}

		c.workqueue.Forget(obj)
		klog.Infof("Successfully synced %q", key)
	}

	return true
}

func (c *Controller) syncAnchor(item *AnchorWorkItem) error {
	// Attempt to fetch the installation target the anchor config map is
	// pointing to.
	it, err := c.itLister.InstallationTargets(item.Namespace).Get(item.ReleaseName)
	if err != nil && !errors.IsNotFound(err) {
		// Return an error only if the error is different than not found, since
		// if the installation target object doesn't exist anymore we want to
		// remove the anchor config map.
		return shippererrors.NewKubeclientGetError(item.Namespace, item.ReleaseName, err).
			WithShipperKind("InstallationTarget")
	} else if it != nil && string(it.UID) == item.InstallationTargetUID {
		// Check to see if the anchor was recreated at some point and report it
		anchorCreationTime := item.ObjectMeta.CreationTimestamp
		itCreationTime := it.CreationTimestamp
		timeDiff := itCreationTime.Time.Sub(anchorCreationTime.Time)
		if timeDiff > 1*time.Minute {
			klog.V(4).Infof("Achor %q in namespace %q was recreated at some point, as its creationTimestamp differs from its owning InstallationTarget creationTimestamp",
				item.Name, item.Namespace)
		}
		// The anchor config map's installation target UID and the installation
		// target object in the manage cluster match, so we just bail out here.
		return nil
	} else {
		klog.V(2).Infof(
			"Release anchor points to either wrong or non-existent installation target UID %q, proceeding to remove it",
			item.InstallationTargetUID)
	}

	configMap := &corev1.ConfigMap{ObjectMeta: item.ObjectMeta}
	if ok, err := c.removeAnchor(item.ClusterName, item.Namespace, item.Name); err != nil {
		c.recorder.Eventf(configMap,
			corev1.EventTypeWarning,
			"ConfigMapDeletionFailed",
			err.Error())
		return err
	} else if ok {
		c.recorder.Eventf(configMap,
			corev1.EventTypeNormal,
			"ConfigMapDeleted",
			"Config map %q has been deleted from cluster %q",
			item.Key, item.ClusterName)
	}

	return nil
}

func (c *Controller) removeAnchor(clusterName string, namespace string, name string) (bool, error) {
	if client, err := c.clusterClientStore.GetClient(clusterName, AgentName); err != nil {
		return false, err
	} else if err := client.CoreV1().ConfigMaps(namespace).Delete(name, &metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
		return false, shippererrors.NewKubeclientDeleteError(namespace, name, err).
			WithCoreV1Kind("ConfigMap")
	} else if err == nil {
		return true, nil
	}

	// If this point is reached, then it's likely that the anchor config map was
	// removed by another external process.
	return false, nil
}

func (c *Controller) syncInstallationTarget(item *InstallationTargetWorkItem) error {
	for _, clusterName := range item.Clusters {
		installationTarget := &shipper.InstallationTarget{ObjectMeta: item.ObjectMeta}
		if ok, err := c.removeAnchor(clusterName, item.Namespace, item.AnchorName); err != nil {
			c.recorder.Eventf(installationTarget,
				corev1.EventTypeWarning,
				"ConfigMapDeleted",
				"Config map %q has been deleted from cluster %q",
				err.Error())
			klog.V(4).Infof(
				"Attempted to remove anchor %q in namespace %q because owner installation target was removed, but error %q occurred",
				item.AnchorName, item.Namespace, err)
			return err
		} else if ok {
			c.recorder.Eventf(installationTarget,
				corev1.EventTypeNormal,
				"ConfigMapDeleted",
				"Config map %q has been deleted from cluster %q",
				item.Key, clusterName)
			klog.V(4).Infof(
				"Removed anchor %q in namespace %q because owner installation target was removed",
				item.AnchorName, item.Namespace)
		}
	}

	return nil
}

func (c *Controller) enqueueConfigMap(obj interface{}, clusterName string) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	// In theory, if we are in this block it means that the object has a
	// shipper.ReleaseLabel label *and* the InstallationTargetUID key in
	// Data, so it should be fine to just get them both.
	cm := obj.(*corev1.ConfigMap)
	releaseName := cm.GetLabels()[shipper.ReleaseLabel]
	uid := cm.Data[InstallationTargetUID]
	wi := &AnchorWorkItem{
		ObjectMeta:            *cm.ObjectMeta.DeepCopy(),
		Namespace:             namespace,
		Name:                  name,
		ClusterName:           clusterName,
		InstallationTargetUID: uid,
		Key:                   key,
		ReleaseName:           releaseName,
	}
	c.workqueue.Add(wi)
}
