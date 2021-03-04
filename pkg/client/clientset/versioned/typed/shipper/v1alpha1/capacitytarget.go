// Code generated by client-gen. DO NOT EDIT.

package v1alpha1

import (
	"context"
	"time"

	v1alpha1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	scheme "github.com/bookingcom/shipper/pkg/client/clientset/versioned/scheme"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// CapacityTargetsGetter has a method to return a CapacityTargetInterface.
// A group's client should implement this interface.
type CapacityTargetsGetter interface {
	CapacityTargets(namespace string) CapacityTargetInterface
}

// CapacityTargetInterface has methods to work with CapacityTarget resources.
type CapacityTargetInterface interface {
	Create(ctx context.Context, capacityTarget *v1alpha1.CapacityTarget, opts v1.CreateOptions) (*v1alpha1.CapacityTarget, error)
	Update(ctx context.Context, capacityTarget *v1alpha1.CapacityTarget, opts v1.UpdateOptions) (*v1alpha1.CapacityTarget, error)
	UpdateStatus(ctx context.Context, capacityTarget *v1alpha1.CapacityTarget, opts v1.UpdateOptions) (*v1alpha1.CapacityTarget, error)
	Delete(ctx context.Context, name string, opts v1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error
	Get(ctx context.Context, name string, opts v1.GetOptions) (*v1alpha1.CapacityTarget, error)
	List(ctx context.Context, opts v1.ListOptions) (*v1alpha1.CapacityTargetList, error)
	Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.CapacityTarget, err error)
	CapacityTargetExpansion
}

// capacityTargets implements CapacityTargetInterface
type capacityTargets struct {
	client rest.Interface
	ns     string
}

// newCapacityTargets returns a CapacityTargets
func newCapacityTargets(c *ShipperV1alpha1Client, namespace string) *capacityTargets {
	return &capacityTargets{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the capacityTarget, and returns the corresponding capacityTarget object, and an error if there is any.
func (c *capacityTargets) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1alpha1.CapacityTarget, err error) {
	result = &v1alpha1.CapacityTarget{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("capacitytargets").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do(ctx).
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of CapacityTargets that match those selectors.
func (c *capacityTargets) List(ctx context.Context, opts v1.ListOptions) (result *v1alpha1.CapacityTargetList, err error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	result = &v1alpha1.CapacityTargetList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("capacitytargets").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Do(ctx).
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested capacityTargets.
func (c *capacityTargets) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("capacitytargets").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Watch(ctx)
}

// Create takes the representation of a capacityTarget and creates it.  Returns the server's representation of the capacityTarget, and an error, if there is any.
func (c *capacityTargets) Create(ctx context.Context, capacityTarget *v1alpha1.CapacityTarget, opts v1.CreateOptions) (result *v1alpha1.CapacityTarget, err error) {
	result = &v1alpha1.CapacityTarget{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("capacitytargets").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(capacityTarget).
		Do(ctx).
		Into(result)
	return
}

// Update takes the representation of a capacityTarget and updates it. Returns the server's representation of the capacityTarget, and an error, if there is any.
func (c *capacityTargets) Update(ctx context.Context, capacityTarget *v1alpha1.CapacityTarget, opts v1.UpdateOptions) (result *v1alpha1.CapacityTarget, err error) {
	result = &v1alpha1.CapacityTarget{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("capacitytargets").
		Name(capacityTarget.Name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(capacityTarget).
		Do(ctx).
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *capacityTargets) UpdateStatus(ctx context.Context, capacityTarget *v1alpha1.CapacityTarget, opts v1.UpdateOptions) (result *v1alpha1.CapacityTarget, err error) {
	result = &v1alpha1.CapacityTarget{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("capacitytargets").
		Name(capacityTarget.Name).
		SubResource("status").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(capacityTarget).
		Do(ctx).
		Into(result)
	return
}

// Delete takes name of the capacityTarget and deletes it. Returns an error if one occurs.
func (c *capacityTargets) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("capacitytargets").
		Name(name).
		Body(&opts).
		Do(ctx).
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *capacityTargets) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	var timeout time.Duration
	if listOpts.TimeoutSeconds != nil {
		timeout = time.Duration(*listOpts.TimeoutSeconds) * time.Second
	}
	return c.client.Delete().
		Namespace(c.ns).
		Resource("capacitytargets").
		VersionedParams(&listOpts, scheme.ParameterCodec).
		Timeout(timeout).
		Body(&opts).
		Do(ctx).
		Error()
}

// Patch applies the patch and returns the patched capacityTarget.
func (c *capacityTargets) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.CapacityTarget, err error) {
	result = &v1alpha1.CapacityTarget{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("capacitytargets").
		Name(name).
		SubResource(subresources...).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(data).
		Do(ctx).
		Into(result)
	return
}
