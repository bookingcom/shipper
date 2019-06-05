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

func NewInvalidRolloutBlockOverrideError(invalidRBname string) InvalidRolloutBlockOverrideError {
	return InvalidRolloutBlockOverrideError{invalidRBname}
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
