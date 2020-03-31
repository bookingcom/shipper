package clusterclientstore

import (
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	corev1informer "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	kubecache "k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperclient "github.com/bookingcom/shipper/pkg/client"
	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shipperinformer "github.com/bookingcom/shipper/pkg/client/informers/externalversions/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/clusterclientstore/cache"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
)

const (
	AgentName = "clusterclientstore"
	noTimeout = 0 * time.Second
)

type Store struct {
	ns                 string
	buildKubeClient    KubeClientBuilderFunc
	buildShipperClient ShipperClientBuilderFunc
	restTimeout        *time.Duration
	cache              cache.CacheServer

	shipperInformerFactory shipperinformers.SharedInformerFactory

	secretInformer  corev1informer.SecretInformer
	clusterInformer shipperinformer.ClusterInformer

	secretWorkqueue  workqueue.RateLimitingInterface
	clusterWorkqueue workqueue.RateLimitingInterface

	// Called when the cluster caches have been populated, so that the controller
	// can register event handlers.
	eventHandlerRegisterFuncs []EventHandlerRegisterFunc

	// Called before the informer factory is started, so that the controller can
	// set watches on objects it's interested in.
	subscriptionRegisterFuncs []SubscriptionRegisterFunc
}

var _ Interface = (*Store)(nil)

// NewStore creates a new client store that will use the specified informers to
// maintain a cache of clientsets, rest.Configs, and informers for target
// clusters.
func NewStore(
	buildKubeClient KubeClientBuilderFunc,
	buildShipperClient ShipperClientBuilderFunc,
	secretInformer corev1informer.SecretInformer,
	shipperInformerFactory shipperinformers.SharedInformerFactory,
	ns string,
	restTimeout *time.Duration,
) *Store {
	s := &Store{
		ns:                 ns,
		buildKubeClient:    buildKubeClient,
		buildShipperClient: buildShipperClient,
		restTimeout:        restTimeout,
		cache:              cache.NewServer(),

		shipperInformerFactory: shipperInformerFactory,

		secretInformer:  secretInformer,
		clusterInformer: shipperInformerFactory.Shipper().V1alpha1().Clusters(),

		secretWorkqueue:  workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Client Store Secrets"),
		clusterWorkqueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Client Store Clusters"),
	}

	s.bindEventHandlers()

	return s
}

func (s *Store) Run(stopCh <-chan struct{}) {
	defer runtime.HandleCrash()
	defer s.clusterWorkqueue.ShutDown()
	defer s.secretWorkqueue.ShutDown()
	defer s.cache.Stop()

	klog.Info("Waiting for client store informer caches to sync")

	ok := kubecache.WaitForCacheSync(
		stopCh,
		s.secretInformer.Informer().HasSynced,
		s.clusterInformer.Informer().HasSynced,
	)

	if !ok {
		runtime.HandleError(fmt.Errorf("failed to sync caches for the ClusterClientStore"))
		return
	}

	klog.Info("Starting cluster client store workers")

	go s.cache.Serve()
	go wait.Until(s.clusterWorker, time.Second, stopCh)
	go wait.Until(s.secretWorker, time.Second, stopCh)

	klog.Info("client store is running")
	defer klog.Info("shutting down client store...")

	<-stopCh
}

func (s *Store) AddSubscriptionCallback(subscription SubscriptionRegisterFunc) {
	s.subscriptionRegisterFuncs = append(s.subscriptionRegisterFuncs, subscription)
}

func (s *Store) AddEventHandlerCallback(eventHandler EventHandlerRegisterFunc) {
	s.eventHandlerRegisterFuncs = append(s.eventHandlerRegisterFuncs, eventHandler)
}

func (s *Store) GetApplicationClusterClientset(clusterName, userAgent string) (ClientsetInterface, error) {
	cluster, ok := s.cache.Fetch(clusterName)
	if !ok {
		return nil, shippererrors.NewClusterNotInStoreError(clusterName)
	}

	config, err := cluster.GetConfig()
	if err != nil {
		return nil, err
	}

	kubeClient, err := cluster.GetKubeClient(userAgent)
	if err != nil {
		return nil, err
	}

	kubeInformerFactory, err := cluster.GetKubeInformerFactory()
	if err != nil {
		return nil, err
	}

	shipperClient, err := cluster.GetShipperClient(userAgent)
	if err != nil {
		return nil, err
	}

	shipperInformerFactory, err := cluster.GetShipperInformerFactory()
	if err != nil {
		return nil, err
	}

	return NewStoreClientset(
		config,
		kubeClient,
		kubeInformerFactory,
		shipperClient,
		shipperInformerFactory,
	), nil
}

func (s *Store) syncCluster(name string) error {
	// No splitting here because clusters are not namespaced.
	clusterObj, err := s.clusterInformer.Lister().Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.Infof("Cluster %q has been deleted; purging it from client store", name)
			s.cache.Remove(name)
			return nil
		}

		return shippererrors.NewKubeclientGetError("", name, err).
			WithShipperKind("Cluster")
	}

	cachedCluster, ok := s.cache.Fetch(name)
	if ok {
		var config *rest.Config
		config, err = cachedCluster.GetConfig()
		// We don't want to regenerate the client if we already have one with the
		// right properties (host or secret checksum) that's either ready (err == nil)
		// or in the process of getting ready. Otherwise we'll refill the cache
		// needlessly, or could even end up in a livelock where waiting for informer
		// cache to fill takes longer than the resync period, and resync resets the
		// informer.
		if err == nil || shippererrors.IsClusterNotReadyError(err) {
			if config != nil && config.Host == clusterObj.Spec.APIMaster {
				klog.Infof("Cluster %q syncing, but we already have a client with the right host in the cache", name)
				return nil
			}
		}
	}

	secret, err := s.secretInformer.Lister().Secrets(s.ns).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.Infof("Cluster %q has no corresponding secret", name)
			return nil
		}

		return shippererrors.NewKubeclientGetError(s.ns, name, err).
			WithCoreV1Kind("Secret")
	}

	return s.create(clusterObj, secret)
}

func (s *Store) syncSecret(key string) error {
	ns, name, err := kubecache.SplitMetaNamespaceKey(key)
	if err != nil {
		return shippererrors.NewUnrecoverableError(err)
	}

	// Programmer error: secretInformer needs to be namespaced to only
	// shipper's own namespace.
	if ns != s.ns {
		return shippererrors.NewUnrecoverableError(fmt.Errorf(
			"client store secret workqueue should only contain secrets from the shipper namespace"))
	}

	secret, err := s.secretInformer.Lister().Secrets(s.ns).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.Infof("Secret %q has been deleted; purging any associated client from client store", key)
			s.cache.Remove(name)
			return nil
		}

		return shippererrors.NewKubeclientGetError(s.ns, name, err).
			WithCoreV1Kind("Secret")
	}

	clusterObj, err := s.clusterInformer.Lister().Get(secret.Name)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.Infof("Secret %q has no corresponding cluster", key)
			return nil
		}

		return shippererrors.NewKubeclientGetError("", secret.Name, err).
			WithShipperKind("Cluster")
	}

	if cachedCluster, ok := s.cache.Fetch(secret.Name); ok {
		secretChecksum := computeSecretChecksum(secret)
		clusterChecksum, err := cachedCluster.GetChecksum()
		if err == nil || shippererrors.IsClusterNotReadyError(err) {
			// We don't want to regenerate the client if we already have one with the
			// right properties (host or secret checksum) that's either ready (err == nil)
			// or in the process of getting ready. Otherwise we'll refill the cache
			// needlessly, or could even end up in a livelock where waiting for informer
			// cache to fill takes longer than the resync period, and resync resets the
			// informer.
			if secretChecksum == clusterChecksum {
				klog.Infof("Secret %q syncing but we already have a client based on the same checksum in the cache", key)
				return nil
			}
		}
	}

	return s.create(clusterObj, secret)
}

func (s *Store) create(cluster *shipper.Cluster, secret *corev1.Secret) error {
	config := shipperclient.BuildConfigFromClusterAndSecret(cluster, secret)
	if s.restTimeout != nil {
		config.Timeout = *s.restTimeout
	}

	// These are only used in shared informers. Setting HTTP timeout here
	// would affect watches which is undesirable. Instead, we leave it to
	// client-go (see k8s.io/client-go/tools/cache) to govern watch
	// durations.
	informerConfig := rest.CopyConfig(config)
	informerConfig.Timeout = noTimeout

	kubeInformerClient, err := s.buildKubeClient(cluster.Name, AgentName, informerConfig)
	if err != nil {
		return shippererrors.NewClusterClientBuild(cluster.Name, err)
	}

	shipperInformerClient, err := s.buildShipperClient(cluster.Name, AgentName, informerConfig)
	if err != nil {
		return shippererrors.NewClusterClientBuild(cluster.Name, err)
	}

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeInformerClient, noTimeout)
	shipperInformerFactory := shipperinformers.NewSharedInformerFactory(shipperInformerClient, noTimeout)

	// Register all the resources that the controllers are interested in, e.g.
	// informerFactory.Core().V1().Pods().Informer().
	for _, cb := range s.subscriptionRegisterFuncs {
		cb(kubeInformerFactory, shipperInformerFactory)
	}

	clusterName := cluster.Name
	checksum := computeSecretChecksum(secret)
	newCachedCluster := cache.NewCluster(
		clusterName, checksum, config,
		kubeInformerFactory, shipperInformerFactory,
		s.buildKubeClient, s.buildShipperClient,
		func() {
			// If/when the informer cache finishes syncing, bind all of the event handler
			// callbacks from the controllers if it does not finish (because the cluster
			// was Shutdown) this will not be called.
			for _, cb := range s.eventHandlerRegisterFuncs {
				cb(kubeInformerFactory, shipperInformerFactory, clusterName)
			}
		})

	s.cache.Store(newCachedCluster)
	return nil
}

// StoreClientset is supposed to be a short-lived container for a set of
// client connections. As it is effectively caching clients and factories, it's
// the caller's responsibility to ensure these objects are not stall.
type StoreClientset struct {
	config *rest.Config

	kubeClient          kubernetes.Interface
	kubeInformerFactory kubeinformers.SharedInformerFactory

	shipperClient          shipperclientset.Interface
	shipperInformerFactory shipperinformers.SharedInformerFactory
}

var _ ClientsetInterface = (*StoreClientset)(nil)

// NewStoreClientset is a dummy constructor forcing all components to be
// present.
func NewStoreClientset(config *rest.Config, kubeClient kubernetes.Interface,
	kubeInformerFactory kubeinformers.SharedInformerFactory, shipperClient shipperclientset.Interface,
	shipperInformerFactory shipperinformers.SharedInformerFactory) *StoreClientset {
	return &StoreClientset{
		config:                 config,
		kubeClient:             kubeClient,
		kubeInformerFactory:    kubeInformerFactory,
		shipperClient:          shipperClient,
		shipperInformerFactory: shipperInformerFactory,
	}
}

func (c *StoreClientset) GetConfig() *rest.Config {
	return c.config
}

func (c *StoreClientset) GetKubeClient() kubernetes.Interface {
	return c.kubeClient
}

func (c *StoreClientset) GetKubeInformerFactory() kubeinformers.SharedInformerFactory {
	return c.kubeInformerFactory
}

func (c *StoreClientset) GetShipperClient() shipperclientset.Interface {
	return c.shipperClient
}

func (c *StoreClientset) GetShipperInformerFactory() shipperinformers.SharedInformerFactory {
	return c.shipperInformerFactory
}

func computeSecretChecksum(secret *corev1.Secret) string {
	hash := crc32.NewIEEE()
	keys := make([]string, len(secret.Data))
	for k := range secret.Data {
		keys = append(keys, k)
	}
	// in order to compute a reproducible hash, all key-value pairs should come
	// in a deterministic order
	sort.Strings(keys)
	for _, k := range keys {
		hash.Write([]byte(k))
		hash.Write(secret.Data[k])
	}
	sum := hex.EncodeToString(hash.Sum(nil))
	return sum
}
