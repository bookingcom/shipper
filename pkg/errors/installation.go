package errors

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
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

type MissingInstallationTargetOwnerLabelError struct {
	obj *unstructured.Unstructured
}

func NewMissingInstallationTargetOwnerLabelError(obj *unstructured.Unstructured) MissingInstallationTargetOwnerLabelError {
	return MissingInstallationTargetOwnerLabelError{
		obj: obj,
	}
}

func (e MissingInstallationTargetOwnerLabelError) Error() string {
	return fmt.Sprintf(
		`Object "%s/%s" is missing required label %q as it was not created by any InstallationTarget. Shipper doesn't know how to deal with this`,
		e.obj.GetNamespace(), e.obj.GetName(),
		shipper.InstallationTargetOwnerLabel)
}

func (e MissingInstallationTargetOwnerLabelError) ShouldRetry() bool {
	return false
}
