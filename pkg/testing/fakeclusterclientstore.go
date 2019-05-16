package testing

import (
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/bookingcom/shipper/pkg/clusterclientstore"
)

func NewFakeClusterClientStore(
	fakeClient *fake.Clientset,
	informer informers.SharedInformerFactory,
	fakeClusterNames []string,
) *FakeClusterClientStore {
	return &FakeClusterClientStore{
		client:                fakeClient,
		informerFactory:       informer,
		subscriptionCallbacks: []clusterclientstore.SubscriptionRegisterFunc{},
		eventHandlerCallbacks: []clusterclientstore.EventHandlerRegisterFunc{},
		FakeClusterNames:      fakeClusterNames,
	}
}

// FakeClusterClientStore stores only one informer and fake clientset, and no
// matter what cluster name it's called with, returns the same clientset and
// informer.
//
// It also supports callbacks, meaning that when it's passed into a controller,
// the controller can register callback and be notified when the Run() method on
// FakeClusterClientstore is called.
type FakeClusterClientStore struct {
	client                *fake.Clientset
	informerFactory       informers.SharedInformerFactory
	subscriptionCallbacks []clusterclientstore.SubscriptionRegisterFunc
	eventHandlerCallbacks []clusterclientstore.EventHandlerRegisterFunc
	// Passed to the registered event handler callbacks.
	FakeClusterNames []string
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
		for _, clusName := range s.FakeClusterNames {
			eventHandlerCallback(s.informerFactory, clusName)
		}
	}
}

func (s *FakeClusterClientStore) GetClient(clusterName string, ua string) (kubernetes.Interface, error) {
	return s.client, nil
}

func (s *FakeClusterClientStore) GetInformerFactory(clusterName string) (informers.SharedInformerFactory, error) {
	return s.informerFactory, nil
}
