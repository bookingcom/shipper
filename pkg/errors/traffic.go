package errors

import (
	"fmt"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

type MissingShipperLabelError struct {
	tt    *shipper.TrafficTarget
	label string
}

func (e MissingShipperLabelError) Error() string {
	return fmt.Sprintf(
		`TrafficTarget "%s/%s" needs a %q label in order to select resources in the target clusters`,
		e.tt.GetNamespace(), e.tt.GetName(), e.label)
}

func (e MissingShipperLabelError) ShouldRetry() bool {
	return false
}

func NewMissingShipperLabelError(tt *shipper.TrafficTarget, label string) MissingShipperLabelError {
	return MissingShipperLabelError{
		tt:    tt,
		label: label,
	}
}

type MissingTrafficWeightsForClusterError struct {
	ns          string
	appName     string
	clusterName string
}

func (e MissingTrafficWeightsForClusterError) Error() string {
	return fmt.Sprintf(`Application "%s/%s" has no traffic weights for cluster %s`,
		e.ns, e.appName, e.clusterName)
}

func (e MissingTrafficWeightsForClusterError) ShouldRetry() bool {
	return false
}

func NewMissingTrafficWeightsForClusterError(ns, appName, clusterName string) MissingTrafficWeightsForClusterError {
	return MissingTrafficWeightsForClusterError{
		ns:          ns,
		appName:     appName,
		clusterName: clusterName,
	}
}

type MissingTargetClusterSelectorError struct {
	ns          string
	serviceName string
}

func (e MissingTargetClusterSelectorError) ShouldRetry() bool {
	return true
}

func (e MissingTargetClusterSelectorError) Error() string {
	return fmt.Sprintf(
		"service %s/%s does not have a selector set. this means we cannot do label-based canary deployment",
		e.ns, e.serviceName)
}

func NewTargetClusterServiceMissesSelectorError(ns, serviceName string) MissingTargetClusterSelectorError {
	return MissingTargetClusterSelectorError{
		ns:          ns,
		serviceName: serviceName,
	}
}

type MultipleTrafficTargetsForReleaseError struct {
	ns          string
	releaseName string
	ttNames     []string
}

func (e MultipleTrafficTargetsForReleaseError) Error() string {
	return fmt.Sprintf(`multiple TrafficTargets for the same release "%s/%s": %v`,
		e.ns, e.releaseName, e.ttNames)
}

func (e MultipleTrafficTargetsForReleaseError) ShouldRetry() bool {
	return false
}

func NewMultipleTrafficTargetsForReleaseError(ns, releaseName string, ttNames []string) MultipleTrafficTargetsForReleaseError {
	return MultipleTrafficTargetsForReleaseError{
		ns:          ns,
		releaseName: releaseName,
		ttNames:     ttNames,
	}
}

type TargetClusterMathError struct {
	releaseName  string
	idlePodCount int
	missingCount int
}

func NewTargetClusterMathError(releaseName string, idlePodCount, missingCount int) TargetClusterMathError {
	return TargetClusterMathError{
		releaseName:  releaseName,
		idlePodCount: idlePodCount,
		missingCount: missingCount,
	}
}

func (e TargetClusterMathError) Error() string {
	return fmt.Sprintf(
		"release error (%q): the math is broken: there aren't enough idle pods (%d) to meet requested increase in traffic pods (%d)",
		e.releaseName, e.idlePodCount, e.missingCount)
}
