// Code generated by informer-gen. DO NOT EDIT.

package v1alpha1

import (
	"context"
	time "time"

	shipperv1alpha1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	versioned "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	internalinterfaces "github.com/bookingcom/shipper/pkg/client/informers/externalversions/internalinterfaces"
	v1alpha1 "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
)

// TrafficTargetInformer provides access to a shared informer and lister for
// TrafficTargets.
type TrafficTargetInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1alpha1.TrafficTargetLister
}

type trafficTargetInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
	namespace        string
}

// NewTrafficTargetInformer constructs a new informer for TrafficTarget type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewTrafficTargetInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredTrafficTargetInformer(client, namespace, resyncPeriod, indexers, nil)
}

// NewFilteredTrafficTargetInformer constructs a new informer for TrafficTarget type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredTrafficTargetInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.ShipperV1alpha1().TrafficTargets(namespace).List(context.TODO(), options)
			},
			WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.ShipperV1alpha1().TrafficTargets(namespace).Watch(context.TODO(), options)
			},
		},
		&shipperv1alpha1.TrafficTarget{},
		resyncPeriod,
		indexers,
	)
}

func (f *trafficTargetInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredTrafficTargetInformer(client, f.namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *trafficTargetInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&shipperv1alpha1.TrafficTarget{}, f.defaultInformer)
}

func (f *trafficTargetInformer) Lister() v1alpha1.TrafficTargetLister {
	return v1alpha1.NewTrafficTargetLister(f.Informer().GetIndexer())
}
