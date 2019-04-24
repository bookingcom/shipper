package janitor

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperclient "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shipperlisters "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/clusterclientstore"
)

const AgentName = "janitor-controller"

type Controller struct {
	shipperClientset   shipperclient.Interface
	workqueue          workqueue.RateLimitingInterface
	clusterClientStore clusterclientstore.Interface
	recorder           record.EventRecorder

	itLister shipperlisters.InstallationTargetLister
	itSynced cache.InformerSynced
}

const AnchorSuffix = "-anchor"

const InstallationTargetUID = "InstallationTargetUID"

func NewController(
	shipperclientset shipperclient.Interface,
	shipperInformerFactory shipperinformers.SharedInformerFactory,
	store clusterclientstore.Interface,
	recorder record.EventRecorder,
) *Controller {

	itInformer := shipperInformerFactory.Shipper().V1alpha1().InstallationTargets()

	controller := &Controller{
		recorder:           recorder,
		workqueue:          workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
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
				releaseName := it.GetLabels()[shipper.ReleaseLabel]
				wi := &InstallationTargetWorkItem{
					ObjectMeta: *it.ObjectMeta.DeepCopy(),
					Key:        key,
					Namespace:  namespace,
					Name:       name,
					AnchorName: fmt.Sprintf("%s%s", releaseName, AnchorSuffix),
					Clusters:   it.Spec.Clusters,
				}
				controller.workqueue.Add(wi)
			}
		},
	})

	store.AddSubscriptionCallback(func(informerFactory kubeinformers.SharedInformerFactory) {
		// Subscribe to receive notifications for events related to config maps
		// from all the clusters available in the store. Config maps are used as
		// anchor, since they're owner of all of the release related objects
		// added to the application cluster by the installation target.
		informerFactory.Core().V1().ConfigMaps().Informer()
	})

	store.AddEventHandlerCallback(func(informerFactory kubeinformers.SharedInformerFactory, clusterName string) {
		informerFactory.Core().V1().ConfigMaps().Informer().AddEventHandler(
			cache.FilteringResourceEventHandler{
				// Anchor objects are basically config maps with the shipper.ReleaseLabel
				// label, so we want to filter to this constraint. Additionally we check
				// whether the config map's Data field has the InstallationTargetUID key,
				// ignoring those config maps that don't have it.
				FilterFunc: func(obj interface{}) bool {
					cm := obj.(*corev1.ConfigMap)
					hasRightName := strings.HasSuffix(cm.GetName(), AnchorSuffix)
					_, hasReleaseLabel := cm.GetLabels()[shipper.ReleaseLabel]
					_, hasUID := cm.Data[InstallationTargetUID]
					if hasRightName && hasReleaseLabel && !hasUID {
						controller.recorder.Eventf(cm,
							corev1.EventTypeWarning,
							"ConfigMapIncomplete",
							"Anchor config map doesn't have %q key, skipping", InstallationTargetUID)
					}
					return hasRightName && hasReleaseLabel && hasUID
				},
				Handler: cache.ResourceEventHandlerFuncs{
					// Enqueue all the config maps that have a shipper.ReleaseLabel,
					// extracting all the information required for the worker to, well, work.
					UpdateFunc: func(oldObj, newObj interface{}) {
						cm := newObj.(*corev1.ConfigMap)
						if key, err := cache.MetaNamespaceKeyFunc(cm); err != nil {
							runtime.HandleError(err)
							return
						} else if namespace, name, err := cache.SplitMetaNamespaceKey(key); err != nil {
							runtime.HandleError(err)
							return
						} else {
							// In theory, if we are in this block it means that the
							// object has a shipper.ReleaseLabel label *and* the
							// InstallationTargetUID key in Data, so it should be
							// fine to just get them both.
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
							controller.workqueue.Add(wi)
						}
					},
				},
			},
		)
	})

	return controller
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

	glog.V(2).Info("Starting Janitor controller")
	defer glog.V(2).Info("Shutting down Janitor controller")

	if ok := cache.WaitForCacheSync(stopCh, c.itSynced); !ok {
		runtime.HandleError(fmt.Errorf("failed to wait for caches to sync"))
		return
	}

	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	glog.V(4).Info("Started Janitor controller")

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

		if err != nil {
			glog.Infof("error syncing %q: %s", workItem.GetKey(), err.Error())
		} else {
			c.workqueue.Forget(obj)
			glog.Infof("Successfully synced %q", workItem.GetKey())
		}
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
		return err
	} else if it != nil && string(it.UID) == item.InstallationTargetUID {
		// The anchor config map's installation target UID and the installation
		// target object in the manage cluster match, so we just bail out here.
		return nil
	} else {
		glog.V(2).Infof(
			"Release anchor points to either wrong or non-existent installation target UID %q, proceeding to remove it",
			item.InstallationTargetUID)
	}

	configMap := &corev1.ConfigMap{ObjectMeta: item.ObjectMeta}
	if ok, err := c.removeAnchor(item.ClusterName, item.Namespace, item.Name); err != nil {
		err.Broadcast(configMap, c.recorder)
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

func (c *Controller) removeAnchor(clusterName string, namespace string, name string) (bool, *RecordableError) {
	if client, err := c.clusterClientStore.GetClient(clusterName, AgentName); err != nil {
		return false, NewRecordableError(
			corev1.EventTypeWarning,
			"ClusterClientError",
			"Error acquiring a client for cluster %q: %s",
			clusterName, err)
	} else if err := client.CoreV1().ConfigMaps(namespace).Delete(name, &metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
		return false, NewRecordableError(
			corev1.EventTypeWarning,
			"ConfigMapDeletionFailed",
			"Config map '%s/%s' deletion on cluster %q failed: %s",
			namespace, name, clusterName, err)
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
			err.Broadcast(installationTarget, c.recorder)
			return err
		} else if ok {
			c.recorder.Eventf(installationTarget,
				corev1.EventTypeNormal,
				"ConfigMapDeleted",
				"Config map %q has been deleted from cluster %q",
				item.Key, clusterName)
		}
	}

	return nil
}
