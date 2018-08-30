package clustersecret

import (
	"fmt"
	"time"

	"github.com/golang/glog"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corelistersv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shipperlisters "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1"
	"github.com/bookingcom/shipper/pkg/tls"
)

const AgentName = "clustersecret-controller"

type Controller struct {
	clusterLister  shipperlisters.ClusterLister
	clustersSynced cache.InformerSynced

	kubeClientset kubernetes.Interface
	secretLister  corelistersv1.SecretLister
	secretsSynced cache.InformerSynced

	workqueue workqueue.RateLimitingInterface
	recorder  record.EventRecorder

	tls          tls.Pair
	ownNamespace string
}

func NewController(
	shipperInformerFactory shipperinformers.SharedInformerFactory,
	kubeClientset kubernetes.Interface,
	kubeInformerFactory informers.SharedInformerFactory,
	certPath string,
	keyPath string,
	ownNamespace string,
	recorder record.EventRecorder,
) *Controller {
	clusterInformer := shipperInformerFactory.Shipper().V1().Clusters()
	secretInformer := kubeInformerFactory.Core().V1().Secrets()

	controller := &Controller{
		clusterLister:  clusterInformer.Lister(),
		clustersSynced: clusterInformer.Informer().HasSynced,

		kubeClientset: kubeClientset,
		secretLister:  secretInformer.Lister(),
		secretsSynced: secretInformer.Informer().HasSynced,

		workqueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "clustersecret_controller_secrets"),
		recorder:  recorder,

		tls:          tls.Pair{certPath, keyPath},
		ownNamespace: ownNamespace,
	}

	clusterInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueCluster,
		DeleteFunc: controller.enqueueCluster,
		// changes to Cluster objects do not result in changes to Secrets
	})

	secretInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(old, new interface{}) {
			oldS, oldOk := old.(*corev1.Secret)
			newS, newOk := new.(*corev1.Secret)
			if oldOk && newOk && oldS.ResourceVersion == newS.ResourceVersion {
				glog.V(6).Info("Received Secret re-sync Update")
				return
			}

			controller.enqueueSecret(new)
		},
		DeleteFunc: controller.enqueueSecret,
	})

	return controller
}

// Run starts ClusterSecret controller workers and blocks until stopCh is
// closed.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	glog.V(2).Info("Starting ClusterSecret controller")
	defer glog.V(2).Info("Shutting down ClusterSecret controller")

	if !cache.WaitForCacheSync(stopCh, c.clustersSynced, c.secretsSynced) {
		runtime.HandleError(fmt.Errorf("failed to sync caches for the ClusterSecret controller"))
		return
	}

	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	glog.V(4).Info("Started ClusterSecret controller")

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

	// We're done processing this object.
	defer c.workqueue.Done(obj)

	var (
		key string
		ok  bool
	)

	if key, ok = obj.(string); !ok {
		// Do not attempt to process invalid objects again.
		c.workqueue.Forget(obj)
		runtime.HandleError(fmt.Errorf("invalid object key: %#v", obj))
		return true
	}

	if err := c.syncOne(key); err != nil {
		runtime.HandleError(fmt.Errorf("error syncing %q: %s", key, err))
		c.workqueue.AddRateLimited(key)
		return true
	}

	// Do not requeue this object because it's already processed.
	c.workqueue.Forget(obj)

	glog.V(6).Infof("Successfully synced %q", key)

	return true
}

func (c *Controller) enqueueSecret(obj interface{}) {
	var (
		meta metav1.Object
		ok   bool
	)

	if meta, ok = obj.(metav1.Object); !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			runtime.HandleError(fmt.Errorf("neither a Meta object, nor a tombstone: %#v", meta))
			return
		}

		meta, ok = tombstone.Obj.(metav1.Object)
		if !ok {
			runtime.HandleError(fmt.Errorf("not a Meta object inside tombstone: %#v", tombstone.Obj))
			return
		}
	}

	cluster, err := c.resolveOwnerRef(meta)
	if err != nil {
		runtime.HandleError(err)
		return
	} else if cluster == nil {
		glog.V(4).Infof("Ignoring Secret %q because it's not controlled by us", meta.GetName())
		return
	}

	c.enqueueCluster(cluster)
}

func (c *Controller) enqueueCluster(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	c.workqueue.AddRateLimited(key)
}

func (c *Controller) syncOne(key string) error {
	_, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return fmt.Errorf("invalid resource key: %q", key)
	}

	cluster, err := c.clusterLister.Get(name)
	if err != nil {
		// The Cluster resource may no longer exist, which is fine. We just stop
		// processing. Related Secret resources will be garbage collected.
		if errors.IsNotFound(err) {
			glog.V(6).Infof("Cluster %q has been deleted", key)
			return nil
		}

		return err
	}

	if err := c.processCluster(cluster); err != nil {
		c.recorder.Event(cluster, corev1.EventTypeWarning, "ClusterSecretError", err.Error())
		return err
	}

	return nil
}

func (c *Controller) resolveOwnerRef(meta metav1.Object) (*shipperv1.Cluster, error) {
	refs := meta.GetOwnerReferences()
	if len(refs) != 1 || refs[0].Kind != "Cluster" {
		return nil, nil
	}

	owner, err := c.clusterLister.Get(refs[0].Name)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve owner ref for Secret %q: %s", meta.GetName(), err)
	} else if owner.UID != refs[0].UID {
		return nil, nil
	}

	return owner, nil
}
