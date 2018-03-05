package cache

import (
	"fmt"
	"sync"

	kubeinformers "k8s.io/client-go/informers"
	kubernetes "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
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

	// these are all read-only after initialization, so no lock needed
	checksum        string
	client          kubernetes.Interface
	config          *rest.Config
	informerFactory kubeinformers.SharedInformerFactory

	cacheSyncCb func()

	stopCh chan struct{}
}

func (c *cluster) IsReady() bool {
	c.stateMut.RLock()
	defer c.stateMut.RUnlock()
	return c.state == StateReady
}

func (c *cluster) GetClient() (kubernetes.Interface, error) {
	if !c.IsReady() {
		return c.client, ErrClusterNotReady
	}

	return c.client, nil
}

func (c *cluster) GetConfig() (*rest.Config, error) {
	if !c.IsReady() {
		return c.config, ErrClusterNotReady
	}

	return c.config, nil
}

func (c *cluster) GetChecksum() (string, error) {
	if !c.IsReady() {
		return c.checksum, ErrClusterNotReady
	}

	return c.checksum, nil
}

func (c *cluster) GetInformerFactory() (kubeinformers.SharedInformerFactory, error) {
	if !c.IsReady() {
		return nil, ErrClusterNotReady
	}

	return c.informerFactory, nil
}

// This will block until the cache syncs. If the cache is never going to sync
// (because you gave it an invalid hostname, for instance) it will hang around
// until this cluster is Shutdown() and replaced by a new one.
func (c *cluster) WaitForInformerCache() {
	// no defer unlock here to keep cache sync out of lock scope
	c.stateMut.Lock()
	if c.state != StateNotReady {
		// I expect this panic to take the system down, but if anyone has
		// a recover() floating around we're better citizens if we
		// don't hold this lock forever
		c.stateMut.Unlock()
		panic(fmt.Sprintf(
			"cluster.WaitForInformerCache called on a cluster in state %q, which is not StateNotReady. this should never happen",
			c.state,
		))
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
		// no defer unlock here because I don't want the lock scope to cover the callbacks
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
