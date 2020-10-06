package clusterclientstore

import (
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type Interface interface {
	AddEventHandlerCallback(EventHandlerRegisterFunc)
	AddSubscriptionCallback(SubscriptionRegisterFunc)
	GetClient(clusterName string, ua string) (kubernetes.Interface, error)
	GetConfig(clusterName string) (*rest.Config, error)
	GetInformerFactory(string) (kubeinformers.SharedInformerFactory, error)
}
