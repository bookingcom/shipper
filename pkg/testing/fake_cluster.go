package testing

import (
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakediscovery "k8s.io/client-go/discovery/fake"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
)

type FakeCluster struct {
	Name string

	Client          *kubefake.Clientset
	DynamicClient   *fakedynamic.FakeDynamicClient
	InformerFactory informers.SharedInformerFactory
}

func NewNamedFakeCluster(name string) *FakeCluster {
	client := kubefake.NewSimpleClientset()
	return &FakeCluster{
		Name:            name,
		Client:          client,
		InformerFactory: informers.NewSharedInformerFactory(client, NoResyncPeriod),
	}
}

// I would love for this to be a single 'Add' with varargs runtime.Object, but
// Go doesn't support that.

func (c *FakeCluster) AddOne(object runtime.Object) {
	c.Client.Tracker().Add(object)
}

func (c *FakeCluster) AddMany(objects []runtime.Object) {
	for _, o := range objects {
		c.Client.Tracker().Add(o)
	}
}

func (c *FakeCluster) InitializeDiscovery(resources []*v1.APIResourceList) {
	fakeDiscovery := c.Client.Discovery().(*fakediscovery.FakeDiscovery)
	fakeDiscovery.Resources = resources
}

func (c *FakeCluster) InitializeDynamicClient(objects []runtime.Object) {
	c.DynamicClient = fakedynamic.NewSimpleDynamicClient(scheme.Scheme, objects...)
}
