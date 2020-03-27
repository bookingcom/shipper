package clusterclientstore

import (
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
)

type Interface interface {
	AddEventHandlerCallback(EventHandlerRegisterFunc)
	AddSubscriptionCallback(SubscriptionRegisterFunc)

	GetApplicationClusterClientset(clusterName, ua string) (ClientsetInterface, error)
}

type ClientsetInterface interface {
	GetConfig() *rest.Config

	GetKubeClient() kubernetes.Interface
	GetKubeInformerFactory() kubeinformers.SharedInformerFactory

	GetShipperClient() shipperclientset.Interface
	GetShipperInformerFactory() shipperinformers.SharedInformerFactory
}

// SubscriptionRegisterFunc should call the relevant functions on a shared
// informer factory to set up watches.
//
// Note that there should be no event handlers being assigned to any informers
// in this function.
type SubscriptionRegisterFunc func(kubeinformers.SharedInformerFactory, shipperinformers.SharedInformerFactory)

// EventHandlerRegisterFunc is called after the caches for the clusters have
// been built, and provides a hook for a controller to register its event
// handlers. These will be event handlers for changes to the resources that the
// controller has subscribed to in the `SubscriptionRegisterFunc` callback.
type EventHandlerRegisterFunc func(kubeinformers.SharedInformerFactory, shipperinformers.SharedInformerFactory, string)

// This enables tests to inject an appropriate fake client, which allows us to
// use the real cluster client store in unit tests.
type KubeClientBuilderFunc func(string, string, *rest.Config) (kubernetes.Interface, error)
type ShipperClientBuilderFunc func(string, string, *rest.Config) (shipperclientset.Interface, error)
