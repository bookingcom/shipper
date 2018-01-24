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

// FakeInstallationTargets implements InstallationTargetInterface
type FakeInstallationTargets struct {
	Fake *FakeShipperV1
	ns   string
}

var installationtargetsResource = schema.GroupVersionResource{Group: "shipper", Version: "v1", Resource: "installationtargets"}

var installationtargetsKind = schema.GroupVersionKind{Group: "shipper", Version: "v1", Kind: "InstallationTarget"}

// Get takes name of the installationTarget, and returns the corresponding installationTarget object, and an error if there is any.
func (c *FakeInstallationTargets) Get(name string, options v1.GetOptions) (result *shipper_v1.InstallationTarget, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(installationtargetsResource, c.ns, name), &shipper_v1.InstallationTarget{})

	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.InstallationTarget), err
}

// List takes label and field selectors, and returns the list of InstallationTargets that match those selectors.
func (c *FakeInstallationTargets) List(opts v1.ListOptions) (result *shipper_v1.InstallationTargetList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(installationtargetsResource, installationtargetsKind, c.ns, opts), &shipper_v1.InstallationTargetList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &shipper_v1.InstallationTargetList{}
	for _, item := range obj.(*shipper_v1.InstallationTargetList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested installationTargets.
func (c *FakeInstallationTargets) Watch(opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(installationtargetsResource, c.ns, opts))

}

// Create takes the representation of a installationTarget and creates it.  Returns the server's representation of the installationTarget, and an error, if there is any.
func (c *FakeInstallationTargets) Create(installationTarget *shipper_v1.InstallationTarget) (result *shipper_v1.InstallationTarget, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(installationtargetsResource, c.ns, installationTarget), &shipper_v1.InstallationTarget{})

	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.InstallationTarget), err
}

// Update takes the representation of a installationTarget and updates it. Returns the server's representation of the installationTarget, and an error, if there is any.
func (c *FakeInstallationTargets) Update(installationTarget *shipper_v1.InstallationTarget) (result *shipper_v1.InstallationTarget, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(installationtargetsResource, c.ns, installationTarget), &shipper_v1.InstallationTarget{})

	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.InstallationTarget), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeInstallationTargets) UpdateStatus(installationTarget *shipper_v1.InstallationTarget) (*shipper_v1.InstallationTarget, error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateSubresourceAction(installationtargetsResource, "status", c.ns, installationTarget), &shipper_v1.InstallationTarget{})

	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.InstallationTarget), err
}

// Delete takes name of the installationTarget and deletes it. Returns an error if one occurs.
func (c *FakeInstallationTargets) Delete(name string, options *v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(installationtargetsResource, c.ns, name), &shipper_v1.InstallationTarget{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeInstallationTargets) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(installationtargetsResource, c.ns, listOptions)

	_, err := c.Fake.Invokes(action, &shipper_v1.InstallationTargetList{})
	return err
}

// Patch applies the patch and returns the patched installationTarget.
func (c *FakeInstallationTargets) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *shipper_v1.InstallationTarget, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(installationtargetsResource, c.ns, name, data, subresources...), &shipper_v1.InstallationTarget{})

	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.InstallationTarget), err
}
