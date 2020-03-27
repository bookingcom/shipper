package cache

import (
	"sync"

	kubeinformers "k8s.io/client-go/informers"
	kubernetes "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
)

const (
	StateReady          = "Ready"
	StateNotReady       = "NotReady"
	StateWaitingForSync = "WaitingForSync"
	StateTerminated     = "Terminated"
)

func NewCluster(
	name,
	checksum string,
	config *rest.Config,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	shipperInformerFactory shipperinformers.SharedInformerFactory,
	buildKubeClient func(string, string, *rest.Config) (kubernetes.Interface, error),
	buildShipperClient func(string, string, *rest.Config) (shipperclientset.Interface, error),
	cacheSyncCb func(),
) *Cluster {
	return &Cluster{
		name:     name,
		checksum: checksum,
		config:   config,

		state: StateNotReady,

		kubeClients:    make(map[string]kubernetes.Interface),
		shipperClients: make(map[string]shipperclientset.Interface),

		kubeInformerFactory: kubeInformerFactory,
		buildKubeClient:     buildKubeClient,

		shipperInformerFactory: shipperInformerFactory,
		buildShipperClient:     buildShipperClient,

		cacheSyncCb: cacheSyncCb,

		stopCh: make(chan struct{}),
	}
}

type Cluster struct {
	// These are all read-only after initialization, so no lock needed.
	name     string
	checksum string
	config   *rest.Config

	stateMut sync.RWMutex
	state    string

	clientsMut     sync.Mutex
	kubeClients    map[string]kubernetes.Interface
	shipperClients map[string]shipperclientset.Interface

	kubeInformerFactory kubeinformers.SharedInformerFactory
	buildKubeClient     func(string, string, *rest.Config) (kubernetes.Interface, error)

	shipperInformerFactory shipperinformers.SharedInformerFactory
	buildShipperClient     func(string, string, *rest.Config) (shipperclientset.Interface, error)

	cacheSyncCb func()

	stopCh chan struct{}
}

func (c *Cluster) IsReady() bool {
	c.stateMut.RLock()
	defer c.stateMut.RUnlock()
	return c.state == StateReady
}

// GetKubeClient returns a kubernetes client for the user agent specified by
// ua. If a client doesn't exist for that user agent, one will be created by
// calling the buildKubeClient func.
func (c *Cluster) GetKubeClient(ua string) (kubernetes.Interface, error) {
	if !c.IsReady() {
		return nil, shippererrors.NewClusterNotReadyError(c.name)
	}

	c.clientsMut.Lock()
	defer c.clientsMut.Unlock()

	if client, ok := c.kubeClients[ua]; ok {
		return client, nil
	}

	client, err := c.buildKubeClient(c.name, ua, c.config)
	if err != nil {
		return nil, err
	}

	c.kubeClients[ua] = client

	return client, nil
}

// GetShipperClient returns a shipperrnetes client for the user agent specified by
// ua. If a client doesn't exist for that user agent, one will be created by
// calling the buildShipperClient func.
func (c *Cluster) GetShipperClient(ua string) (shipperclientset.Interface, error) {
	if !c.IsReady() {
		return nil, shippererrors.NewClusterNotReadyError(c.name)
	}

	c.clientsMut.Lock()
	defer c.clientsMut.Unlock()

	if client, ok := c.shipperClients[ua]; ok {
		return client, nil
	}

	client, err := c.buildShipperClient(c.name, ua, c.config)
	if err != nil {
		return nil, err
	}

	c.shipperClients[ua] = client

	return client, nil
}

func (c *Cluster) GetConfig() (*rest.Config, error) {
	if !c.IsReady() {
		return c.config, shippererrors.NewClusterNotReadyError(c.name)
	}

	return c.config, nil
}

func (c *Cluster) GetChecksum() (string, error) {
	if !c.IsReady() {
		return c.checksum, shippererrors.NewClusterNotReadyError(c.name)
	}

	return c.checksum, nil
}

func (c *Cluster) GetKubeInformerFactory() (kubeinformers.SharedInformerFactory, error) {
	if !c.IsReady() {
		return nil, shippererrors.NewClusterNotReadyError(c.name)
	}

	return c.kubeInformerFactory, nil
}

func (c *Cluster) GetShipperInformerFactory() (shipperinformers.SharedInformerFactory, error) {
	if !c.IsReady() {
		return nil, shippererrors.NewClusterNotReadyError(c.name)
	}

	return c.shipperInformerFactory, nil
}

// This will block until the cache syncs. If the cache is never going to sync
// (because you gave it an invalid hostname, for instance) it will hang around
// until this cluster is Shutdown() and replaced by a new one.
func (c *Cluster) WaitForInformerCache() {
	// No defer unlock here to keep cache sync out of lock scope.
	c.stateMut.Lock()
	if c.state != StateNotReady {
		// This means that something happened and we already changed the state of the
		// cluster cache entry. this is almost always in a test case where we're
		// calling server.Store in close proximity to server.Stop(). If the state has
		// changed to terminated, returning here is a totally sane and safe thing to
		// do: we certainly don't want to warm up any cache.
		c.stateMut.Unlock()
		return
	}
	c.state = StateWaitingForSync
	c.stateMut.Unlock()

	c.kubeInformerFactory.Start(c.stopCh)
	c.shipperInformerFactory.Start(c.stopCh)

	ok := true
	syncedKubeInformers := c.kubeInformerFactory.WaitForCacheSync(c.stopCh)
	for _, synced := range syncedKubeInformers {
		ok = ok && synced
	}

	syncedShipperInformers := c.shipperInformerFactory.WaitForCacheSync(c.stopCh)
	for _, synced := range syncedShipperInformers {
		ok = ok && synced
	}

	if ok {
		// No defer unlock here because I don't want the lock scope to cover the
		// callbacks.
		c.stateMut.Lock()
		if c.state == StateTerminated {
			c.stateMut.Unlock()
			return
		}
		c.state = StateReady
		c.stateMut.Unlock()

		c.cacheSyncCb()
	}
}

func (c *Cluster) Shutdown() {
	c.stateMut.Lock()
	c.state = StateTerminated
	close(c.stopCh)
	c.stateMut.Unlock()
}

func (c *Cluster) Match(other *Cluster) bool {
	if other == nil {
		return false
	}

	if other.checksum == c.checksum && other.config.Host == c.config.Host {
		return true
	}

	return false
}
