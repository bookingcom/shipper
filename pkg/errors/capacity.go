package errors

import (
	"fmt"
)

type CapacityInProgressError string

func (e CapacityInProgressError) Error() string {
	return string(e)
}

func (e CapacityInProgressError) ShouldRetry() bool {
	return true
}

func NewCapacityInProgressError(ctName string) CapacityInProgressError {
	return CapacityInProgressError(fmt.Sprintf("capacity target %s in progress",
		ctName))
}
