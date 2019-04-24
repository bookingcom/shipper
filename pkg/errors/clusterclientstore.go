package errors

import (
	"fmt"
)

type ClusterNotInStoreError struct {
	clusterName string
}

func (e ClusterNotInStoreError) Error() string {
	return fmt.Sprintf("cluster %q unknown; please check if cluster exists in management cluster", e.clusterName)
}

func (e ClusterNotInStoreError) ShouldRetry() bool {
	return true
}

func NewClusterNotInStoreError(clusterName string) error {
	return ClusterNotInStoreError{clusterName: clusterName}
}

func IsClusterNotInStoreError(err error) bool {
	_, ok := err.(ClusterNotInStoreError)
	return ok
}

type ClusterNotReadyError struct {
	clusterName string
}

func (e ClusterNotReadyError) Error() string {
	return fmt.Sprintf("cluster %q not ready for use yet; cluster client is being initialized", e.clusterName)
}

func (e ClusterNotReadyError) ShouldRetry() bool {
	return true
}

func (e ClusterNotReadyError) ShouldBroadcast() bool {
	return false
}

func NewClusterNotReadyError(clusterName string) error {
	return ClusterNotReadyError{clusterName: clusterName}
}

func IsClusterNotReadyError(err error) bool {
	_, ok := err.(ClusterNotReadyError)
	return ok
}

type ClusterClientBuildError struct {
	clusterName string
	err         error
}

func (e ClusterClientBuildError) Error() string {
	return fmt.Sprintf("cannot create client for cluster %q: %s", e.clusterName, e.err.Error())
}

func (e ClusterClientBuildError) ShouldRetry() bool {
	return true
}

func NewClusterClientBuild(clusterName string, err error) error {
	return ClusterClientBuildError{
		clusterName: clusterName,
		err:         err,
	}
}

func IsClusterClientStoreError(err error) bool {
	switch err.(type) {
	case ClusterNotReadyError, ClusterNotInStoreError, ClusterClientBuildError:
		return true
	}

	return false
}
