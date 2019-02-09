package clusterclientstore

import (
	"fmt"
	"strconv"
	"time"

	"github.com/golang/glog"
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

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperinformer "github.com/bookingcom/shipper/pkg/client/informers/externalversions/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/clusterclientstore/cache"
)

// This enables tests to inject an appropriate fake client, which allows us to
// use the real cluster client store in unit tests.
type ClientBuilderFunc func(string, *rest.Config) (kubernetes.Interface, error)

type Store struct {
	ns          string
	buildClient ClientBuilderFunc
	restTimeout *time.Duration
	cache       cache.CacheServer

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

// NewStore creates a new client store that will use the specified informers to
// maintain a cache of clientsets, rest.Configs, and informers for target
// clusters.
func NewStore(
	buildClient ClientBuilderFunc,
	secretInformer corev1informer.SecretInformer,
	clusterInformer shipperinformer.ClusterInformer,
	ns string,
	restTimeout *time.Duration,
) *Store {
	s := &Store{
		ns:          ns,
		buildClient: buildClient,
		restTimeout: restTimeout,
		cache:       cache.NewServer(),

		secretInformer:  secretInformer,
		clusterInformer: clusterInformer,

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

	glog.Info("Waiting for client store informer caches to sync")

	ok := kubecache.WaitForCacheSync(
		stopCh,
		s.secretInformer.Informer().HasSynced,
		s.clusterInformer.Informer().HasSynced,
	)

	if !ok {
		runtime.HandleError(fmt.Errorf("failed to sync caches for the ClusterClientStore"))
		return
	}

	glog.Info("Starting cluster client store workers")

	go s.cache.Serve()
	go wait.Until(s.clusterWorker, time.Second, stopCh)
	go wait.Until(s.secretWorker, time.Second, stopCh)

	glog.Info("client store is running")
	defer glog.Info("shutting down client store...")

	<-stopCh
}

func (s *Store) AddSubscriptionCallback(subscription SubscriptionRegisterFunc) {
	s.subscriptionRegisterFuncs = append(s.subscriptionRegisterFuncs, subscription)
}

func (s *Store) AddEventHandlerCallback(eventHandler EventHandlerRegisterFunc) {
	s.eventHandlerRegisterFuncs = append(s.eventHandlerRegisterFuncs, eventHandler)
}

// GetClient returns a client for the specified cluster name.
func (s *Store) GetClient(clusterName string) (kubernetes.Interface, error) {
	cluster, ok := s.cache.Fetch(clusterName)
	if !ok {
		return nil, cache.ErrClusterNotInStore
	}

	return cluster.GetClient()
}

// GetConfig returns a rest.Config for the specified cluster name.
func (s *Store) GetConfig(clusterName string) (*rest.Config, error) {
	cluster, ok := s.cache.Fetch(clusterName)
	if !ok {
		return nil, cache.ErrClusterNotInStore
	}

	return cluster.GetConfig()
}

// GetInformerFactory returns an informer factory for the specified
// cluster name.
func (s *Store) GetInformerFactory(clusterName string) (kubeinformers.SharedInformerFactory, error) {
	cluster, ok := s.cache.Fetch(clusterName)
	if !ok {
		return nil, cache.ErrClusterNotInStore
	}

	return cluster.GetInformerFactory()
}

func (s *Store) syncCluster(name string) error {
	// No splitting here because clusters are not namespaced.
	clusterObj, err := s.clusterInformer.Lister().Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			glog.Infof("Cluster %q has been deleted; purging it from client store", name)
			s.cache.Remove(name)
			return nil
		}

		return err
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
		if err == nil || err == cache.ErrClusterNotReady {
			if config != nil && config.Host == clusterObj.Spec.APIMaster {
				glog.Infof("Cluster %q syncing, but we already have a client with the right host in the cache", name)
				return nil
			}
		}
	}

	secret, err := s.secretInformer.Lister().Secrets(s.ns).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			glog.Infof("Cluster %q has no corresponding secret", name)
			return nil
		}

		return err
	}

	return s.create(clusterObj, secret)
}

func (s *Store) syncSecret(key string) error {
	ns, name, err := kubecache.SplitMetaNamespaceKey(key)
	if err != nil {
		return fmt.Errorf("invalid resource key: %q", key)
	}

	// Programmer error: there's a filter func on the callbacks before things get
	// enqueued.
	if ns != s.ns {
		panic("client store secret workqueue should only contain secrets from the shipper namespace")
	}

	secret, err := s.secretInformer.Lister().Secrets(s.ns).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			glog.Infof("Secret %q has been deleted; purging any associated client from client store", key)
			s.cache.Remove(name)
			return nil
		}

		return err
	}

	clusterObj, err := s.clusterInformer.Lister().Get(secret.Name)
	if err != nil {
		if errors.IsNotFound(err) {
			glog.Infof("Secret %q has no corresponding cluster", key)
			return nil
		}

		return err
	}

	checksum, ok := secret.GetAnnotations()[shipper.SecretChecksumAnnotation]
	if !ok {
		return fmt.Errorf("secret %q looks like a cluster secret but doesn't have a checksum", key)
	}

	cachedCluster, ok := s.cache.Fetch(secret.Name)
	if ok {
		existingChecksum, err := cachedCluster.GetChecksum()
		// We don't want to regenerate the client if we already have one with the
		// right properties (host or secret checksum) that's either ready (err == nil)
		// or in the process of getting ready. Otherwise we'll refill the cache
		// needlessly, or could even end up in a livelock where waiting for informer
		// cache to fill takes longer than the resync period, and resync resets the
		// informer.
		if err == nil || err == cache.ErrClusterNotReady {
			if existingChecksum == checksum {
				glog.Infof("Secret %q syncing but we already have a client based on the same checksum in the cache", key)
				return nil
			}
		}
	}

	return s.create(clusterObj, secret)
}

func (s *Store) create(cluster *shipper.Cluster, secret *corev1.Secret) error {
	checksum, ok := secret.GetAnnotations()[shipper.SecretChecksumAnnotation]
	// Programmer error: this is filtered for at the informer level.
	if !ok {
		panic(fmt.Sprintf("Secret %q doesn't have a checksum annotation. this should be checked before calling 'create'", secret.Name))
	}

	config, err := buildConfig(cluster.Spec.APIMaster, secret, s.restTimeout)
	if err != nil {
		return fmt.Errorf("create client configuration for Cluster %q: %s", cluster.Name, err)
	}

	client, err := s.buildClient(cluster.Name, config)
	if err != nil {
		return fmt.Errorf("create client for Cluster %q: %s", cluster.Name, err)
	}

	// These are only used in shared informers. Setting HTTP timeout here would
	// affect watches which is undesirable. Instead, we leave it to client-go (see
	// k8s.io/client-go/tools/cache) to govern watch durations.
	informerConfig, err := buildConfig(cluster.Spec.APIMaster, secret, nil)
	if err != nil {
		return fmt.Errorf("build informer client configuration for Cluster %q: %s", cluster.Name, err)
	}

	informerClient, err := s.buildClient(cluster.Name, informerConfig)
	if err != nil {
		return fmt.Errorf("create informer client for Cluster %q: %s", cluster.Name, err)
	}

	// TODO(asurikov): propagate -resync from cmd/shipper here.
	informerFactory := kubeinformers.NewSharedInformerFactory(informerClient, time.Second*30)
	// Register all the resources that the controllers are interested in, e.g.
	// informerFactory.Core().V1().Pods().Informer().
	for _, cb := range s.subscriptionRegisterFuncs {
		cb(informerFactory)
	}

	clusterName := cluster.Name
	newCachedCluster := cache.NewCluster(clusterName, checksum, client, config, informerFactory, func() {
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
			glog.Infof("found %q annotation with value %q for host %q", shipper.SecretClusterSkipTlsVerifyAnnotation, encodedInsecureSkipTlsVerify, host)
			config.Insecure = insecureSkipTlsVerify
		} else {
			glog.Infof("found %q annotation with value %q for host %q but failed to decode a bool from it, ignoring it", shipper.SecretClusterSkipTlsVerifyAnnotation, encodedInsecureSkipTlsVerify, host)
		}
	}

	return config, nil
}
