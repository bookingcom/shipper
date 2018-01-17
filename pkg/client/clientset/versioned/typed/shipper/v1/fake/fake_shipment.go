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

// FakeShipments implements ShipmentInterface
type FakeShipments struct {
	Fake *FakeShipperV1
	ns   string
}

var shipmentsResource = schema.GroupVersionResource{Group: "shipper", Version: "v1", Resource: "shipments"}

var shipmentsKind = schema.GroupVersionKind{Group: "shipper", Version: "v1", Kind: "Shipment"}

// Get takes name of the shipment, and returns the corresponding shipment object, and an error if there is any.
func (c *FakeShipments) Get(name string, options v1.GetOptions) (result *shipper_v1.Shipment, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(shipmentsResource, c.ns, name), &shipper_v1.Shipment{})

	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.Shipment), err
}

// List takes label and field selectors, and returns the list of Shipments that match those selectors.
func (c *FakeShipments) List(opts v1.ListOptions) (result *shipper_v1.ShipmentList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(shipmentsResource, shipmentsKind, c.ns, opts), &shipper_v1.ShipmentList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &shipper_v1.ShipmentList{}
	for _, item := range obj.(*shipper_v1.ShipmentList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested shipments.
func (c *FakeShipments) Watch(opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(shipmentsResource, c.ns, opts))

}

// Create takes the representation of a shipment and creates it.  Returns the server's representation of the shipment, and an error, if there is any.
func (c *FakeShipments) Create(shipment *shipper_v1.Shipment) (result *shipper_v1.Shipment, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(shipmentsResource, c.ns, shipment), &shipper_v1.Shipment{})

	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.Shipment), err
}

// Update takes the representation of a shipment and updates it. Returns the server's representation of the shipment, and an error, if there is any.
func (c *FakeShipments) Update(shipment *shipper_v1.Shipment) (result *shipper_v1.Shipment, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(shipmentsResource, c.ns, shipment), &shipper_v1.Shipment{})

	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.Shipment), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeShipments) UpdateStatus(shipment *shipper_v1.Shipment) (*shipper_v1.Shipment, error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateSubresourceAction(shipmentsResource, "status", c.ns, shipment), &shipper_v1.Shipment{})

	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.Shipment), err
}

// Delete takes name of the shipment and deletes it. Returns an error if one occurs.
func (c *FakeShipments) Delete(name string, options *v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(shipmentsResource, c.ns, name), &shipper_v1.Shipment{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeShipments) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(shipmentsResource, c.ns, listOptions)

	_, err := c.Fake.Invokes(action, &shipper_v1.ShipmentList{})
	return err
}

// Patch applies the patch and returns the patched shipment.
func (c *FakeShipments) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *shipper_v1.Shipment, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(shipmentsResource, c.ns, name, data, subresources...), &shipper_v1.Shipment{})

	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.Shipment), err
}
