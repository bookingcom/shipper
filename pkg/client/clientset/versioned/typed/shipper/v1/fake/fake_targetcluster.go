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

// FakeTargetClusters implements TargetClusterInterface
type FakeTargetClusters struct {
	Fake *FakeShipperV1
}

var targetclustersResource = schema.GroupVersionResource{Group: "shipper", Version: "v1", Resource: "targetclusters"}

var targetclustersKind = schema.GroupVersionKind{Group: "shipper", Version: "v1", Kind: "TargetCluster"}

// Get takes name of the targetCluster, and returns the corresponding targetCluster object, and an error if there is any.
func (c *FakeTargetClusters) Get(name string, options v1.GetOptions) (result *shipper_v1.TargetCluster, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootGetAction(targetclustersResource, name), &shipper_v1.TargetCluster{})
	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.TargetCluster), err
}

// List takes label and field selectors, and returns the list of TargetClusters that match those selectors.
func (c *FakeTargetClusters) List(opts v1.ListOptions) (result *shipper_v1.TargetClusterList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootListAction(targetclustersResource, targetclustersKind, opts), &shipper_v1.TargetClusterList{})
	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &shipper_v1.TargetClusterList{}
	for _, item := range obj.(*shipper_v1.TargetClusterList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested targetClusters.
func (c *FakeTargetClusters) Watch(opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewRootWatchAction(targetclustersResource, opts))
}

// Create takes the representation of a targetCluster and creates it.  Returns the server's representation of the targetCluster, and an error, if there is any.
func (c *FakeTargetClusters) Create(targetCluster *shipper_v1.TargetCluster) (result *shipper_v1.TargetCluster, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootCreateAction(targetclustersResource, targetCluster), &shipper_v1.TargetCluster{})
	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.TargetCluster), err
}

// Update takes the representation of a targetCluster and updates it. Returns the server's representation of the targetCluster, and an error, if there is any.
func (c *FakeTargetClusters) Update(targetCluster *shipper_v1.TargetCluster) (result *shipper_v1.TargetCluster, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootUpdateAction(targetclustersResource, targetCluster), &shipper_v1.TargetCluster{})
	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.TargetCluster), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeTargetClusters) UpdateStatus(targetCluster *shipper_v1.TargetCluster) (*shipper_v1.TargetCluster, error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootUpdateSubresourceAction(targetclustersResource, "status", targetCluster), &shipper_v1.TargetCluster{})
	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.TargetCluster), err
}

// Delete takes name of the targetCluster and deletes it. Returns an error if one occurs.
func (c *FakeTargetClusters) Delete(name string, options *v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewRootDeleteAction(targetclustersResource, name), &shipper_v1.TargetCluster{})
	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeTargetClusters) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	action := testing.NewRootDeleteCollectionAction(targetclustersResource, listOptions)

	_, err := c.Fake.Invokes(action, &shipper_v1.TargetClusterList{})
	return err
}

// Patch applies the patch and returns the patched targetCluster.
func (c *FakeTargetClusters) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *shipper_v1.TargetCluster, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootPatchSubresourceAction(targetclustersResource, name, data, subresources...), &shipper_v1.TargetCluster{})
	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.TargetCluster), err
}
