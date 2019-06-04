package errors

import (
	"fmt"
)

type InvalidRolloutBlockOverrideError string

func (e InvalidRolloutBlockOverrideError) Error() string {
	return string(e)
}

func (e InvalidRolloutBlockOverrideError) ShouldRetry() bool {
	return false
}

func NewInvalidRolloutBlockOverrideError(invalidRBname string) InvalidRolloutBlockOverrideError {
	return InvalidRolloutBlockOverrideError(fmt.Sprintf("rollout block with name %s does not exists",
		invalidRBname))
}

type RolloutBlockError string

func (e RolloutBlockError) Error() string {
	return string(e)
}

func (e RolloutBlockError) ShouldRetry() bool {
	return true
}

func NewRolloutBlockError(invalidRBname string) RolloutBlockError {
	return RolloutBlockError(fmt.Sprintf("rollout block(s) with name(s) %s exist",
		invalidRBname))
}
