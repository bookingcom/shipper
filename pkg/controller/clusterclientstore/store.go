// package clusterclientstore provides a thread-safe storage for kubernetes
// clients. The internal storage is updated automatically by observing
// the kubernetes cluster for cluster objects. New cluster objects
// trigger a client creation, updates to Secret objects trigger
// re-creation of a client, and Cluster deletions cause the removal of
// a client.
package clusterclientstore

import (
	"fmt"
	"sync"
	"time"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shipper "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	kubeinformers "k8s.io/client-go/informers"
	kubernetes "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

type Store struct {
	// internal lock, protecting access to the map of cluster clients
	clientLock     sync.RWMutex
	clusterClients map[string]kubernetes.Interface
	// internal lock, protecting access to the map of informer factories
	sharedInformerLock       sync.RWMutex
	clusterInformerFactories map[string]kubeinformers.SharedInformerFactory

	// the client for the shipper group on the management cluster
	managementClusterShipperClient shipper.Interface
	// an informer watching the shipper group on the management cluster
	managementClusterShipperInformerFactory shipperinformers.SharedInformerFactory
	// The client for the kubernetes group on the management cluster
	managementClusterKubeClient kubernetes.Interface
	// The informer for the kubernetes group on the management cluster
	managementClusterKubeInformerFactory kubeinformers.SharedInformerFactory
	// the stop channel to be passed to informers
	stopchan <-chan struct{}

	// called when the cluster caches have been populated, so that the controller can register event handlers
	EventHandlerRegisterFunc EventHandlerRegisterFunc

	// called before the informer factory is started, so that the controller can set watches on objects it's interested in
	SubscriptionRegisterFunc SubscriptionRegisterFunc
}

// NewStore creates a new client store that will use the specified
// client to set up watches on Cluster objects.
func NewStore(
	kubeClient kubernetes.Interface,
	shipperClient shipper.Interface,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	shipperInformerFactory shipperinformers.SharedInformerFactory,
	stopchan <-chan struct{},
) *Store {
	return &Store{
		managementClusterKubeClient:             kubeClient,
		managementClusterShipperClient:          shipperClient,
		managementClusterKubeInformerFactory:    kubeInformerFactory,
		managementClusterShipperInformerFactory: shipperInformerFactory,
		stopchan:                 stopchan,
		clusterClients:           map[string]kubernetes.Interface{},
		clusterInformerFactories: map[string]kubeinformers.SharedInformerFactory{},
	}
}

// Run registers event handlers to watch Secret and
// Cluster objects on the management cluster.
func (s *Store) Run() {
	secretsInformer := s.managementClusterKubeInformerFactory.Core().V1().Secrets().Informer()
	clustersInformer := s.managementClusterShipperInformerFactory.Shipper().V1().Clusters().Informer()

	// Start the informers
	s.managementClusterKubeInformerFactory.Start(s.stopchan)
	s.managementClusterShipperInformerFactory.Start(s.stopchan)

	// Wait for caches to sync
	s.managementClusterKubeInformerFactory.WaitForCacheSync(s.stopchan)
	s.managementClusterShipperInformerFactory.WaitForCacheSync(s.stopchan)

	// register the event handlers
	secretsInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: s.updateSecret,
	})

	clustersInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    s.addCluster,
		UpdateFunc: s.updateCluster,
		DeleteFunc: s.deleteCluster,
	})
}

// GetClient returns a client for the specified cluster name.
func (s *Store) GetClient(clusterName string) (kubernetes.Interface, error) {
	s.clientLock.RLock()
	defer s.clientLock.RUnlock()

	var client kubernetes.Interface
	var ok bool
	if client, ok = s.clusterClients[clusterName]; !ok {
		return nil, fmt.Errorf("No client for cluster %s", clusterName)
	}

	return client, nil
}

// GetInformerFactory returns an informer factory for the specified cluster name.
func (s *Store) GetInformerFactory(clusterName string) (kubeinformers.SharedInformerFactory, error) {
	s.sharedInformerLock.RLock()
	s.sharedInformerLock.RUnlock()

	informer, ok := s.clusterInformerFactories[clusterName]
	if !ok {
		return nil, fmt.Errorf("No informer factory exists for a cluster called %s", clusterName)
	}

	return informer, nil
}

func (s *Store) setClient(clusterName string, client kubernetes.Interface) {
	s.clientLock.Lock()
	defer s.clientLock.Unlock()

	s.clusterClients[clusterName] = client
}

func (s *Store) setInformerFactory(clusterName string, informerFactory kubeinformers.SharedInformerFactory) {
	s.sharedInformerLock.Lock()
	s.sharedInformerLock.Unlock()

	s.clusterInformerFactories[clusterName] = informerFactory
	s.SubscriptionRegisterFunc(informerFactory)
	informerFactory.Start(s.stopchan)
	informerFactory.WaitForCacheSync(s.stopchan)
	s.EventHandlerRegisterFunc(informerFactory, clusterName)
}

func (s *Store) unsetClient(clusterName string) {
	s.clientLock.Lock()
	defer s.clientLock.Unlock()

	delete(s.clusterClients, clusterName)
}

func (s *Store) unsetInformerFactory(clusterName string) {
	s.sharedInformerLock.Lock()
	defer s.sharedInformerLock.Unlock()

	delete(s.clusterInformerFactories, clusterName)
}

func (s *Store) updateSecret(old, new interface{}) {

}

func (s *Store) addCluster(obj interface{}) {
	cluster := obj.(*shipperv1.Cluster)

	secret, err := s.managementClusterKubeClient.Core().Secrets(shipperv1.ShipperNamespace).Get(cluster.Name, metav1.GetOptions{})
	if err != nil {
		runtime.HandleError(err)
		return
	}

	config := &rest.Config{
		Host: cluster.Spec.APIMaster,
	}
	config.CAData = secret.Data["tls.ca"]
	config.CertData = secret.Data["tls.cert"]
	config.KeyData = secret.Data["tls.key"]

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	informerFactory := kubeinformers.NewSharedInformerFactory(client, time.Second*30)

	s.setClient(cluster.Name, client)
	s.setInformerFactory(cluster.Name, informerFactory)
}

func (s *Store) updateCluster(old, new interface{}) {
	s.addCluster(new)
}

func (s *Store) deleteCluster(obj interface{}) {
	cluster := obj.(*shipperv1.Cluster)

	s.unsetClient(cluster.Name)
	s.unsetInformerFactory(cluster.Name)
}
