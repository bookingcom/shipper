package errors

import (
	"fmt"
)

type InvalidRolloutBlockOverrideError struct {
	RolloutBlockName string
}

func (e InvalidRolloutBlockOverrideError) Error() string {
	return fmt.Sprintf("rollout block with name %q does not exist",
		e.RolloutBlockName)
}

func (e InvalidRolloutBlockOverrideError) ShouldRetry() bool {
	return false
}

func NewInvalidRolloutBlockOverrideError(invalidRolloutBlockName string) InvalidRolloutBlockOverrideError {
	return InvalidRolloutBlockOverrideError{invalidRolloutBlockName}
}

type RolloutBlockError struct {
	RolloutBlockName string
}

func (e RolloutBlockError) Error() string {
	return fmt.Sprintf("rollout block(s) with name(s) %q exist",
		e.RolloutBlockName)
}

func (e RolloutBlockError) ShouldRetry() bool {
	return false
}

func NewRolloutBlockError(rolloutBlockName string) RolloutBlockError {
	return RolloutBlockError{rolloutBlockName}
}
