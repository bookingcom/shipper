package controller

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

func NewAppClusterEventHandler(filterLabel string, callback func(obj interface{})) cache.ResourceEventHandler {
	return cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			kubeobj, ok := obj.(metav1.Object)
			if !ok {
				klog.Warningf("Received something that's not a metav1/Object: %v", obj)
				return false
			}

			_, ok = kubeobj.GetLabels()[filterLabel]

			return ok
		},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				callback(obj)
			},
			UpdateFunc: func(old, new interface{}) {
				callback(new)
			},
			DeleteFunc: func(obj interface{}) {
				callback(obj)
			},
		},
	}
}
