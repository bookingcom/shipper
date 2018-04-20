package traffic

import "fmt"

type TargetClusterServiceError error

func NewTargetClusterFetchServiceFailedError(
	clusterName string,
	selector string,
	namespace string,
	err error,
) TargetClusterServiceError {
	return TargetClusterServiceError(fmt.Errorf(
		`cluster error (%q): failed to fetch Service matching %q in namespace %q: %s`,
		clusterName, selector, namespace, err))
}

func NewTargetClusterWrongServiceCountError(
	clusterName string,
	selector string,
	namespace string,
	serviceCount int,
) TargetClusterServiceError {
	return TargetClusterServiceError(fmt.Errorf(
		"cluster error (%q): expected exactly one Service in namespace %q matching %q, but got %d",
		clusterName, namespace, selector, serviceCount))
}

func NewTargetClusterServiceMissesSelectorError(
	clusterName string,
	namespace string,
	serviceName string,
) TargetClusterServiceError {
	return TargetClusterServiceError(fmt.Errorf(
		"cluster error (%q): service %s/%s does not have a selector set. this means we cannot do label-based canary deployment",
		clusterName, namespace, serviceName))
}

type TargetClusterTrafficError error

func NewTargetClusterTrafficModifyingLabelError(
	clusterName string,
	namespace string,
	podName string,
	err error,
) TargetClusterTrafficError {
	return TargetClusterTrafficError(fmt.Errorf(
		"pod error (%s/%s/%s): failed to add traffic label: %q",
		clusterName, namespace, podName, err.Error()))
}

type TargetClusterPodListingError error

func NewTargetClusterPodListingError(
	clusterName string,
	namespace string,
	err error,
) TargetClusterPodListingError {
	return TargetClusterPodListingError(fmt.Errorf(
		"cluster error (%q): failed to list pods in '%s': %q",
		clusterName, namespace, err.Error()))
}

func NewTargetClusterReleasePodListingError(
	releaseName string,
	clusterName string,
	namespace string,
	err error,
) TargetClusterPodListingError {
	return TargetClusterPodListingError(fmt.Errorf(
		"release error (%q): failed to list pods in '%s/%s': %q",
		releaseName, clusterName, namespace, err.Error()))
}

type TargetClusterMathError error

func NewTargetClusterMathError(
	releaseName string,
	idlePodCount int,
	missingCount int,
) TargetClusterMathError {
	return TargetClusterMathError(fmt.Errorf(
		"release error (%q): the math is broken: there aren't enough idle pods (%d) to meet requested increase in traffic pods (%d)",
		releaseName,
		idlePodCount,
		missingCount))
}
