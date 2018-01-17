/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	v1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	scheme "github.com/bookingcom/shipper/pkg/client/clientset/versioned/scheme"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	Create(*v1.CapacityTarget) (*v1.CapacityTarget, error)
	Update(*v1.CapacityTarget) (*v1.CapacityTarget, error)
	UpdateStatus(*v1.CapacityTarget) (*v1.CapacityTarget, error)
	Delete(name string, options *meta_v1.DeleteOptions) error
	DeleteCollection(options *meta_v1.DeleteOptions, listOptions meta_v1.ListOptions) error
	Get(name string, options meta_v1.GetOptions) (*v1.CapacityTarget, error)
	List(opts meta_v1.ListOptions) (*v1.CapacityTargetList, error)
	Watch(opts meta_v1.ListOptions) (watch.Interface, error)
	Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1.CapacityTarget, err error)
	CapacityTargetExpansion
}

// capacityTargets implements CapacityTargetInterface
type capacityTargets struct {
	client rest.Interface
	ns     string
}

// newCapacityTargets returns a CapacityTargets
func newCapacityTargets(c *ShipperV1Client, namespace string) *capacityTargets {
	return &capacityTargets{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the capacityTarget, and returns the corresponding capacityTarget object, and an error if there is any.
func (c *capacityTargets) Get(name string, options meta_v1.GetOptions) (result *v1.CapacityTarget, err error) {
	result = &v1.CapacityTarget{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("capacitytargets").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of CapacityTargets that match those selectors.
func (c *capacityTargets) List(opts meta_v1.ListOptions) (result *v1.CapacityTargetList, err error) {
	result = &v1.CapacityTargetList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("capacitytargets").
		VersionedParams(&opts, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested capacityTargets.
func (c *capacityTargets) Watch(opts meta_v1.ListOptions) (watch.Interface, error) {
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("capacitytargets").
		VersionedParams(&opts, scheme.ParameterCodec).
		Watch()
}

// Create takes the representation of a capacityTarget and creates it.  Returns the server's representation of the capacityTarget, and an error, if there is any.
func (c *capacityTargets) Create(capacityTarget *v1.CapacityTarget) (result *v1.CapacityTarget, err error) {
	result = &v1.CapacityTarget{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("capacitytargets").
		Body(capacityTarget).
		Do().
		Into(result)
	return
}

// Update takes the representation of a capacityTarget and updates it. Returns the server's representation of the capacityTarget, and an error, if there is any.
func (c *capacityTargets) Update(capacityTarget *v1.CapacityTarget) (result *v1.CapacityTarget, err error) {
	result = &v1.CapacityTarget{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("capacitytargets").
		Name(capacityTarget.Name).
		Body(capacityTarget).
		Do().
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().

func (c *capacityTargets) UpdateStatus(capacityTarget *v1.CapacityTarget) (result *v1.CapacityTarget, err error) {
	result = &v1.CapacityTarget{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("capacitytargets").
		Name(capacityTarget.Name).
		SubResource("status").
		Body(capacityTarget).
		Do().
		Into(result)
	return
}

// Delete takes name of the capacityTarget and deletes it. Returns an error if one occurs.
func (c *capacityTargets) Delete(name string, options *meta_v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("capacitytargets").
		Name(name).
		Body(options).
		Do().
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *capacityTargets) DeleteCollection(options *meta_v1.DeleteOptions, listOptions meta_v1.ListOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("capacitytargets").
		VersionedParams(&listOptions, scheme.ParameterCodec).
		Body(options).
		Do().
		Error()
}

// Patch applies the patch and returns the patched capacityTarget.
func (c *capacityTargets) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1.CapacityTarget, err error) {
	result = &v1.CapacityTarget{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("capacitytargets").
		SubResource(subresources...).
		Name(name).
		Body(data).
		Do().
		Into(result)
	return
}
