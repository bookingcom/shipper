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

// ShipmentsGetter has a method to return a ShipmentInterface.
// A group's client should implement this interface.
type ShipmentsGetter interface {
	Shipments(namespace string) ShipmentInterface
}

// ShipmentInterface has methods to work with Shipment resources.
type ShipmentInterface interface {
	Create(*v1.Shipment) (*v1.Shipment, error)
	Update(*v1.Shipment) (*v1.Shipment, error)
	UpdateStatus(*v1.Shipment) (*v1.Shipment, error)
	Delete(name string, options *meta_v1.DeleteOptions) error
	DeleteCollection(options *meta_v1.DeleteOptions, listOptions meta_v1.ListOptions) error
	Get(name string, options meta_v1.GetOptions) (*v1.Shipment, error)
	List(opts meta_v1.ListOptions) (*v1.ShipmentList, error)
	Watch(opts meta_v1.ListOptions) (watch.Interface, error)
	Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1.Shipment, err error)
	ShipmentExpansion
}

// shipments implements ShipmentInterface
type shipments struct {
	client rest.Interface
	ns     string
}

// newShipments returns a Shipments
func newShipments(c *ShipperV1Client, namespace string) *shipments {
	return &shipments{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the shipment, and returns the corresponding shipment object, and an error if there is any.
func (c *shipments) Get(name string, options meta_v1.GetOptions) (result *v1.Shipment, err error) {
	result = &v1.Shipment{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("shipments").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of Shipments that match those selectors.
func (c *shipments) List(opts meta_v1.ListOptions) (result *v1.ShipmentList, err error) {
	result = &v1.ShipmentList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("shipments").
		VersionedParams(&opts, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested shipments.
func (c *shipments) Watch(opts meta_v1.ListOptions) (watch.Interface, error) {
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("shipments").
		VersionedParams(&opts, scheme.ParameterCodec).
		Watch()
}

// Create takes the representation of a shipment and creates it.  Returns the server's representation of the shipment, and an error, if there is any.
func (c *shipments) Create(shipment *v1.Shipment) (result *v1.Shipment, err error) {
	result = &v1.Shipment{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("shipments").
		Body(shipment).
		Do().
		Into(result)
	return
}

// Update takes the representation of a shipment and updates it. Returns the server's representation of the shipment, and an error, if there is any.
func (c *shipments) Update(shipment *v1.Shipment) (result *v1.Shipment, err error) {
	result = &v1.Shipment{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("shipments").
		Name(shipment.Name).
		Body(shipment).
		Do().
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().

func (c *shipments) UpdateStatus(shipment *v1.Shipment) (result *v1.Shipment, err error) {
	result = &v1.Shipment{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("shipments").
		Name(shipment.Name).
		SubResource("status").
		Body(shipment).
		Do().
		Into(result)
	return
}

// Delete takes name of the shipment and deletes it. Returns an error if one occurs.
func (c *shipments) Delete(name string, options *meta_v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("shipments").
		Name(name).
		Body(options).
		Do().
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *shipments) DeleteCollection(options *meta_v1.DeleteOptions, listOptions meta_v1.ListOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("shipments").
		VersionedParams(&listOptions, scheme.ParameterCodec).
		Body(options).
		Do().
		Error()
}

// Patch applies the patch and returns the patched shipment.
func (c *shipments) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1.Shipment, err error) {
	result = &v1.Shipment{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("shipments").
		SubResource(subresources...).
		Name(name).
		Body(data).
		Do().
		Into(result)
	return
}
