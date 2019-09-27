package errors

import (
	"fmt"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

type UnexpectedObjectCountFromSelectorError struct {
	selector labels.Selector
	gvk      schema.GroupVersionKind
	expected int
	got      int
}

func (e UnexpectedObjectCountFromSelectorError) Error() string {
	return fmt.Sprintf("expected %d %s for selector %q, got %d instead",
		e.expected, e.gvk.String(), e.selector.String(), e.got)
}

func NewUnexpectedObjectCountFromSelectorError(
	selector labels.Selector,
	gvk schema.GroupVersionKind,
	expected, got int,
) UnexpectedObjectCountFromSelectorError {
	return UnexpectedObjectCountFromSelectorError{
		selector: selector,
		gvk:      gvk,
		expected: expected,
		got:      got,
	}
}

func (e UnexpectedObjectCountFromSelectorError) ShouldRetry() bool {
	return false
}

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
