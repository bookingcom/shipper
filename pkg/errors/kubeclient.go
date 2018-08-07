package errors

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

type apiVerbs struct {
	Create string
	Get    string
	Update string
	Delete string

	Discover string
	List     string
}

var API = &apiVerbs{
	Create: "CREATE",
	Get:    "GET",
	Update: "UPDATE",
	Delete: "DELETE",

	Discover: "DISCOVER",
	List:     "LIST",
}

type FailedAPICall struct {
	verb     string
	resource string

	err error
}

func IsFailedAPICall(err error) bool {
	_, ok := err.(FailedAPICall)
	return ok
}

func (e FailedAPICall) Name() string {
	return "FailedAPICall"
}

func (e FailedAPICall) Error() string {
	return fmt.Sprintf(
		"Failed API call: %s %s: %q", e.verb, e.resource, e.err,
	)
}

func (e FailedAPICall) ShouldRetry() bool {
	return true
}

func NewFailedAPICall(verb, resource string, err error) FailedAPICall {
	return FailedAPICall{
		verb:     verb,
		resource: resource,

		err: err,
	}
}

type FailedCRUD struct {
	verb     string
	ns       string
	resource string
	gvk      schema.GroupVersionKind

	err error
}

func IsFailedCRUD(err error) bool {
	_, ok := err.(FailedCRUD)
	return ok
}

func (e FailedCRUD) Name() string {
	return "FailedCRUD"
}

func (e FailedCRUD) Error() string {
	return fmt.Sprintf(
		"Failed CRUD call: %s %s/%s/%s: %q", e.verb, printGVK(e.gvk), e.ns, e.resource, e.err,
	)
}

func (e FailedCRUD) ShouldRetry() bool {
	return true
}

func NewFailedCRUD(verb string, object kubeObject, err error) FailedCRUD {
	return FailedCRUD{
		verb:     verb,
		gvk:      object.GetObjectKind().GroupVersionKind(),
		ns:       object.GetNamespace(),
		resource: object.GetName(),
		err:      err,
	}
}
