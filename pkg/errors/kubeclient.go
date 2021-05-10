package errors

import (
	"fmt"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog"
)

type KubeclientVerb string

const (
	KubeclientVerbCreate   KubeclientVerb = "CREATE"
	KubeclientVerbGet      KubeclientVerb = "GET"
	KubeclientVerbUpdate   KubeclientVerb = "UPDATE"
	KubeclientVerbDelete   KubeclientVerb = "DELETE"
	KubeclientVerbPatch    KubeclientVerb = "PATCH"
	KubeclientVerbList     KubeclientVerb = "LIST"
	KubeclientVerbDiscover KubeclientVerb = "DISCOVER"
)

// KubeclientError is a RetryAware and BroadcastAware wrapper around
// kerrors.APIStatus errors returned by the Kubernetes client.
type KubeclientError struct {
	verb KubeclientVerb
	gvk  schema.GroupVersionKind
	ns   string
	name string
	err  error
}

func (e KubeclientError) Error() string {
	var fqn string
	if e.ns == "" {
		fqn = e.name
	} else {
		fqn = fmt.Sprintf("%s/%s", e.ns, e.name)
	}

	return fmt.Sprintf("failed to %s %s %q: %s", e.verb, e.gvk.Kind, fqn, e.err)
}

// ShouldRetry implements the RetryAware interface, and determines if the error
// should be retried based on its status code. It follows the API conventions
// stipulated by
// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#http-status-codes
func (e KubeclientError) ShouldRetry() bool {
	// client-go does not build its own metav1.Status in some cases,
	// particularly when it gets a response returned by an admission
	// controller. If the admission controller does not return
	// Status.Reason, we'll get an unknown reason here. Luckily, they
	// always return a Status.Code, so we can check it here instead of the
	// reason.
	statuserr, ok := e.err.(kerrors.APIStatus)
	if ok {
		var kind string
		if details := statuserr.Status().Details; details != nil {
			kind = details.Kind
		}
		if
		(kerrors.HasStatusCause(e.err, corev1.NamespaceTerminatingCause) && statuserr.Status().Code == 403) ||
			(kind == "namespaces" && statuserr.Status().Code == 404) {
			// if the namespace is being terminated, or already deleted (namespace not found)
			// we should retry until the namespace is recreated
			return true
		}
		switch statuserr.Status().Code {
		case 400, 403, 404, 405, 410, 422:
			return false
		case 401, 409, 429, 500, 503, 504:
			return true
		}
	}


	klog.V(8).Infof("Cannot determine reason for error %#v, will assume it's retriable", e)
	return true
}

// WithKind returns a new KubeclientError associated with a
// gvk.GroupVersionKind. All KubeclientErrors are expected to have this
// property set, so error messages can be generated with enough information.
func (e KubeclientError) WithKind(gvk schema.GroupVersionKind) KubeclientError {
	e.gvk = gvk
	return e
}

// WithShipperKind returns a new KubeclientError associated with a Shipper GVK.
func (e KubeclientError) WithShipperKind(kind string) KubeclientError {
	return e.WithKind(shipper.SchemeGroupVersion.WithKind(kind))
}

// WithCoreV1Kind returns a new KubeclientError associated with a Kubernetes
// Core v1 GVK.
func (e KubeclientError) WithCoreV1Kind(kind string) KubeclientError {
	return e.WithKind(corev1.SchemeGroupVersion.WithKind(kind))
}

func NewKubeclientErrorFromObject(verb KubeclientVerb, obj kubeobj, err error) KubeclientError {
	return NewKubeclientError(verb, obj.GetNamespace(), obj.GetName(), err)
}

func NewKubeclientError(verb KubeclientVerb, ns, name string, err error) KubeclientError {
	return KubeclientError{
		verb: verb,
		ns:   ns,
		name: name,
		err:  err,
	}
}

func NewKubeclientGetError(ns, name string, err error) KubeclientError {
	return NewKubeclientError(KubeclientVerbGet, ns, name, err)
}

func NewKubeclientDeleteError(ns, name string, err error) KubeclientError {
	return NewKubeclientError(KubeclientVerbDelete, ns, name, err)
}

func NewKubeclientPatchError(ns, name string, err error) KubeclientError {
	return NewKubeclientError(KubeclientVerbPatch, ns, name, err)
}

func NewKubeclientUpdateError(obj kubeobj, err error) KubeclientError {
	return NewKubeclientErrorFromObject(KubeclientVerbUpdate, obj, err)
}

func NewKubeclientCreateError(obj kubeobj, err error) KubeclientError {
	return NewKubeclientErrorFromObject(KubeclientVerbCreate, obj, err)
}

// KubeclientListError is a more specialized version of KubeclientError that
// includes the selector used in a .List() call.
type KubeclientListError struct {
	// embed KubeclientError so we don't need a copy of ShouldRetry
	KubeclientError
	selector labels.Selector
}

func (e KubeclientListError) Error() string {
	return fmt.Sprintf("failed to list %s in namespace %q using selector %q: %s",
		e.gvk.Kind, e.ns, e.selector.String(), e.err.Error())
}

func NewKubeclientListError(gvk schema.GroupVersionKind, ns string, selector labels.Selector, err error) error {
	return KubeclientListError{
		KubeclientError: KubeclientError{
			verb: KubeclientVerbList,
			gvk:  gvk,
			ns:   ns,
			err:  err,
		},
		selector: selector,
	}
}

// KubeclientDiscoverError is a more specialized version of KubeclientError
// that includes the schema.GroupVersion used in a .Discover() call.
type KubeclientDiscoverError struct {
	// embed KubeclientError so we don't need a copy of ShouldRetry
	KubeclientError
	gv schema.GroupVersion
}

func (e KubeclientDiscoverError) Error() string {
	return fmt.Sprintf("failed to discover server resources for GroupVersion %q: %s",
		e.gv.String(), e.err.Error())
}

func NewKubeclientDiscoverError(gv schema.GroupVersion, err error) error {
	return KubeclientDiscoverError{
		KubeclientError: KubeclientError{
			verb: KubeclientVerbDiscover,
			err:  err,
		},
		gv: gv,
	}
}

func IsKubeclientError(err error) bool {
	switch err.(type) {
	case KubeclientError, KubeclientListError, KubeclientDiscoverError:
		return true
	}

	return false
}
