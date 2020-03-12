package clusterclientstore

import (
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"sort"
	"strconv"
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
	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shipperinformer "github.com/bookingcom/shipper/pkg/client/informers/externalversions/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/clusterclientstore/cache"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
)

const AgentName = "clusterclientstore"

// This enables tests to inject an appropriate fake client, which allows us to
// use the real cluster client store in unit tests.
type KubeClientBuilderFunc func(string, string, *rest.Config) (kubernetes.Interface, error)
type ShipperClientBuilderFunc func(string, string, *rest.Config) (shipperclientset.Interface, error)

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

// GetClient returns a client for the specified cluster name and user agent
// pair.
func (s *Store) GetClient(clusterName string, ua string) (kubernetes.Interface, error) {
	cluster, ok := s.cache.Fetch(clusterName)
	if !ok {
		return nil, shippererrors.NewClusterNotInStoreError(clusterName)
	}

	return cluster.GetClient(ua)
}

// GetConfig returns a rest.Config for the specified cluster name.
func (s *Store) GetConfig(clusterName string) (*rest.Config, error) {
	cluster, ok := s.cache.Fetch(clusterName)
	if !ok {
		return nil, shippererrors.NewClusterNotInStoreError(clusterName)
	}

	return cluster.GetConfig()
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

	kubeClient, err := cluster.GetClient(userAgent)
	if err != nil {
		return nil, err
	}

	kubeInformerFactory, err := cluster.GetInformerFactory()
	if err != nil {
		return nil, err
	}

	shipperClient, err := s.buildShipperClient(clusterName, userAgent, config)
	if err != nil {
		return nil, err
	}

	return NewStoreClientset(
		config,
		kubeClient,
		kubeInformerFactory,
		shipperClient,
		s.shipperInformerFactory,
	), nil
}

// GetInformerFactory returns an informer factory for the specified
// cluster name.
func (s *Store) GetInformerFactory(clusterName string) (kubeinformers.SharedInformerFactory, error) {
	cluster, ok := s.cache.Fetch(clusterName)
	if !ok {
		return nil, shippererrors.NewClusterNotInStoreError(clusterName)
	}

	return cluster.GetInformerFactory()
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
	config, err := buildConfig(cluster.Spec.APIMaster, secret, s.restTimeout)
	if err != nil {
		return shippererrors.NewClusterClientBuild(cluster.Name, err)
	}

	// These are only used in shared informers. Setting HTTP timeout here would
	// affect watches which is undesirable. Instead, we leave it to client-go (see
	// k8s.io/client-go/tools/cache) to govern watch durations.
	informerConfig, err := buildConfig(cluster.Spec.APIMaster, secret, nil)
	if err != nil {
		return shippererrors.NewClusterClientBuild(cluster.Name, err)
	}

	informerClient, err := s.buildKubeClient(cluster.Name, AgentName, informerConfig)
	if err != nil {
		return shippererrors.NewClusterClientBuild(cluster.Name, err)
	}

	informerFactory := kubeinformers.NewSharedInformerFactory(informerClient, 0*time.Second)
	// Register all the resources that the controllers are interested in, e.g.
	// informerFactory.Core().V1().Pods().Informer().
	for _, cb := range s.subscriptionRegisterFuncs {
		cb(informerFactory)
	}

	clusterName := cluster.Name
	checksum := computeSecretChecksum(secret)
	newCachedCluster := cache.NewCluster(
		clusterName,
		checksum,
		config,
		informerFactory,
		s.buildKubeClient,
		func() {
			// If/when the informer cache finishes syncing, bind all of the event handler
			// callbacks from the controllers if it does not finish (because the cluster
			// was Shutdown) this will not be called.
			for _, cb := range s.eventHandlerRegisterFuncs {
				cb(informerFactory, clusterName)
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

// TODO(btyler): error here or let any invalid data get picked up by errors from
// kube.NewForConfig or auth problems at connection time?
func buildConfig(host string, secret *corev1.Secret, restTimeout *time.Duration) (*rest.Config, error) {
	config := &rest.Config{
		Host: host,
	}

	if restTimeout != nil {
		config.Timeout = *restTimeout
	}

	// Can't use the ServiceAccountToken type because we don't want the service
	// account controller to touch it.
	_, tokenOK := secret.Data["token"]
	if tokenOK {
		ca := secret.Data["ca.crt"]
		config.CAData = ca

		token := secret.Data["token"]
		config.BearerToken = string(token)
		return config, nil
	}

	// Let's figure it's either a TLS secret or an opaque thing formatted like a
	// TLS secret.

	// The cluster secret controller does not include the CA in the secret: you end
	// up using the system CA trust store. However, it's much handier for
	// integration testing to be able to create a secret that is independent of the
	// underlying system trust store.
	if ca, ok := secret.Data["tls.ca"]; ok {
		config.CAData = ca
	}

	if crt, ok := secret.Data["tls.crt"]; ok {
		config.CertData = crt
	}

	if key, ok := secret.Data["tls.key"]; ok {
		config.KeyData = key
	}

	if encodedInsecureSkipTlsVerify, ok := secret.Annotations[shipper.SecretClusterSkipTlsVerifyAnnotation]; ok {
		if insecureSkipTlsVerify, err := strconv.ParseBool(encodedInsecureSkipTlsVerify); err == nil {
			klog.Infof("found %q annotation with value %q for host %q", shipper.SecretClusterSkipTlsVerifyAnnotation, encodedInsecureSkipTlsVerify, host)
			config.Insecure = insecureSkipTlsVerify
		} else {
			klog.Infof("found %q annotation with value %q for host %q but failed to decode a bool from it, ignoring it", shipper.SecretClusterSkipTlsVerifyAnnotation, encodedInsecureSkipTlsVerify, host)
		}
	}

	return config, nil
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
