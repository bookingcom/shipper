package clusterclientstore

import (
	"fmt"

	"github.com/golang/glog"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	kubecache "k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
)

func (s *Store) clusterWorker() {
	for processNextWorkItem(s.clusterWorkqueue, s.syncCluster) {
	}
}

func (s *Store) secretWorker() {
	for processNextWorkItem(s.secretWorkqueue, s.syncSecret) {
	}
}

func (s *Store) bindEventHandlers() {
	enqueueSecret := func(obj interface{}) { enqueueWorkItem(s.secretWorkqueue, obj) }
	s.secretInformer.Informer().AddEventHandler(kubecache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			secret, ok := obj.(*corev1.Secret)
			if !ok {
				return false
			}
			// NOTE(btyler) this is a bit aggressive, but I think it makes sense;
			// otherwise we get logs about the service account token
			_, ok = secret.GetAnnotations()[shipperv1.SecretChecksumAnnotation]
			return ok && secret.Namespace == shipperv1.ShipperNamespace
		},
		Handler: kubecache.ResourceEventHandlerFuncs{
			AddFunc: enqueueSecret,
			UpdateFunc: func(_, newObj interface{}) {
				enqueueSecret(newObj)
			},
			DeleteFunc: func(obj interface{}) {
				secret, ok := obj.(*corev1.Secret)
				if !ok {
					tombstone, ok := obj.(kubecache.DeletedFinalStateUnknown)
					if !ok {
						runtime.HandleError(fmt.Errorf("Couldn't get object from tombstone %#v", obj))
						return
					}
					secret, ok = tombstone.Obj.(*corev1.Secret)
					if !ok {
						runtime.HandleError(fmt.Errorf("Tombstone contained object that is not a Secret %#v", obj))
						return
					}
				}
				enqueueSecret(secret)
			},
		},
	})

	enqueueCluster := func(obj interface{}) { enqueueWorkItem(s.clusterWorkqueue, obj) }
	s.clusterInformer.Informer().AddEventHandler(kubecache.ResourceEventHandlerFuncs{
		AddFunc: enqueueCluster,
		UpdateFunc: func(_, new interface{}) {
			enqueueCluster(new)
		},
		DeleteFunc: func(obj interface{}) {
			cluster, ok := obj.(*shipperv1.Cluster)
			if !ok {
				tombstone, ok := obj.(kubecache.DeletedFinalStateUnknown)
				if !ok {
					runtime.HandleError(fmt.Errorf("Couldn't get object from tombstone %#v", obj))
					return
				}
				cluster, ok = tombstone.Obj.(*shipperv1.Cluster)
				if !ok {
					runtime.HandleError(fmt.Errorf("Tombstone contained object that is not a Cluster %#v", obj))
					return
				}
			}
			enqueueCluster(cluster)
		},
	})
}

func enqueueWorkItem(wq workqueue.RateLimitingInterface, obj interface{}) {
	key, err := kubecache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	wq.AddRateLimited(key)
}

func processNextWorkItem(wq workqueue.RateLimitingInterface, handler func(string) error) bool {
	obj, shutdown := wq.Get()
	if shutdown {
		return false
	}

	// We're done processing this object.
	defer wq.Done(obj)

	var (
		key string
		ok  bool
	)

	if key, ok = obj.(string); !ok {
		// Do not attempt to process invalid objects again.
		wq.Forget(obj)
		runtime.HandleError(fmt.Errorf("invalid object key: %#v", obj))
		return true
	}

	if err := handler(key); err != nil {
		runtime.HandleError(fmt.Errorf("error syncing %q: %s", key, err))
		wq.AddRateLimited(key)
		return true
	}

	// Do not requeue this object because it's already processed.
	wq.Forget(obj)

	glog.Infof("Successfully synced %q", key)

	return true
}
