package errors

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime/schema"

	shipperV1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
)

type UnreadableManifest struct {
	err error
}

func IsUnreadableManifest(err error) bool {
	_, ok := err.(UnreadableManifest)
	return ok
}

func (e UnreadableManifest) Name() string {
	return "UnreadableManifest"
}

func (e UnreadableManifest) Error() string {
	return fmt.Sprintf(
		"error decoding manifest: %s", e.err,
	)
}

func (e UnreadableManifest) ShouldRetry() bool {
	return false
}

func NewUnreadableManifest(err error) UnreadableManifest {
	return UnreadableManifest{
		err: err,
	}
}

type CannotConvertUnstructured struct {
	gvk  schema.GroupVersionKind
	name string
	err  error
}

func IsCannotConvertUnstructured(err error) bool {
	_, ok := err.(CannotConvertUnstructured)
	return ok
}

func (e CannotConvertUnstructured) Name() string {
	return "CannotConvertUnstructured"
}

func (e CannotConvertUnstructured) Error() string {
	return fmt.Sprintf(
		"cannot convert %s/%s to unstructured: %s", printGVK(e.gvk), e.name, e.err,
	)
}

func (e CannotConvertUnstructured) ShouldRetry() bool {
	return false
}

func NewCannotConvertUnstructured(object kubeObject, err error) CannotConvertUnstructured {
	return CannotConvertUnstructured{
		gvk:  object.GetObjectKind().GroupVersionKind(),
		name: object.GetName(),
		err:  err,
	}
}

type CannotBuildResourceClient struct {
	gvk schema.GroupVersionKind
	err error
}

func IsCannotBuildResourceClient(err error) bool {
	_, ok := err.(CannotBuildResourceClient)
	return ok
}

func (e CannotBuildResourceClient) Name() string {
	return "CannotBuildResourceClient"
}

func (e CannotBuildResourceClient) Error() string {
	return fmt.Sprintf(
		"Cannot build client for %s: %s", printGVK(e.gvk), e.err,
	)
}

func (e CannotBuildResourceClient) ShouldRetry() bool {
	return false
}

func NewCannotBuildResourceClient(gvk schema.GroupVersionKind, err error) CannotBuildResourceClient {
	return CannotBuildResourceClient{
		gvk: gvk,
		err: err,
	}
}

type IncompleteRelease struct {
	ns   string
	name string
}

func IsIncompleteRelease(err error) bool {
	_, ok := err.(IncompleteRelease)
	return ok
}

func (e IncompleteRelease) Name() string {
	return "IncompleteRelease"
}

func (e IncompleteRelease) Error() string {
	return fmt.Sprintf(
		`Release "%s/%s" misses the required label %q`, e.ns, e.name, shipperV1.ReleaseLabel,
	)
}

func (e IncompleteRelease) ShouldRetry() bool {
	return false
}

func NewIncompleteRelease(ns, name string) IncompleteRelease {
	return IncompleteRelease{
		ns:   ns,
		name: name,
	}
}
