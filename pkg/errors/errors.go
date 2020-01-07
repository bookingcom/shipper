package errors

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog"
)

// kubeobj is close to an union of Object and Type, as there's no interface in
// apimachinery that has them both together, and it's used in a few places here
// to get k8s objects as a parameter.
type kubeobj interface {
	GetName() string
	GetNamespace() string
	GroupVersionKind() schema.GroupVersionKind
}

// RetryAware is an error that knows if the action that caused it should be
// retried.
type RetryAware interface {
	ShouldRetry() bool
}

// ShouldRetry determines if err should be retried. It trusts err.ShouldRetry
// if err implements RetryAware, but otherwise assumes that all errors should
// be retried, in order not to stop retries for errors that haven't been
// classified yet.
func ShouldRetry(err error) bool {
	retryAware, ok := err.(RetryAware)
	if ok {
		return retryAware.ShouldRetry()
	}

	klog.V(4).Infof("Cannot determine if untagged error %#v is retriable, will assume it is", err)

	return true
}

// RecoverableError is a generic error that will cause an action to be retried.
// It mostly behaves like any other error that doesn't implement the RetryAware
// interface, but by using it we signal that this is an error that we're
// consciously willing to retry, so it is preferred over bare generic errors.
type RecoverableError struct {
	err error
}

func (e RecoverableError) ShouldRetry() bool {
	return true
}

func (e RecoverableError) Error() string {
	return e.err.Error()
}

func NewRecoverableError(err error) RecoverableError {
	return RecoverableError{err: err}
}

// UnrecoverableError is a generic error that will cause an action to NOT be
// retried and dropped from any worker queues.
type UnrecoverableError struct {
	err error
}

func (e UnrecoverableError) ShouldRetry() bool {
	return false
}

func (e UnrecoverableError) Error() string {
	return e.err.Error()
}

func NewUnrecoverableError(err error) UnrecoverableError {
	return UnrecoverableError{err: err}
}

// MultiError is an collection of errors that implements both RetryAware and
// BroadcastAware.
type MultiError struct {
	Errors []error
}

// Error implements the error interface. It concatenates the messages for all
// the errors in the collection.
func (e *MultiError) Error() string {
	nErrors := len(e.Errors)
	points := make([]string, nErrors)
	for i, err := range e.Errors {
		points[i] = fmt.Sprintf("* %s", err)
	}

	return fmt.Sprintf(
		"%d errors occurred: %s",
		nErrors, strings.Join(points, ";"))
}

// Append appends an error to the collection.
func (e *MultiError) Append(err error) {
	e.Errors = append(e.Errors, err)
}

// Any returns true if there are any errors in the collection.
func (e *MultiError) Any() bool {
	return len(e.Errors) > 0
}

// Flatten unboxes a MultiError into a single error if there's only one in the
// collection, nil if there are none, or itself otherwise.
func (e *MultiError) Flatten() error {
	l := len(e.Errors)
	if l == 0 {
		return nil
	} else if l == 1 {
		return e.Errors[0]
	} else {
		return e
	}
}

// ShouldRetry returns true when at least one error in the collection
// should be retried, and false otherwise.
func (e *MultiError) ShouldRetry() bool {
	for _, err := range e.Errors {
		if ShouldRetry(err) {
			return true
		}
	}

	return false
}

func NewMultiError() *MultiError {
	return &MultiError{}
}
