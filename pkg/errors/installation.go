package errors

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

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

type InstallationTargetOwnershipError struct {
	obj *unstructured.Unstructured
}

func NewInstallationTargetOwnershipError(obj *unstructured.Unstructured) InstallationTargetOwnershipError {
	return InstallationTargetOwnershipError{obj: obj}
}

func (e InstallationTargetOwnershipError) Error() string {
	msg := `%s "%s/%s" cannot be updated as it does not belong to the application. It may have been created by other means, and it conflicts with a matching object in the chart shipper is currently trying to install`
	return fmt.Sprintf(msg, e.obj.GetKind(), e.obj.GetNamespace(), e.obj.GetName())
}

func (e InstallationTargetOwnershipError) ShouldRetry() bool {
	return false
}
