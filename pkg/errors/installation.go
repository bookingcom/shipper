package errors

import "fmt"

type DecodeManifestError struct {
	err error
}

func (e DecodeManifestError) Error() string {
	return e.err.Error()
}

func (e DecodeManifestError) ShouldRetry() bool {
	return false
}

func NewDecodeManifestError(format string, args ...interface{}) DecodeManifestError {
	return DecodeManifestError{fmt.Errorf(format, args...)}
}

func IsDecodeManifestError(err error) bool {
	_, ok := err.(DecodeManifestError)
	return ok
}

type ConvertUnstructuredError struct {
	err error
}

func (e ConvertUnstructuredError) Error() string {
	return e.err.Error()
}

func (e ConvertUnstructuredError) ShouldRetry() bool {
	return false
}

func NewConvertUnstructuredError(format string, args ...interface{}) ConvertUnstructuredError {
	return ConvertUnstructuredError{fmt.Errorf(format, args...)}
}

func IsConvertUnstructuredError(err error) bool {
	_, ok := err.(ConvertUnstructuredError)
	return ok
}

type RenderManifestError struct {
	err error
}

func (e RenderManifestError) Error() string {
	return e.err.Error()
}

func (e RenderManifestError) ShouldRetry() bool {
	return false
}

func NewRenderManifestError(err error) RenderManifestError {
	return RenderManifestError{err}
}

type IncompleteReleaseError struct {
	err error
}

func (e IncompleteReleaseError) Error() string {
	return e.err.Error()
}

func (e IncompleteReleaseError) ShouldRetry() bool {
	return false
}

func NewIncompleteReleaseError(format string, args ...interface{}) IncompleteReleaseError {
	return IncompleteReleaseError{fmt.Errorf(format, args...)}
}

func IsIncompleteReleaseError(err error) bool {
	_, ok := err.(IncompleteReleaseError)
	return ok
}

// Incomplete release should not retry
