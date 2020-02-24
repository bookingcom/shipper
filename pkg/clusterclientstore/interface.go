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
