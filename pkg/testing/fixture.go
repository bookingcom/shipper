package testing

import (
	"fmt"
	"testing"
)

type Fixture struct {
	T                   *testing.T
	ManagementCluster   *ClusterFixture
	ApplicationClusters []*ClusterFixture
	Store               *clusterclientstore.Store
	stop                chan struct{}
}

func NewFixture(name string, stopCh chan struct{}, t *testing.T) *Fixture {
	return &Fixture{
		T: t,
		// slightly annoying; don't want to create the cluster client store fixture just yet
		Store:             nil,
		ManagementCluster: &ClusterFixture{Name: "management"},
		stop:              stopCh,
	}
}

func (f *Fixture) AddCluster() *ClusterFixture {
	name := fmt.Sprintf("cluster-%d", len(f.ApplicationClusters))
	cluster := NewClusterFixture(name)
	f.ApplicationClusters = append(f.ApplicationClusters, cluster)
	return cluster
}

// change traffic_controller_test to use this instead of store directly
// try to take over "newController" from fixtures; maybe also most portions of 'run'
