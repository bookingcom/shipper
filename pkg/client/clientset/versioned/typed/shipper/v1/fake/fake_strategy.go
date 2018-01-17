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

// FakeStrategies implements StrategyInterface
type FakeStrategies struct {
	Fake *FakeShipperV1
	ns   string
}

var strategiesResource = schema.GroupVersionResource{Group: "shipper", Version: "v1", Resource: "strategies"}

var strategiesKind = schema.GroupVersionKind{Group: "shipper", Version: "v1", Kind: "Strategy"}

// Get takes name of the strategy, and returns the corresponding strategy object, and an error if there is any.
func (c *FakeStrategies) Get(name string, options v1.GetOptions) (result *shipper_v1.Strategy, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(strategiesResource, c.ns, name), &shipper_v1.Strategy{})

	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.Strategy), err
}

// List takes label and field selectors, and returns the list of Strategies that match those selectors.
func (c *FakeStrategies) List(opts v1.ListOptions) (result *shipper_v1.StrategyList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(strategiesResource, strategiesKind, c.ns, opts), &shipper_v1.StrategyList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &shipper_v1.StrategyList{}
	for _, item := range obj.(*shipper_v1.StrategyList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested strategies.
func (c *FakeStrategies) Watch(opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(strategiesResource, c.ns, opts))

}

// Create takes the representation of a strategy and creates it.  Returns the server's representation of the strategy, and an error, if there is any.
func (c *FakeStrategies) Create(strategy *shipper_v1.Strategy) (result *shipper_v1.Strategy, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(strategiesResource, c.ns, strategy), &shipper_v1.Strategy{})

	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.Strategy), err
}

// Update takes the representation of a strategy and updates it. Returns the server's representation of the strategy, and an error, if there is any.
func (c *FakeStrategies) Update(strategy *shipper_v1.Strategy) (result *shipper_v1.Strategy, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(strategiesResource, c.ns, strategy), &shipper_v1.Strategy{})

	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.Strategy), err
}

// Delete takes name of the strategy and deletes it. Returns an error if one occurs.
func (c *FakeStrategies) Delete(name string, options *v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(strategiesResource, c.ns, name), &shipper_v1.Strategy{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeStrategies) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(strategiesResource, c.ns, listOptions)

	_, err := c.Fake.Invokes(action, &shipper_v1.StrategyList{})
	return err
}

// Patch applies the patch and returns the patched strategy.
func (c *FakeStrategies) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *shipper_v1.Strategy, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(strategiesResource, c.ns, name, data, subresources...), &shipper_v1.Strategy{})

	if obj == nil {
		return nil, err
	}
	return obj.(*shipper_v1.Strategy), err
}
