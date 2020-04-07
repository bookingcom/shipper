package errors

import (
	"fmt"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
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

func (e UnexpectedObjectCountFromSelectorError) ShouldRetry() bool {
	return false
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

type WrongOwnerReferenceError struct {
	object kubeobj
	owner  kubeobj
}

func (e WrongOwnerReferenceError) Error() string {
	return fmt.Sprintf(`%s "%s/%s" does not belong to %s "%s/%s"`,
		e.object.GroupVersionKind().Kind, e.object.GetNamespace(), e.object.GetName(),
		e.owner.GroupVersionKind().Kind, e.owner.GetNamespace(), e.owner.GetName())
}

func (e WrongOwnerReferenceError) ShouldRetry() bool {
	return false
}

func IsWrongOwnerReferenceError(err error) bool {
	_, ok := err.(WrongOwnerReferenceError)
	return ok
}

func NewWrongOwnerReferenceError(object, owner kubeobj) WrongOwnerReferenceError {
	return WrongOwnerReferenceError{
		object: object,
		owner:  owner,
	}
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
