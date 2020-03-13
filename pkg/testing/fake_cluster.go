package testing

import (
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/dynamic"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
)

type FakeCluster struct {
	Name string

	KubeClient          *kubefake.Clientset
	KubeInformerFactory informers.SharedInformerFactory

	ShipperClient          *shipperfake.Clientset
	ShipperInformerFactory shipperinformers.SharedInformerFactory

	DynamicClient *fakedynamic.FakeDynamicClient
}

func NewNamedFakeCluster(name string) *FakeCluster {
	kubeClient := kubefake.NewSimpleClientset()
	kubeInformerFactory := informers.NewSharedInformerFactory(kubeClient,
		NoResyncPeriod)

	shipperClient := shipperfake.NewSimpleClientset()
	shipperInformerFactory := shipperinformers.NewSharedInformerFactory(
		shipperClient, NoResyncPeriod)

	return &FakeCluster{
		Name: name,

		KubeClient:          kubeClient,
		KubeInformerFactory: kubeInformerFactory,

		ShipperClient:          shipperClient,
		ShipperInformerFactory: shipperInformerFactory,
	}
}

// I would love for this to be a single 'Add' with varargs runtime.Object, but
// Go doesn't support that.

func (c *FakeCluster) AddOne(object runtime.Object) {
	c.KubeClient.Tracker().Add(object)
}

func (c *FakeCluster) AddMany(objects []runtime.Object) {
	for _, o := range objects {
		c.AddOne(o)
	}
}

func (c *FakeCluster) InitializeDiscovery(resources []*v1.APIResourceList) {
	fakeDiscovery := c.KubeClient.Discovery().(*fakediscovery.FakeDiscovery)
	fakeDiscovery.Resources = resources
}

func (c *FakeCluster) InitializeDynamicClient(objects []runtime.Object) {
	c.DynamicClient = fakedynamic.NewSimpleDynamicClient(scheme.Scheme, objects...)
}

func (c *FakeCluster) DynamicClientBuilder(kind *schema.GroupVersionKind, restConfig *rest.Config, cluster *shipper.Cluster) dynamic.Interface {
	return c.DynamicClient
}
