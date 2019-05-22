package errors

import (
	"errors"
	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"testing"
)

func TestKubeclientError_ShouldRetry(t *testing.T) {
	kubeobj := &shipper.Application{}
	gvk := kubeobj.GroupVersionKind()
	kind := gvk.GroupKind()
	resource := schema.GroupResource{Group: gvk.Group, Resource: "application"}
	nonRetriableErrors := []*kerrors.StatusError{
		kerrors.NewBadRequest("reason"),
		kerrors.NewForbidden(resource, "name", errors.New("error")),
		kerrors.NewNotFound(resource, "name"),
		kerrors.NewMethodNotSupported(resource, "action"),
		kerrors.NewGone("message"),
		kerrors.NewInvalid(kind, "name", nil),
	}

	for _, err := range nonRetriableErrors {
		kerr := NewKubeclientUpdateError(kubeobj, err)
		if kerr.ShouldRetry() {
			t.Errorf("expected to be non-retriable: %T", kerrors.ReasonForError(err))
		}
	}

	retriableErrors := []*kerrors.StatusError{
		kerrors.NewUnauthorized("reason"),
		kerrors.NewConflict(resource, "name", errors.New("error")),
		kerrors.NewTooManyRequests("message", 1),
		kerrors.NewInternalError(errors.New("error")),
		kerrors.NewServiceUnavailable("reason"),
		kerrors.NewServerTimeout(resource, "operation", 1),
	}

	for _, err := range retriableErrors {
		kerr := NewKubeclientUpdateError(kubeobj, err)
		if !kerr.ShouldRetry() {
			t.Errorf("expected to be retriable: %T", kerrors.ReasonForError(err))
		}
	}

	kerr := NewKubeclientUpdateError(kubeobj, errors.New("generic error"))
	if !kerr.ShouldRetry() {
		t.Error("unknown error expected to be retriable")
	}
}
