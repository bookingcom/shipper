package errors

import (
	"fmt"
)

type SelfAwareError interface {
	error
	Name() string
	ShouldRetry() bool
}

func Classify(err error) SelfAwareError {
	selfAware, ok := err.(SelfAwareError)
	if !ok {
		return NewUnknown(err)
	}
	return selfAware
}

type AggregatedSelfAwareError struct {
	errors []SelfAwareError
}

func (a *AggregatedSelfAwareError) Collect(err SelfAwareError) {
	a.errors = append(a.errors, err)
}

// in the interest of doing as much work as possible, if any of the errors we
// collect can be retried, let's retry
func (a *AggregatedSelfAwareError) ShouldRetry() bool {
	for _, err := range a.errors {
		if err.ShouldRetry() {
			return true
		}
	}
	return false
}

type Unknown struct {
	err error
}

func IsUnknown(err error) bool {
	_, ok := err.(Unknown)
	return ok
}

func (e Unknown) Name() string {
	return "Unknown"
}

func (e Unknown) Error() string {
	return fmt.Sprintf(
		"An unknown error occurred: %q", e.err,
	)
}

func (e Unknown) ShouldRetry() bool {
	return true
}

func NewUnknown(err error) Unknown {
	return Unknown{
		err: err,
	}
}
