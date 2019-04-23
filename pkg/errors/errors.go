// Package errors provides detailed error types for Shipper issues.

// Shipper errors can be classified into recoverable and unrecoverable errors.

// A recoverable error represents a transient issue that is expected to
// eventually recover, either on its own (a cluster that was unreachable and
// becomes reachable again) or through use action on resources we cannot
// observe (like an agent acquiring access rights through an external party).
// Actions causing such an error are expected to be retried indefinitely until
// they eventually succeed.

// An unrecoverable error represents an issue that is not expected to ever
// succeed by retrying the actions that caused it without being corrected by
// the user, such as trying to create a malformed or inherently invalid object.
// The actions that cause these errors are NOT expected to be retried, and
// should be permanently dropped from any worker queues.
package errors

import (
	"fmt"
	"github.com/golang/glog"
	"strings"
)

// RetryAware is an error that knows if the action that caused it should be
// retried.
type RetryAware interface {
	ShouldRetry() bool
}

type BroadcastAware interface {
	ShouldBroadcast() bool
}

func ShouldRetry(err error) bool {
	retryAware, ok := err.(RetryAware)
	if ok {
		return retryAware.ShouldRetry()
	}

	glog.V(8).Infof("Cannot determine if untagged error %#v is retriable, will assume it is", err)

	return true
}

func ShouldBroadcast(err error) bool {
	broadcastAware, ok := err.(BroadcastAware)
	if ok {
		return broadcastAware.ShouldBroadcast()
	}

	glog.V(8).Infof("Cannot determine if untagged error %#v is broadcastable, will assume it is", err)

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

func (e RecoverableError) ShouldBroadcast() bool {
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

func (e UnrecoverableError) ShouldBroadcast() bool {
	return true
}

func (e UnrecoverableError) Error() string {
	return e.err.Error()
}

func NewUnrecoverableError(err error) UnrecoverableError {
	return UnrecoverableError{err: err}
}

// MultiError is an collection of errors that is retry aware, so it knows if any
// of the collected errors are to be retried.
type MultiError struct {
	Errors []error
}

func NewMultiError() *MultiError {
	return &MultiError{}
}

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

func (e *MultiError) Append(err error) {
	e.Errors = append(e.Errors, err)
}

func (e *MultiError) Any() bool {
	return len(e.Errors) > 0
}

func (e *MultiError) ShouldRetry() bool {
	for _, err := range e.Errors {
		if ShouldRetry(err) {
			return true
		}
	}

	return false
}

func (e *MultiError) ShouldBroadcast() bool {
	for _, err := range e.Errors {
		if ShouldBroadcast(err) {
			return true
		}
	}

	return false
}
