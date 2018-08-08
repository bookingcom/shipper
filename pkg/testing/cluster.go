package testing

import (
	"k8s.io/apimachinery/pkg/runtime"
	kubetesting "k8s.io/client-go/testing"
)

type ClusterFixture struct {
	Name    string
	objects []runtime.Object
	actions []kubetesting.Action
}

func NewClusterFixture(name string) *ClusterFixture {
	return &ClusterFixture{
		Name: name,
	}
}

// I would love for this to be a single 'Add' with varargs runtime.Object, but Go doesn't support that.
func (c *ClusterFixture) AddOne(object runtime.Object) {
	c.objects = append(c.objects, object)
}

func (c *ClusterFixture) AddMany(objects []runtime.Object) {
	c.objects = append(c.objects, objects...)
}

func (c *ClusterFixture) Expect(actions ...kubetesting.Action) {
	c.actions = append(c.actions, actions...)
}

func (c *ClusterFixture) Objects() []runtime.Object {
	return c.objects
}

func (c *ClusterFixture) ExpectedActions() []kubetesting.Action {
	return c.actions
}
