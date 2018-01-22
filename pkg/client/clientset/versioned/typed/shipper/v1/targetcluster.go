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

// TargetClustersGetter has a method to return a TargetClusterInterface.
// A group's client should implement this interface.
type TargetClustersGetter interface {
	TargetClusters() TargetClusterInterface
}

// TargetClusterInterface has methods to work with TargetCluster resources.
type TargetClusterInterface interface {
	Create(*v1.TargetCluster) (*v1.TargetCluster, error)
	Update(*v1.TargetCluster) (*v1.TargetCluster, error)
	UpdateStatus(*v1.TargetCluster) (*v1.TargetCluster, error)
	Delete(name string, options *meta_v1.DeleteOptions) error
	DeleteCollection(options *meta_v1.DeleteOptions, listOptions meta_v1.ListOptions) error
	Get(name string, options meta_v1.GetOptions) (*v1.TargetCluster, error)
	List(opts meta_v1.ListOptions) (*v1.TargetClusterList, error)
	Watch(opts meta_v1.ListOptions) (watch.Interface, error)
	Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1.TargetCluster, err error)
	TargetClusterExpansion
}

// targetClusters implements TargetClusterInterface
type targetClusters struct {
	client rest.Interface
}

// newTargetClusters returns a TargetClusters
func newTargetClusters(c *ShipperV1Client) *targetClusters {
	return &targetClusters{
		client: c.RESTClient(),
	}
}

// Get takes name of the targetCluster, and returns the corresponding targetCluster object, and an error if there is any.
func (c *targetClusters) Get(name string, options meta_v1.GetOptions) (result *v1.TargetCluster, err error) {
	result = &v1.TargetCluster{}
	err = c.client.Get().
		Resource("targetclusters").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of TargetClusters that match those selectors.
func (c *targetClusters) List(opts meta_v1.ListOptions) (result *v1.TargetClusterList, err error) {
	result = &v1.TargetClusterList{}
	err = c.client.Get().
		Resource("targetclusters").
		VersionedParams(&opts, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested targetClusters.
func (c *targetClusters) Watch(opts meta_v1.ListOptions) (watch.Interface, error) {
	opts.Watch = true
	return c.client.Get().
		Resource("targetclusters").
		VersionedParams(&opts, scheme.ParameterCodec).
		Watch()
}

// Create takes the representation of a targetCluster and creates it.  Returns the server's representation of the targetCluster, and an error, if there is any.
func (c *targetClusters) Create(targetCluster *v1.TargetCluster) (result *v1.TargetCluster, err error) {
	result = &v1.TargetCluster{}
	err = c.client.Post().
		Resource("targetclusters").
		Body(targetCluster).
		Do().
		Into(result)
	return
}

// Update takes the representation of a targetCluster and updates it. Returns the server's representation of the targetCluster, and an error, if there is any.
func (c *targetClusters) Update(targetCluster *v1.TargetCluster) (result *v1.TargetCluster, err error) {
	result = &v1.TargetCluster{}
	err = c.client.Put().
		Resource("targetclusters").
		Name(targetCluster.Name).
		Body(targetCluster).
		Do().
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().

func (c *targetClusters) UpdateStatus(targetCluster *v1.TargetCluster) (result *v1.TargetCluster, err error) {
	result = &v1.TargetCluster{}
	err = c.client.Put().
		Resource("targetclusters").
		Name(targetCluster.Name).
		SubResource("status").
		Body(targetCluster).
		Do().
		Into(result)
	return
}

// Delete takes name of the targetCluster and deletes it. Returns an error if one occurs.
func (c *targetClusters) Delete(name string, options *meta_v1.DeleteOptions) error {
	return c.client.Delete().
		Resource("targetclusters").
		Name(name).
		Body(options).
		Do().
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *targetClusters) DeleteCollection(options *meta_v1.DeleteOptions, listOptions meta_v1.ListOptions) error {
	return c.client.Delete().
		Resource("targetclusters").
		VersionedParams(&listOptions, scheme.ParameterCodec).
		Body(options).
		Do().
		Error()
}

// Patch applies the patch and returns the patched targetCluster.
func (c *targetClusters) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1.TargetCluster, err error) {
	result = &v1.TargetCluster{}
	err = c.client.Patch(pt).
		Resource("targetclusters").
		SubResource(subresources...).
		Name(name).
		Body(data).
		Do().
		Into(result)
	return
}
