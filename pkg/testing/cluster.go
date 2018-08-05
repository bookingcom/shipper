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

func (c *ClusterFixture) Add(objects ...runtime.Object) {
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
