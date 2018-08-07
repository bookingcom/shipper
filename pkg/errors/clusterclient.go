package errors

import (
	"fmt"
)

type ClusterNotReady struct {
	cluster string
	state   string
}

func IsClusterNotReady(err error) bool {
	_, ok := err.(ClusterNotReady)
	return ok
}

func (e ClusterNotReady) Name() string {
	return "ClusterNotReady"
}

func (e ClusterNotReady) Error() string {
	return fmt.Sprintf(
		"Cluster %q is not ready for use: it is in state %s", e.cluster, e.state,
	)
}

func (e ClusterNotReady) ShouldRetry() bool {
	return true
}

func NewClusterNotReady(cluster, state string) ClusterNotReady {
	return ClusterNotReady{
		cluster: cluster,
		state:   state,
	}
}

type ClusterNotInStore struct {
	cluster string
}

func IsClusterNotInStore(err error) bool {
	_, ok := err.(ClusterNotInStore)
	return ok
}

func (e ClusterNotInStore) Name() string {
	return "ClusterNotInStore"
}

func (e ClusterNotInStore) Error() string {
	return fmt.Sprintf(
		"Cluster %q is not in the cluster store. This usually means a cluster was added without a corresponding secret.", e.cluster,
	)
}

func (e ClusterNotInStore) ShouldRetry() bool {
	return false
}

func NewClusterNotInStore(cluster string) ClusterNotInStore {
	return ClusterNotInStore{
		cluster: cluster,
	}
}
