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
