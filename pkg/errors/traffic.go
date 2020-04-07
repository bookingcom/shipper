package errors

import (
	"fmt"
)

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
