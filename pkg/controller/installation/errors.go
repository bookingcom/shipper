package installation

import "fmt"

type DecodeManifestError error

func NewDecodeManifestError(format string, args ...interface{}) DecodeManifestError {
	return DecodeManifestError(fmt.Errorf(format, args...))
}

type ConvertUnstructuredError error

func NewConvertUnstructuredError(format string, args ...interface{}) ConvertUnstructuredError {
	return ConvertUnstructuredError(fmt.Errorf(format, args...))
}

type ResourceClientError error

func NewResourceClientError(format string, args ...interface{}) ResourceClientError {
	return ResourceClientError(fmt.Errorf(format, args...))
}

type CreateResourceError error

func NewCreateResourceError(format string, args ...interface{}) CreateResourceError {
	return CreateResourceError(fmt.Errorf(format, args...))
}

type RenderManifestError error
