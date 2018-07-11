package installation

import "fmt"

type DecodeManifestError error

func NewDecodeManifestError(format string, args ...interface{}) DecodeManifestError {
	return DecodeManifestError(fmt.Errorf(format, args...))
}

func IsDecodeManifestError(err error) bool {
	_, ok := err.(DecodeManifestError)
	return ok
}

type ConvertUnstructuredError error

func NewConvertUnstructuredError(format string, args ...interface{}) ConvertUnstructuredError {
	return ConvertUnstructuredError(fmt.Errorf(format, args...))
}

func IsConvertUnstructuredError(err error) bool {
	_, ok := err.(ConvertUnstructuredError)
	return ok
}

type ResourceClientError error

func NewResourceClientError(format string, args ...interface{}) ResourceClientError {
	return ResourceClientError(fmt.Errorf(format, args...))
}

func IsResourceClientError(err error) bool {
	_, ok := err.(ResourceClientError)
	return ok
}

type CreateResourceError error

func NewCreateResourceError(format string, args ...interface{}) CreateResourceError {
	return CreateResourceError(fmt.Errorf(format, args...))
}

func IsCreateResourceError(err error) bool {
	_, ok := err.(CreateResourceError)
	return ok
}

type UpdateResourceError error

func NewUpdateResourceError(format string, args ...interface{}) CreateResourceError {
	return UpdateResourceError(fmt.Errorf(format, args...))
}

type GetResourceError error

func NewGetResourceError(format string, args ...interface{}) GetResourceError {
	return GetResourceError(fmt.Errorf(format, args...))
}

func IsGetResourceError(err error) bool {
	_, ok := err.(GetResourceError)
	return ok
}

type RenderManifestError error

type NotContenderError error

func NewNotContenderError(format string, args ...interface{}) NotContenderError {
	return NotContenderError(fmt.Errorf(format, args...))
}

func IsNotContenderError(err error) bool {
	_, ok := err.(NotContenderError)
	return ok
}

type IncompleteReleaseError error

func NewIncompleteReleaseError(format string, args ...interface{}) IncompleteReleaseError {
	return IncompleteReleaseError(fmt.Errorf(format, args...))
}

func IsIncompleteReleaseError(err error) bool {
	_, ok := err.(IncompleteReleaseError)
	return ok
}