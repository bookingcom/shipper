package cache

import (
	"sync"

	kubeinformers "k8s.io/client-go/informers"
	kubernetes "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/bookingcom/shipper/pkg/errors"
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
	client kubernetes.Interface,
	config *rest.Config,
	informerFactory kubeinformers.SharedInformerFactory,
	cacheSyncCb func(),
) *cluster {
	return &cluster{
		state:           StateNotReady,
		name:            name,
		checksum:        checksum,
		client:          client,
		config:          config,
		informerFactory: informerFactory,
		cacheSyncCb:     cacheSyncCb,
		stopCh:          make(chan struct{}),
	}
}

type cluster struct {
	name string

	stateMut sync.RWMutex
	state    string

	// These are all read-only after initialization, so no lock needed.
	checksum        string
	client          kubernetes.Interface
	config          *rest.Config
	informerFactory kubeinformers.SharedInformerFactory

	cacheSyncCb func()

	stopCh chan struct{}
}

func (c *cluster) IsReady() bool {
	return c.State() == StateReady
}

func (c *cluster) State() string {
	c.stateMut.RLock()
	defer c.stateMut.RUnlock()
	return c.state
}

func (c *cluster) GetClient() (kubernetes.Interface, error) {
	if !c.IsReady() {
		return c.client, errors.NewClusterNotReady(c.name, c.State())
	}

	return c.client, nil
}

func (c *cluster) GetConfig() (*rest.Config, error) {
	if !c.IsReady() {
		return c.config, errors.NewClusterNotReady(c.name, c.State())
	}

	return c.config, nil
}

func (c *cluster) GetChecksum() (string, error) {
	if !c.IsReady() {
		return c.checksum, errors.NewClusterNotReady(c.name, c.State())
	}

	return c.checksum, nil
}

func (c *cluster) GetInformerFactory() (kubeinformers.SharedInformerFactory, error) {
	if !c.IsReady() {
		return nil, errors.NewClusterNotReady(c.name, c.State())
	}

	return c.informerFactory, nil
}

// This will block until the cache syncs. If the cache is never going to sync
// (because you gave it an invalid hostname, for instance) it will hang around
// until this cluster is Shutdown() and replaced by a new one.
func (c *cluster) WaitForInformerCache() {
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

	c.informerFactory.Start(c.stopCh)
	ok := true
	syncedInformers := c.informerFactory.WaitForCacheSync(c.stopCh)
	for _, synced := range syncedInformers {
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

func (c *cluster) Shutdown() {
	c.stateMut.Lock()
	c.state = StateTerminated
	close(c.stopCh)
	c.stateMut.Unlock()
}

func (c *cluster) Match(other *cluster) bool {
	if other == nil {
		return false
	}

	if other.checksum == c.checksum && other.config.Host == c.config.Host {
		return true
	}

	return false
}
