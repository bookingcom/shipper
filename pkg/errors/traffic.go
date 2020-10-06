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
