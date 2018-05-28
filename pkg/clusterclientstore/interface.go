package clusterclientstore

import (
	kubeInformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type ClientProvider interface {
	GetClient(clusterName string) (kubernetes.Interface, error)
	GetConfig(clusterName string) (*rest.Config, error)
}

type Interface interface {
	AddSubscriptionCallback(SubscriptionRegisterFunc)
	AddEventHandlerCallback(EventHandlerRegisterFunc)
	GetClient(string) (kubernetes.Interface, error)
	GetInformerFactory(string) (kubeInformers.SharedInformerFactory, error)
}
