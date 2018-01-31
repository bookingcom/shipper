package v1

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func (c *Cluster) Client() (*kubernetes.Clientset, error) {
	// Get the cluster token from the management cluster
	cfg := &rest.Config{
		Host: c.Spec.APIMaster,
	}
	clusterClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return clusterClient, nil

}
