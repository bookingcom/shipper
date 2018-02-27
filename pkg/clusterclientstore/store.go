package clusterclientstore

import (
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	kubeinformers "k8s.io/client-go/informers"
	corev1informer "k8s.io/client-go/informers/core/v1"
	kubernetes "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shipperv1informer "github.com/bookingcom/shipper/pkg/client/informers/externalversions/shipper/v1"
)

type Store struct {
	// internal lock, protecting access to the map of cluster clients
	clientLock           sync.RWMutex
	clusterClients       map[string]kubernetes.Interface
	clusterClientConfigs map[string]*rest.Config
	// internal lock, protecting access to the map of informer factories
	sharedInformerLock       sync.RWMutex
	clusterInformerFactories map[string]kubeinformers.SharedInformerFactory

	secretInformer  corev1informer.SecretInformer
	clusterInformer shipperv1informer.ClusterInformer

	stopchan <-chan struct{}

	// called when the cluster caches have been populated, so that the controller can register event handlers
	EventHandlerRegisterFunc EventHandlerRegisterFunc

	// called before the informer factory is started, so that the controller can set watches on objects it's interested in
	SubscriptionRegisterFunc SubscriptionRegisterFunc
}

// NewStore creates a new client store that will use the specified
// client to set up watches on Cluster objects.
func NewStore(
	secretInformer corev1informer.SecretInformer,
	clusterInformer shipperv1informer.ClusterInformer,
	stopchan <-chan struct{},
) *Store {
	s := &Store{
		secretInformer:           secretInformer,
		clusterInformer:          clusterInformer,
		stopchan:                 stopchan,
		clusterClients:           map[string]kubernetes.Interface{},
		clusterClientConfigs:     map[string]*rest.Config{},
		clusterInformerFactories: map[string]kubeinformers.SharedInformerFactory{},
	}

	// register the event handlers
	secretInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: s.updateSecret,
	})

	clusterInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    s.addCluster,
		UpdateFunc: s.updateCluster,
		DeleteFunc: s.deleteCluster,
	})

	return s
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

// GetConfig returns a client for the specified cluster name.
func (s *Store) GetConfig(clusterName string) (*rest.Config, error) {
	s.clientLock.RLock()
	defer s.clientLock.RUnlock()

	var config *rest.Config
	var ok bool
	if config, ok = s.clusterClientConfigs[clusterName]; !ok {
		return nil, fmt.Errorf("No client config for cluster %s", clusterName)
	}

	return config, nil
}

// GetInformerFactory returns an informer factory for the specified
// cluster name.
func (s *Store) GetInformerFactory(clusterName string) (kubeinformers.SharedInformerFactory, error) {
	s.sharedInformerLock.RLock()
	defer s.sharedInformerLock.RUnlock()

	informer, ok := s.clusterInformerFactories[clusterName]
	if !ok {
		return nil, fmt.Errorf("No informer factory exists for a cluster called %s", clusterName)
	}

	return informer, nil
}

func (s *Store) setClient(clusterName string, client kubernetes.Interface, config *rest.Config) {
	s.clientLock.Lock()
	defer s.clientLock.Unlock()

	s.clusterClients[clusterName] = client
	s.clusterClientConfigs[clusterName] = config
}

func (s *Store) setInformerFactory(clusterName string, informerFactory kubeinformers.SharedInformerFactory) {
	s.sharedInformerLock.Lock()
	defer s.sharedInformerLock.Unlock()

	s.clusterInformerFactories[clusterName] = informerFactory
	if s.SubscriptionRegisterFunc != nil {
		s.SubscriptionRegisterFunc(informerFactory)
	}

	informerFactory.Start(s.stopchan)
	informerFactory.WaitForCacheSync(s.stopchan)

	if s.EventHandlerRegisterFunc != nil {
		s.EventHandlerRegisterFunc(informerFactory, clusterName)
	}
}

func (s *Store) unsetClient(clusterName string) {
	s.clientLock.Lock()
	defer s.clientLock.Unlock()

	delete(s.clusterClients, clusterName)
	delete(s.clusterClientConfigs, clusterName)
}

func (s *Store) unsetInformerFactory(clusterName string) {
	s.sharedInformerLock.Lock()
	defer s.sharedInformerLock.Unlock()

	delete(s.clusterInformerFactories, clusterName)
}

func (s *Store) updateSecret(old, new interface{}) {
	//TODO(btyler) check these assertions #28
	oldSecret := old.(*corev1.Secret)
	newSecret := new.(*corev1.Secret)

	if oldSecret.Namespace != shipperv1.ShipperNamespace || newSecret.Namespace != shipperv1.ShipperNamespace {
		// These secrets are not in our namespace, so we don't
		// need to do anything
		return
	}

	s.clientLock.RLock()
	_, ok := s.clusterClients[newSecret.Name]
	s.clientLock.RUnlock()
	if !ok {
		// we don't have a cluster by this name, so don't do
		// anything
		return
	}

	oldChecksum, ok := oldSecret.GetAnnotations()[shipperv1.SecretChecksumAnnotation]
	if !ok {
		runtime.HandleError(fmt.Errorf("Can't compare secrets: no %s annotation defined on the old version of %s secret", shipperv1.SecretChecksumAnnotation, oldSecret.Name))
		return
	}

	newChecksum, ok := newSecret.GetAnnotations()[shipperv1.SecretChecksumAnnotation]
	if !ok {
		runtime.HandleError(fmt.Errorf("Can't compare secrets: no %s annotation defined on the new version of %s secret", shipperv1.SecretChecksumAnnotation, newSecret.Name))
		return
	}

	if oldChecksum == newChecksum {
		// Nothing changed, nothing to do
		return
	}

	cluster, err := s.clusterInformer.Lister().Get(newSecret.Name)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	s.addCluster(cluster)
}

func (s *Store) addCluster(obj interface{}) {
	//TODO(btyler) check this assertion #28
	cluster := obj.(*shipperv1.Cluster)

	secret, err := s.secretInformer.Lister().Secrets(shipperv1.ShipperNamespace).Get(cluster.Name)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	config := &rest.Config{
		Host: cluster.Spec.APIMaster,
	}

	// the cluster secret controller does not include the CA in the secret:
	// you end up using the system CA trust store. However, it's much handier
	// for integration testing to be able to create a secret that is
	// independent of the underlying system trust store.
	if ca, ok := secret.Data["tls.ca"]; ok {
		config.CAData = ca
	}

	config.CertData = secret.Data["tls.crt"]
	config.KeyData = secret.Data["tls.key"]

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	informerFactory := kubeinformers.NewSharedInformerFactory(client, time.Second*30)

	s.setClient(cluster.Name, client, config)
	s.setInformerFactory(cluster.Name, informerFactory)
}

func (s *Store) updateCluster(old, new interface{}) {
	//TODO(btyler) check these assertions #28
	oldCluster := old.(*shipperv1.Cluster)
	newCluster := new.(*shipperv1.Cluster)

	// only inspect API server because that's the only field which has a bearing on client connectivity
	if oldCluster.Spec.APIMaster == newCluster.Spec.APIMaster {
		return
	}

	s.addCluster(new)
}

func (s *Store) deleteCluster(obj interface{}) {
	//TODO(btyler) check this assertion #28
	cluster := obj.(*shipperv1.Cluster)

	s.unsetClient(cluster.Name)
	s.unsetInformerFactory(cluster.Name)
}
