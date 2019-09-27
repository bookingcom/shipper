package testing

import (
	fakedynamic "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/bookingcom/shipper/pkg/clusterclientstore"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
)

type FakeClientPair struct {
	Client        *kubefake.Clientset
	DynamicClient *fakedynamic.FakeDynamicClient
}

func NewSimpleFakeClusterClientStore(
	fakeClient *fake.Clientset,
	informerFactory informers.SharedInformerFactory,
	clusterNames []string,
) *FakeClusterClientStore {
	clientsPerCluster := make(map[string]FakeClientPair)
	for _, clusterName := range clusterNames {
		clientsPerCluster[clusterName] = FakeClientPair{Client: fakeClient}
	}

	return NewFakeClusterClientStore(clientsPerCluster, informerFactory)
}

func NewFakeClusterClientStore(
	clientsPerCluster map[string]FakeClientPair,
	informerFactory informers.SharedInformerFactory,
) *FakeClusterClientStore {
	return &FakeClusterClientStore{
		clientsPerCluster: clientsPerCluster,
		informerFactory:   informerFactory,
	}
}

// FakeClusterClientStore stores one clientset per cluster, but only one
// informer, that gets returned no matter the cluster./ It also supports
// callbacks, meaning that when it's passed into a controller, the controller
// can register callback and be notified when the Run() method on
// FakeClusterClientstore is called.
type FakeClusterClientStore struct {
	clientsPerCluster map[string]FakeClientPair

	restConfig      *rest.Config
	informerFactory informers.SharedInformerFactory

	subscriptionCallbacks []clusterclientstore.SubscriptionRegisterFunc
	eventHandlerCallbacks []clusterclientstore.EventHandlerRegisterFunc
}

func (s *FakeClusterClientStore) AddSubscriptionCallback(subscriptionCallback clusterclientstore.SubscriptionRegisterFunc) {
	s.subscriptionCallbacks = append(s.subscriptionCallbacks, subscriptionCallback)
}

func (s *FakeClusterClientStore) AddEventHandlerCallback(eventHandlerCallback clusterclientstore.EventHandlerRegisterFunc) {
	s.eventHandlerCallbacks = append(s.eventHandlerCallbacks, eventHandlerCallback)
}

func (s *FakeClusterClientStore) Run(stopCh <-chan struct{}) {
	for _, subscriptionCallback := range s.subscriptionCallbacks {
		subscriptionCallback(s.informerFactory)
	}

	for _, eventHandlerCallback := range s.eventHandlerCallbacks {
		for clusName, _ := range s.clientsPerCluster {
			eventHandlerCallback(s.informerFactory, clusName)
		}
	}
}

func (s *FakeClusterClientStore) GetClient(clusterName string, ua string) (kubernetes.Interface, error) {
	return s.clientsPerCluster[clusterName].Client, nil
}

func (s *FakeClusterClientStore) GetConfig(clusterName string) (*rest.Config, error) {
	return &rest.Config{}, nil
}

func (s *FakeClusterClientStore) GetInformerFactory(clusterName string) (informers.SharedInformerFactory, error) {
	return s.informerFactory, nil
}

func NewFailingFakeClusterClientStore() clusterclientstore.Interface {
	return &FailingFakeClusterClientStore{}
}

// FailingFakeClusterClientStore is an implementation of
// clusterclientstore.Interface that always fails when trying to get clients
// for any cluster.
type FailingFakeClusterClientStore struct {
	FakeClusterClientStore
}

func (s *FailingFakeClusterClientStore) GetClient(clusterName string, ua string) (kubernetes.Interface, error) {
	return nil, shippererrors.NewClusterNotReadyError(clusterName)
}
