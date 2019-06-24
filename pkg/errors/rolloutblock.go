package errors

import (
	"fmt"
)

type InvalidRolloutBlockOverrideError struct {
	RolloutBlockName string
}

func (e InvalidRolloutBlockOverrideError) Error() string {
	return fmt.Sprintf("rollout block with name %s does not exists",
		e.RolloutBlockName)
}

func (e InvalidRolloutBlockOverrideError) ShouldRetry() bool {
	return true
}

func NewInvalidRolloutBlockOverrideError(invalidRolloutBlockName string) InvalidRolloutBlockOverrideError {
	return InvalidRolloutBlockOverrideError{invalidRolloutBlockName}
}

type RolloutBlockError string

func (e RolloutBlockError) Error() string {
	return string(e)
}

func (e RolloutBlockError) ShouldRetry() bool {
	return true
}

func NewRolloutBlockError(invalidRolloutBlockName string) RolloutBlockError {
	return RolloutBlockError(fmt.Sprintf("rollout block(s) with name(s) %s exist",
		invalidRolloutBlockName))
}
