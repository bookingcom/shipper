package controller

import (
	"fmt"
	"k8s.io/apimachinery/pkg/types"
)

type MultipleOwnerReferencesError string

func (e MultipleOwnerReferencesError) Error() string {
	return string(e)
}

func (e MultipleOwnerReferencesError) ShouldRetry() bool {
	return false
}

func IsMultipleOwnerReferencesError(err error) bool {
	_, ok := err.(MultipleOwnerReferencesError)
	return ok
}

func NewMultipleOwnerReferencesError(name string, references int) MultipleOwnerReferencesError {
	return MultipleOwnerReferencesError(fmt.Sprintf(
		"expected exactly one owner for object %q, got %d",
		name, references))
}

type WrongOwnerReferenceError string

func (e WrongOwnerReferenceError) Error() string {
	return string(e)
}

func (e WrongOwnerReferenceError) ShouldRetry() bool {
	return false
}

func IsWrongOwnerReferenceError(err error) bool {
	_, ok := err.(WrongOwnerReferenceError)
	return ok
}

func NewWrongOwnerReferenceError(name string, expectedUID, gotUID types.UID) WrongOwnerReferenceError {
	return WrongOwnerReferenceError(fmt.Sprintf(
		"the owner Release for InstallationTarget %q is gone; expected UID %s but got %s",
		name,
		expectedUID,
		gotUID,
	))
}

type InvalidChartError struct {
	message string
}

func (e InvalidChartError) Error() string {
	return e.message
}

func (e InvalidChartError) ShouldRetry() bool {
	return false
}

func IsInvalidChartError(err error) bool {
	_, ok := err.(InvalidChartError)
	return ok
}

func NewInvalidChartError(m string) InvalidChartError {
	return InvalidChartError{message: m}
}
