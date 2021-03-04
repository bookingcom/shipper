// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	"context"

	v1alpha1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeTrafficTargets implements TrafficTargetInterface
type FakeTrafficTargets struct {
	Fake *FakeShipperV1alpha1
	ns   string
}

var traffictargetsResource = schema.GroupVersionResource{Group: "shipper.booking.com", Version: "v1alpha1", Resource: "traffictargets"}

var traffictargetsKind = schema.GroupVersionKind{Group: "shipper.booking.com", Version: "v1alpha1", Kind: "TrafficTarget"}

// Get takes name of the trafficTarget, and returns the corresponding trafficTarget object, and an error if there is any.
func (c *FakeTrafficTargets) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1alpha1.TrafficTarget, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(traffictargetsResource, c.ns, name), &v1alpha1.TrafficTarget{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.TrafficTarget), err
}

// List takes label and field selectors, and returns the list of TrafficTargets that match those selectors.
func (c *FakeTrafficTargets) List(ctx context.Context, opts v1.ListOptions) (result *v1alpha1.TrafficTargetList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(traffictargetsResource, traffictargetsKind, c.ns, opts), &v1alpha1.TrafficTargetList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1alpha1.TrafficTargetList{ListMeta: obj.(*v1alpha1.TrafficTargetList).ListMeta}
	for _, item := range obj.(*v1alpha1.TrafficTargetList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested trafficTargets.
func (c *FakeTrafficTargets) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(traffictargetsResource, c.ns, opts))

}

// Create takes the representation of a trafficTarget and creates it.  Returns the server's representation of the trafficTarget, and an error, if there is any.
func (c *FakeTrafficTargets) Create(ctx context.Context, trafficTarget *v1alpha1.TrafficTarget, opts v1.CreateOptions) (result *v1alpha1.TrafficTarget, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(traffictargetsResource, c.ns, trafficTarget), &v1alpha1.TrafficTarget{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.TrafficTarget), err
}

// Update takes the representation of a trafficTarget and updates it. Returns the server's representation of the trafficTarget, and an error, if there is any.
func (c *FakeTrafficTargets) Update(ctx context.Context, trafficTarget *v1alpha1.TrafficTarget, opts v1.UpdateOptions) (result *v1alpha1.TrafficTarget, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(traffictargetsResource, c.ns, trafficTarget), &v1alpha1.TrafficTarget{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.TrafficTarget), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeTrafficTargets) UpdateStatus(ctx context.Context, trafficTarget *v1alpha1.TrafficTarget, opts v1.UpdateOptions) (*v1alpha1.TrafficTarget, error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateSubresourceAction(traffictargetsResource, "status", c.ns, trafficTarget), &v1alpha1.TrafficTarget{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.TrafficTarget), err
}

// Delete takes name of the trafficTarget and deletes it. Returns an error if one occurs.
func (c *FakeTrafficTargets) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(traffictargetsResource, c.ns, name), &v1alpha1.TrafficTarget{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeTrafficTargets) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(traffictargetsResource, c.ns, listOpts)

	_, err := c.Fake.Invokes(action, &v1alpha1.TrafficTargetList{})
	return err
}

// Patch applies the patch and returns the patched trafficTarget.
func (c *FakeTrafficTargets) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.TrafficTarget, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(traffictargetsResource, c.ns, name, pt, data, subresources...), &v1alpha1.TrafficTarget{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.TrafficTarget), err
}
