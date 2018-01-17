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

// ShipmentOrdersGetter has a method to return a ShipmentOrderInterface.
// A group's client should implement this interface.
type ShipmentOrdersGetter interface {
	ShipmentOrders(namespace string) ShipmentOrderInterface
}

// ShipmentOrderInterface has methods to work with ShipmentOrder resources.
type ShipmentOrderInterface interface {
	Create(*v1.ShipmentOrder) (*v1.ShipmentOrder, error)
	Update(*v1.ShipmentOrder) (*v1.ShipmentOrder, error)
	UpdateStatus(*v1.ShipmentOrder) (*v1.ShipmentOrder, error)
	Delete(name string, options *meta_v1.DeleteOptions) error
	DeleteCollection(options *meta_v1.DeleteOptions, listOptions meta_v1.ListOptions) error
	Get(name string, options meta_v1.GetOptions) (*v1.ShipmentOrder, error)
	List(opts meta_v1.ListOptions) (*v1.ShipmentOrderList, error)
	Watch(opts meta_v1.ListOptions) (watch.Interface, error)
	Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1.ShipmentOrder, err error)
	ShipmentOrderExpansion
}

// shipmentOrders implements ShipmentOrderInterface
type shipmentOrders struct {
	client rest.Interface
	ns     string
}

// newShipmentOrders returns a ShipmentOrders
func newShipmentOrders(c *ShipperV1Client, namespace string) *shipmentOrders {
	return &shipmentOrders{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the shipmentOrder, and returns the corresponding shipmentOrder object, and an error if there is any.
func (c *shipmentOrders) Get(name string, options meta_v1.GetOptions) (result *v1.ShipmentOrder, err error) {
	result = &v1.ShipmentOrder{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("shipmentorders").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of ShipmentOrders that match those selectors.
func (c *shipmentOrders) List(opts meta_v1.ListOptions) (result *v1.ShipmentOrderList, err error) {
	result = &v1.ShipmentOrderList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("shipmentorders").
		VersionedParams(&opts, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested shipmentOrders.
func (c *shipmentOrders) Watch(opts meta_v1.ListOptions) (watch.Interface, error) {
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("shipmentorders").
		VersionedParams(&opts, scheme.ParameterCodec).
		Watch()
}

// Create takes the representation of a shipmentOrder and creates it.  Returns the server's representation of the shipmentOrder, and an error, if there is any.
func (c *shipmentOrders) Create(shipmentOrder *v1.ShipmentOrder) (result *v1.ShipmentOrder, err error) {
	result = &v1.ShipmentOrder{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("shipmentorders").
		Body(shipmentOrder).
		Do().
		Into(result)
	return
}

// Update takes the representation of a shipmentOrder and updates it. Returns the server's representation of the shipmentOrder, and an error, if there is any.
func (c *shipmentOrders) Update(shipmentOrder *v1.ShipmentOrder) (result *v1.ShipmentOrder, err error) {
	result = &v1.ShipmentOrder{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("shipmentorders").
		Name(shipmentOrder.Name).
		Body(shipmentOrder).
		Do().
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().

func (c *shipmentOrders) UpdateStatus(shipmentOrder *v1.ShipmentOrder) (result *v1.ShipmentOrder, err error) {
	result = &v1.ShipmentOrder{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("shipmentorders").
		Name(shipmentOrder.Name).
		SubResource("status").
		Body(shipmentOrder).
		Do().
		Into(result)
	return
}

// Delete takes name of the shipmentOrder and deletes it. Returns an error if one occurs.
func (c *shipmentOrders) Delete(name string, options *meta_v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("shipmentorders").
		Name(name).
		Body(options).
		Do().
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *shipmentOrders) DeleteCollection(options *meta_v1.DeleteOptions, listOptions meta_v1.ListOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("shipmentorders").
		VersionedParams(&listOptions, scheme.ParameterCodec).
		Body(options).
		Do().
		Error()
}

// Patch applies the patch and returns the patched shipmentOrder.
func (c *shipmentOrders) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1.ShipmentOrder, err error) {
	result = &v1.ShipmentOrder{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("shipmentorders").
		SubResource(subresources...).
		Name(name).
		Body(data).
		Do().
		Into(result)
	return
}
