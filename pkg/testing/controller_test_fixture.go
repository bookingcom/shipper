package testing

import (
	"fmt"

	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"

	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
)

type ControllerTestFixture struct {
	ShipperClient          *shipperfake.Clientset
	ShipperInformerFactory shipperinformers.SharedInformerFactory

	Clusters           map[string]*FakeCluster
	ClusterClientStore *FakeClusterClientStore

	Recorder *record.FakeRecorder
}

func NewControllerTestFixture() *ControllerTestFixture {
	const recorderBufSize = 42

	shipperClient := shipperfake.NewSimpleClientset()
	shipperInformerFactory := shipperinformers.NewSharedInformerFactory(
		shipperClient, NoResyncPeriod)

	store := NewFakeClusterClientStore(map[string]*FakeCluster{})

	return &ControllerTestFixture{
		ShipperClient:          shipperClient,
		ShipperInformerFactory: shipperInformerFactory,

		Clusters:           make(map[string]*FakeCluster),
		ClusterClientStore: store,

		Recorder: record.NewFakeRecorder(recorderBufSize),
	}
}

func (f *ControllerTestFixture) AddNamedCluster(name string) *FakeCluster {
	cluster := NewNamedFakeCluster(
		name,
		kubefake.NewSimpleClientset(), nil)

	f.Clusters[cluster.Name] = cluster
	f.ClusterClientStore.AddCluster(cluster)

	return cluster
}

func (f *ControllerTestFixture) AddCluster() *FakeCluster {
	name := fmt.Sprintf("cluster-%d", len(f.Clusters))
	return f.AddNamedCluster(name)
}

func (f *ControllerTestFixture) Run(stopCh chan struct{}) {
	f.ShipperInformerFactory.Start(stopCh)
	f.ShipperInformerFactory.WaitForCacheSync(stopCh)
	f.ClusterClientStore.Run(stopCh)
}
