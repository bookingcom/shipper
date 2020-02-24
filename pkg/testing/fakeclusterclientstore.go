package testing

import (
	"fmt"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	"github.com/bookingcom/shipper/pkg/clusterclientstore"
)

// FakeClusterClientStore is a fake implementation of a ClusterClientStore,
// allowing you to provide your own clientsets.
type FakeClusterClientStore struct {
	clusters map[string]*FakeCluster

	subscriptionCallbacks []clusterclientstore.SubscriptionRegisterFunc
	eventHandlerCallbacks []clusterclientstore.EventHandlerRegisterFunc
}

var _ clusterclientstore.Interface = (*FakeClusterClientStore)(nil)

func NewFakeClusterClientStore(clusters map[string]*FakeCluster) *FakeClusterClientStore {
	return &FakeClusterClientStore{clusters: clusters}
}

func (s *FakeClusterClientStore) AddCluster(c *FakeCluster) {
	s.clusters[c.Name] = c
}

func (s *FakeClusterClientStore) AddSubscriptionCallback(c clusterclientstore.SubscriptionRegisterFunc) {
	s.subscriptionCallbacks = append(s.subscriptionCallbacks, c)
}

func (s *FakeClusterClientStore) AddEventHandlerCallback(c clusterclientstore.EventHandlerRegisterFunc) {
	s.eventHandlerCallbacks = append(s.eventHandlerCallbacks, c)
}

func (s *FakeClusterClientStore) Run(stopCh <-chan struct{}) {
	for name, cluster := range s.clusters {
		informerFactory := cluster.InformerFactory

		for _, subscriptionCallback := range s.subscriptionCallbacks {
			subscriptionCallback(informerFactory)
		}

		for _, eventHandlerCallback := range s.eventHandlerCallbacks {
			eventHandlerCallback(informerFactory, name)
		}

		informerFactory.Start(stopCh)
		informerFactory.WaitForCacheSync(stopCh)
	}
}

func (s *FakeClusterClientStore) GetApplicationClusterClientset(clusterName, ua string) (clusterclientstore.ClientsetInterface, error) {
	if _, ok := s.clusters[clusterName]; !ok {
		return nil, fmt.Errorf("no client for cluster %q", clusterName)
	}
	return NewFakeClusterClientset(s.clusters[clusterName]), nil
}

type FakeClusterClientset struct {
	cluster *FakeCluster
}

var _ clusterclientstore.ClientsetInterface = (*FakeClusterClientset)(nil)

func NewFakeClusterClientset(cluster *FakeCluster) *FakeClusterClientset {
	return &FakeClusterClientset{
		cluster: cluster,
	}
}

func (fs *FakeClusterClientset) GetConfig() *rest.Config {
	return &rest.Config{}
}

func (fs *FakeClusterClientset) GetKubeClient() kubernetes.Interface {
	return fs.cluster.Client
}

func (fs *FakeClusterClientset) GetKubeInformerFactory() informers.SharedInformerFactory {
	return fs.cluster.InformerFactory
}

func (fs *FakeClusterClientset) GetShipperClient() shipperclientset.Interface {
	return nil
}

func (fs *FakeClusterClientset) GetShipperInformerFactory() shipperinformers.SharedInformerFactory {
	return nil
}
