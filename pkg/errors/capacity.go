package errors

import (
	"fmt"
)

type InvalidCapacityTargetError struct {
	releaseName string
	count       int
}

func (e InvalidCapacityTargetError) Error() string {
	if e.count < 1 {
		return fmt.Sprintf("missing capacity target with release label %q", e.releaseName)
	}

	return fmt.Sprintf("expected one capacity target for release label %q, got %d instead", e.releaseName, e.count)
}

func (e InvalidCapacityTargetError) ShouldRetry() bool {
	return true
}

func NewInvalidCapacityTargetError(releaseName string, count int) InvalidCapacityTargetError {
	return InvalidCapacityTargetError{
		releaseName: releaseName,
		count:       count,
	}
}

type TargetDeploymentCountError struct {
	cluster   string
	namespace string
	labels    string
	count     int
}

// ShouldRetry tells the capacity controller not to retry a
// TargetDeploymentCountError on its own, as whenever a deployment is created
// or deleted, a new event will be triggered, causing the CapacityTarget to be
// processed only when necessary instead of continously.
func (e TargetDeploymentCountError) ShouldRetry() bool {
	return false
}

func (e TargetDeploymentCountError) Error() string {
	if e.count < 1 {
		return fmt.Sprintf("missing deployment with label %q on cluster %q and namepace %q", e.labels, e.cluster, e.namespace)
	}

	return fmt.Sprintf("expected one deployment with label %q on cluster %q and namespace %q, got %d instead", e.labels, e.cluster, e.namespace, e.count)
}

func NewTargetDeploymentCountError(cluster, ns, labels string, count int) TargetDeploymentCountError {
	return TargetDeploymentCountError{
		cluster:   cluster,
		namespace: ns,
		labels:    labels,
		count:     count,
	}
}
