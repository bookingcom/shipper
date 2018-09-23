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
	return "FailedKubeAPICall"
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
	FailedAPICall
	ns  string
	gvk schema.GroupVersionKind
}

func IsFailedCRUD(err error) bool {
	_, ok := err.(FailedCRUD)
	return ok
}

func (e FailedCRUD) Error() string {
	return fmt.Sprintf(
		"Failed CRUD API call: %s %s/%s/%s: %q", e.verb, printGVK(e.gvk), e.ns, e.resource, e.err,
	)
}

func NewFailedCRUD(verb string, object kubeObject, err error) FailedCRUD {
	return FailedCRUD{
		FailedAPICall: FailedAPICall{
			verb:     verb,
			resource: object.GetName(),
			err:      err,
		},
		gvk: object.GetObjectKind().GroupVersionKind(),
		ns:  object.GetNamespace(),
	}
}

type UnknownResource struct {
	gvk schema.GroupVersionKind
}

func IsUnknownResource(err error) bool {
	_, ok := err.(UnknownResource)
	return ok
}

func (e UnknownResource) Error() string {
	return fmt.Sprintf(
		"Cluster does not know about resource %s", printGVK(e.gvk),
	)
}

func NewUnknownResource(gvk schema.GroupVersionKind) UnknownResource {
	return UnknownResource{
		gvk: gvk,
	}
}
