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

package fake

import (
	shipper_v1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeShipmentOrders implements ShipmentOrderInterface
type FakeShipmentOrders struct {
	Fake *FakeShipperV1
	ns   string
}

var shipmentordersResource = schema.GroupVersionResource{Group: "shipper.booking.com", Version: "v1", Resource: "shipmentorders"}

var shipmentordersKind = schema.GroupVersionKind{Group: "shipper.booking.com", Version: "v1", Kind: "ShipmentOrder"}

// Get takes name of the shipmentOrder, and returns the corresponding shipmentOrder object, and an error if there is any.
func (c *FakeShipmentOrders) Get(name string, options v1.GetOptions) (result *shipper_v1.ShipmentOrder, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(shipmentordersResource, c.ns, name), &shipper_v1.ShipmentOrder{})

	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.ShipmentOrder), err
}

// List takes label and field selectors, and returns the list of ShipmentOrders that match those selectors.
func (c *FakeShipmentOrders) List(opts v1.ListOptions) (result *shipper_v1.ShipmentOrderList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(shipmentordersResource, shipmentordersKind, c.ns, opts), &shipper_v1.ShipmentOrderList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &shipper_v1.ShipmentOrderList{}
	for _, item := range obj.(*shipper_v1.ShipmentOrderList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested shipmentOrders.
func (c *FakeShipmentOrders) Watch(opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(shipmentordersResource, c.ns, opts))

}

// Create takes the representation of a shipmentOrder and creates it.  Returns the server's representation of the shipmentOrder, and an error, if there is any.
func (c *FakeShipmentOrders) Create(shipmentOrder *shipper_v1.ShipmentOrder) (result *shipper_v1.ShipmentOrder, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(shipmentordersResource, c.ns, shipmentOrder), &shipper_v1.ShipmentOrder{})

	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.ShipmentOrder), err
}

// Update takes the representation of a shipmentOrder and updates it. Returns the server's representation of the shipmentOrder, and an error, if there is any.
func (c *FakeShipmentOrders) Update(shipmentOrder *shipper_v1.ShipmentOrder) (result *shipper_v1.ShipmentOrder, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(shipmentordersResource, c.ns, shipmentOrder), &shipper_v1.ShipmentOrder{})

	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.ShipmentOrder), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeShipmentOrders) UpdateStatus(shipmentOrder *shipper_v1.ShipmentOrder) (*shipper_v1.ShipmentOrder, error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateSubresourceAction(shipmentordersResource, "status", c.ns, shipmentOrder), &shipper_v1.ShipmentOrder{})

	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.ShipmentOrder), err
}

// Delete takes name of the shipmentOrder and deletes it. Returns an error if one occurs.
func (c *FakeShipmentOrders) Delete(name string, options *v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(shipmentordersResource, c.ns, name), &shipper_v1.ShipmentOrder{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeShipmentOrders) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(shipmentordersResource, c.ns, listOptions)

	_, err := c.Fake.Invokes(action, &shipper_v1.ShipmentOrderList{})
	return err
}

// Patch applies the patch and returns the patched shipmentOrder.
func (c *FakeShipmentOrders) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *shipper_v1.ShipmentOrder, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(shipmentordersResource, c.ns, name, data, subresources...), &shipper_v1.ShipmentOrder{})

	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.ShipmentOrder), err
}
