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

// FakeCapacityTargets implements CapacityTargetInterface
type FakeCapacityTargets struct {
	Fake *FakeShipperV1
	ns   string
}

var capacitytargetsResource = schema.GroupVersionResource{Group: "shipper", Version: "v1", Resource: "capacitytargets"}

var capacitytargetsKind = schema.GroupVersionKind{Group: "shipper", Version: "v1", Kind: "CapacityTarget"}

// Get takes name of the capacityTarget, and returns the corresponding capacityTarget object, and an error if there is any.
func (c *FakeCapacityTargets) Get(name string, options v1.GetOptions) (result *shipper_v1.CapacityTarget, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(capacitytargetsResource, c.ns, name), &shipper_v1.CapacityTarget{})

	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.CapacityTarget), err
}

// List takes label and field selectors, and returns the list of CapacityTargets that match those selectors.
func (c *FakeCapacityTargets) List(opts v1.ListOptions) (result *shipper_v1.CapacityTargetList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(capacitytargetsResource, capacitytargetsKind, c.ns, opts), &shipper_v1.CapacityTargetList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &shipper_v1.CapacityTargetList{}
	for _, item := range obj.(*shipper_v1.CapacityTargetList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested capacityTargets.
func (c *FakeCapacityTargets) Watch(opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(capacitytargetsResource, c.ns, opts))

}

// Create takes the representation of a capacityTarget and creates it.  Returns the server's representation of the capacityTarget, and an error, if there is any.
func (c *FakeCapacityTargets) Create(capacityTarget *shipper_v1.CapacityTarget) (result *shipper_v1.CapacityTarget, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(capacitytargetsResource, c.ns, capacityTarget), &shipper_v1.CapacityTarget{})

	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.CapacityTarget), err
}

// Update takes the representation of a capacityTarget and updates it. Returns the server's representation of the capacityTarget, and an error, if there is any.
func (c *FakeCapacityTargets) Update(capacityTarget *shipper_v1.CapacityTarget) (result *shipper_v1.CapacityTarget, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(capacitytargetsResource, c.ns, capacityTarget), &shipper_v1.CapacityTarget{})

	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.CapacityTarget), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeCapacityTargets) UpdateStatus(capacityTarget *shipper_v1.CapacityTarget) (*shipper_v1.CapacityTarget, error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateSubresourceAction(capacitytargetsResource, "status", c.ns, capacityTarget), &shipper_v1.CapacityTarget{})

	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.CapacityTarget), err
}

// Delete takes name of the capacityTarget and deletes it. Returns an error if one occurs.
func (c *FakeCapacityTargets) Delete(name string, options *v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(capacitytargetsResource, c.ns, name), &shipper_v1.CapacityTarget{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeCapacityTargets) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(capacitytargetsResource, c.ns, listOptions)

	_, err := c.Fake.Invokes(action, &shipper_v1.CapacityTargetList{})
	return err
}

// Patch applies the patch and returns the patched capacityTarget.
func (c *FakeCapacityTargets) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *shipper_v1.CapacityTarget, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(capacitytargetsResource, c.ns, name, data, subresources...), &shipper_v1.CapacityTarget{})

	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.CapacityTarget), err
}
