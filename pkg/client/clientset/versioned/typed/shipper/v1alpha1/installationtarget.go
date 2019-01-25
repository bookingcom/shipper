/*
Copyright 2019 The Kubernetes Authors.

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

package v1alpha1

import (
	v1alpha1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	scheme "github.com/bookingcom/shipper/pkg/client/clientset/versioned/scheme"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// InstallationTargetsGetter has a method to return a InstallationTargetInterface.
// A group's client should implement this interface.
type InstallationTargetsGetter interface {
	InstallationTargets(namespace string) InstallationTargetInterface
}

// InstallationTargetInterface has methods to work with InstallationTarget resources.
type InstallationTargetInterface interface {
	Create(*v1alpha1.InstallationTarget) (*v1alpha1.InstallationTarget, error)
	Update(*v1alpha1.InstallationTarget) (*v1alpha1.InstallationTarget, error)
	UpdateStatus(*v1alpha1.InstallationTarget) (*v1alpha1.InstallationTarget, error)
	Delete(name string, options *v1.DeleteOptions) error
	DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error
	Get(name string, options v1.GetOptions) (*v1alpha1.InstallationTarget, error)
	List(opts v1.ListOptions) (*v1alpha1.InstallationTargetList, error)
	Watch(opts v1.ListOptions) (watch.Interface, error)
	Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha1.InstallationTarget, err error)
	InstallationTargetExpansion
}

// installationTargets implements InstallationTargetInterface
type installationTargets struct {
	client rest.Interface
	ns     string
}

// newInstallationTargets returns a InstallationTargets
func newInstallationTargets(c *ShipperV1alpha1Client, namespace string) *installationTargets {
	return &installationTargets{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the installationTarget, and returns the corresponding installationTarget object, and an error if there is any.
func (c *installationTargets) Get(name string, options v1.GetOptions) (result *v1alpha1.InstallationTarget, err error) {
	result = &v1alpha1.InstallationTarget{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("installationtargets").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of InstallationTargets that match those selectors.
func (c *installationTargets) List(opts v1.ListOptions) (result *v1alpha1.InstallationTargetList, err error) {
	result = &v1alpha1.InstallationTargetList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("installationtargets").
		VersionedParams(&opts, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested installationTargets.
func (c *installationTargets) Watch(opts v1.ListOptions) (watch.Interface, error) {
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("installationtargets").
		VersionedParams(&opts, scheme.ParameterCodec).
		Watch()
}

// Create takes the representation of a installationTarget and creates it.  Returns the server's representation of the installationTarget, and an error, if there is any.
func (c *installationTargets) Create(installationTarget *v1alpha1.InstallationTarget) (result *v1alpha1.InstallationTarget, err error) {
	result = &v1alpha1.InstallationTarget{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("installationtargets").
		Body(installationTarget).
		Do().
		Into(result)
	return
}

// Update takes the representation of a installationTarget and updates it. Returns the server's representation of the installationTarget, and an error, if there is any.
func (c *installationTargets) Update(installationTarget *v1alpha1.InstallationTarget) (result *v1alpha1.InstallationTarget, err error) {
	result = &v1alpha1.InstallationTarget{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("installationtargets").
		Name(installationTarget.Name).
		Body(installationTarget).
		Do().
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().

func (c *installationTargets) UpdateStatus(installationTarget *v1alpha1.InstallationTarget) (result *v1alpha1.InstallationTarget, err error) {
	result = &v1alpha1.InstallationTarget{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("installationtargets").
		Name(installationTarget.Name).
		SubResource("status").
		Body(installationTarget).
		Do().
		Into(result)
	return
}

// Delete takes name of the installationTarget and deletes it. Returns an error if one occurs.
func (c *installationTargets) Delete(name string, options *v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("installationtargets").
		Name(name).
		Body(options).
		Do().
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *installationTargets) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("installationtargets").
		VersionedParams(&listOptions, scheme.ParameterCodec).
		Body(options).
		Do().
		Error()
}

// Patch applies the patch and returns the patched installationTarget.
func (c *installationTargets) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha1.InstallationTarget, err error) {
	result = &v1alpha1.InstallationTarget{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("installationtargets").
		SubResource(subresources...).
		Name(name).
		Body(data).
		Do().
		Into(result)
	return
}
