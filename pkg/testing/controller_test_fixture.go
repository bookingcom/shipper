package testing

import (
	"fmt"

	"k8s.io/client-go/tools/record"
)

type ControllerTestFixture struct {
	*FakeCluster

	Clusters           map[string]*FakeCluster
	ClusterClientStore *FakeClusterClientStore

	Recorder *record.FakeRecorder
}

func NewControllerTestFixture() *ControllerTestFixture {
	const recorderBufSize = 42

	store := NewFakeClusterClientStore()
	fakeCluster := NewNamedFakeCluster("mgmt")

	return &ControllerTestFixture{
		FakeCluster: fakeCluster,

		Clusters:           make(map[string]*FakeCluster),
		ClusterClientStore: store,

		Recorder: record.NewFakeRecorder(recorderBufSize),
	}
}

func (f *ControllerTestFixture) AddNamedCluster(name string) *FakeCluster {
	cluster := NewNamedFakeCluster(name)

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
