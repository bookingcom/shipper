package testing

import (
	"k8s.io/apimachinery/pkg/runtime"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	kubetesting "k8s.io/client-go/testing"
)

type FakeCluster struct {
	Name string

	Client          *kubefake.Clientset
	DynamicClient   *fakedynamic.FakeDynamicClient
	InformerFactory informers.SharedInformerFactory

	objects []runtime.Object
	actions []kubetesting.Action
}

func NewFakeCluster(
	client *kubefake.Clientset,
	dynamic *fakedynamic.FakeDynamicClient,
) *FakeCluster {
	return NewNamedFakeCluster("", client, dynamic)
}

func NewNamedFakeCluster(
	name string,
	client *kubefake.Clientset,
	dynamic *fakedynamic.FakeDynamicClient,
) *FakeCluster {
	return &FakeCluster{
		Name:            name,
		Client:          client,
		DynamicClient:   dynamic,
		InformerFactory: informers.NewSharedInformerFactory(client, NoResyncPeriod),
	}
}

// I would love for this to be a single 'Add' with varargs runtime.Object, but
// Go doesn't support that.

func (c *FakeCluster) AddOne(object runtime.Object) {
	c.objects = append(c.objects, object)
	c.Client.Tracker().Add(object)
}

func (c *FakeCluster) AddMany(objects []runtime.Object) {
	c.objects = append(c.objects, objects...)
	for _, o := range objects {
		c.Client.Tracker().Add(o)
	}
}

func (c *FakeCluster) Expect(actions ...kubetesting.Action) {
	c.actions = append(c.actions, actions...)
}

func (c *FakeCluster) Objects() []runtime.Object {
	return c.objects
}

func (c *FakeCluster) ExpectedActions() []kubetesting.Action {
	return c.actions
}
