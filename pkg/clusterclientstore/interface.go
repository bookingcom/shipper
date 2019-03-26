package clusterclientstore

import (
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type ClientProvider interface {
	GetClient(string, string) (kubernetes.Interface, error)
	GetConfig(string) (*rest.Config, error)
}

type Interface interface {
	AddSubscriptionCallback(SubscriptionRegisterFunc)
	AddEventHandlerCallback(EventHandlerRegisterFunc)
	GetClient(string, string) (kubernetes.Interface, error)
	GetInformerFactory(string) (kubeinformers.SharedInformerFactory, error)
}
