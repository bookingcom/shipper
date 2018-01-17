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

// FakeApplicationClusters implements ApplicationClusterInterface
type FakeApplicationClusters struct {
	Fake *FakeShipperV1
}

var applicationclustersResource = schema.GroupVersionResource{Group: "shipper", Version: "v1", Resource: "applicationclusters"}

var applicationclustersKind = schema.GroupVersionKind{Group: "shipper", Version: "v1", Kind: "ApplicationCluster"}

// Get takes name of the applicationCluster, and returns the corresponding applicationCluster object, and an error if there is any.
func (c *FakeApplicationClusters) Get(name string, options v1.GetOptions) (result *shipper_v1.ApplicationCluster, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootGetAction(applicationclustersResource, name), &shipper_v1.ApplicationCluster{})
	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.ApplicationCluster), err
}

// List takes label and field selectors, and returns the list of ApplicationClusters that match those selectors.
func (c *FakeApplicationClusters) List(opts v1.ListOptions) (result *shipper_v1.ApplicationClusterList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootListAction(applicationclustersResource, applicationclustersKind, opts), &shipper_v1.ApplicationClusterList{})
	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &shipper_v1.ApplicationClusterList{}
	for _, item := range obj.(*shipper_v1.ApplicationClusterList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested applicationClusters.
func (c *FakeApplicationClusters) Watch(opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewRootWatchAction(applicationclustersResource, opts))
}

// Create takes the representation of a applicationCluster and creates it.  Returns the server's representation of the applicationCluster, and an error, if there is any.
func (c *FakeApplicationClusters) Create(applicationCluster *shipper_v1.ApplicationCluster) (result *shipper_v1.ApplicationCluster, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootCreateAction(applicationclustersResource, applicationCluster), &shipper_v1.ApplicationCluster{})
	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.ApplicationCluster), err
}

// Update takes the representation of a applicationCluster and updates it. Returns the server's representation of the applicationCluster, and an error, if there is any.
func (c *FakeApplicationClusters) Update(applicationCluster *shipper_v1.ApplicationCluster) (result *shipper_v1.ApplicationCluster, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootUpdateAction(applicationclustersResource, applicationCluster), &shipper_v1.ApplicationCluster{})
	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.ApplicationCluster), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeApplicationClusters) UpdateStatus(applicationCluster *shipper_v1.ApplicationCluster) (*shipper_v1.ApplicationCluster, error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootUpdateSubresourceAction(applicationclustersResource, "status", applicationCluster), &shipper_v1.ApplicationCluster{})
	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.ApplicationCluster), err
}

// Delete takes name of the applicationCluster and deletes it. Returns an error if one occurs.
func (c *FakeApplicationClusters) Delete(name string, options *v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewRootDeleteAction(applicationclustersResource, name), &shipper_v1.ApplicationCluster{})
	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeApplicationClusters) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	action := testing.NewRootDeleteCollectionAction(applicationclustersResource, listOptions)

	_, err := c.Fake.Invokes(action, &shipper_v1.ApplicationClusterList{})
	return err
}

// Patch applies the patch and returns the patched applicationCluster.
func (c *FakeApplicationClusters) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *shipper_v1.ApplicationCluster, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootPatchSubresourceAction(applicationclustersResource, name, data, subresources...), &shipper_v1.ApplicationCluster{})
	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.ApplicationCluster), err
}
