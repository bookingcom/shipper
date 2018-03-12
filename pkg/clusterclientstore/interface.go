package clusterclientstore

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type ClientProvider interface {
	GetClient(clusterName string) (kubernetes.Interface, error)
	GetConfig(clusterName string) (*rest.Config, error)
}
