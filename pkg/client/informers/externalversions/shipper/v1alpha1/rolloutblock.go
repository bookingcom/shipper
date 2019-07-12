// Code generated by informer-gen. DO NOT EDIT.

package v1alpha1

import (
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

// RolloutBlockInformer provides access to a shared informer and lister for
// RolloutBlocks.
type RolloutBlockInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1alpha1.RolloutBlockLister
}

type rolloutBlockInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
	namespace        string
}

// NewRolloutBlockInformer constructs a new informer for RolloutBlock type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewRolloutBlockInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredRolloutBlockInformer(client, namespace, resyncPeriod, indexers, nil)
}

// NewFilteredRolloutBlockInformer constructs a new informer for RolloutBlock type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredRolloutBlockInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.ShipperV1alpha1().RolloutBlocks(namespace).List(options)
			},
			WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.ShipperV1alpha1().RolloutBlocks(namespace).Watch(options)
			},
		},
		&shipperv1alpha1.RolloutBlock{},
		resyncPeriod,
		indexers,
	)
}

func (f *rolloutBlockInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredRolloutBlockInformer(client, f.namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *rolloutBlockInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&shipperv1alpha1.RolloutBlock{}, f.defaultInformer)
}

func (f *rolloutBlockInformer) Lister() v1alpha1.RolloutBlockLister {
	return v1alpha1.NewRolloutBlockLister(f.Informer().GetIndexer())
}