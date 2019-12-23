package clusterclientstore

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	kubecache "k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
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
			// This is a bit aggressive, but I think it makes sense; otherwise we get
			// logs about the service account token.
			_, ok = secret.GetAnnotations()[shipper.SecretChecksumAnnotation]
			return ok
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
						runtime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
						return
					}
					secret, ok = tombstone.Obj.(*corev1.Secret)
					if !ok {
						runtime.HandleError(fmt.Errorf("tombstone contained object that is not a Secret %#v", obj))
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
			cluster, ok := obj.(*shipper.Cluster)
			if !ok {
				tombstone, ok := obj.(kubecache.DeletedFinalStateUnknown)
				if !ok {
					runtime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
					return
				}
				cluster, ok = tombstone.Obj.(*shipper.Cluster)
				if !ok {
					runtime.HandleError(fmt.Errorf("tombstone contained object that is not a Cluster %#v", obj))
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

	defer wq.Done(obj)

	var (
		key string
		ok  bool
	)

	if key, ok = obj.(string); !ok {
		wq.Forget(obj)
		runtime.HandleError(fmt.Errorf("invalid object key (will retry: false): %#v", obj))
		return true
	}

	shouldRetry := false
	err := handler(key)

	if err != nil {
		shouldRetry = shippererrors.ShouldRetry(err)
		runtime.HandleError(fmt.Errorf("error syncing %q (will retry: %t): %s", key, shouldRetry, err.Error()))
	}

	if shouldRetry {
		wq.AddRateLimited(key)
		return true
	}

	wq.Forget(obj)

	klog.Infof("Successfully synced %q", key)

	return true
}
