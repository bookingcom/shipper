package cache

import (
	"fmt"
	"testing"
	"time"

	kubeinformers "k8s.io/client-go/informers"
	kubernetes "k8s.io/client-go/kubernetes"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
)

const (
	testClusterName = "test-cluster"
	testChecksum    = "test-checksum"
)

func TestStoreFetch(t *testing.T) {
	expected := newCluster(testClusterName)
	srv := NewServer()
	defer srv.Stop()

	go srv.Serve()

	srv.Store(expected)
	fetched, _ := srv.Fetch(testClusterName)
	if expected != fetched {
		t.Errorf("expected to fetch %v, but got %v", expected, fetched)
	}
}

func TestStoreCount(t *testing.T) {
	srv := NewServer()
	defer srv.Stop()

	target := 100
	go srv.Serve()

	for i := 0; i < target; i++ {
		srv.Store(newCluster(fmt.Sprintf("cluster-%d", i)))
	}

	if srv.Count() != target {
		t.Errorf("expected %d clusters, but found %d", target, srv.Count())
	}
}

// New clusters with the same name + key properties should not overwrite the
// existing cluster.
func TestStoreDuplicatesNoReplacement(t *testing.T) {
	expected := newCluster(testClusterName)
	srv := NewServer()
	defer srv.Stop()

	target := 100
	go srv.Serve()
	srv.Store(expected)

	for i := 0; i < target; i++ {
		srv.Store(newCluster(testClusterName))
	}

	found, _ := srv.Fetch(testClusterName)
	if found != expected {
		t.Errorf("expected redundant updates to be discarded and still find cluster %v, but instead found cluster %v", expected, found)
	}

	if srv.Count() != 1 {
		t.Errorf("expected %d clusters, but found %d", 1, srv.Count())
	}
}

func TestStoreRemove(t *testing.T) {
	srv := NewServer()
	defer srv.Stop()

	go srv.Serve()

	srv.Store(newCluster(testClusterName))
	_, ok := srv.Fetch(testClusterName)
	if !ok {
		t.Errorf("expected to fetch cluster %q but didn't find it", testClusterName)
	}

	srv.Remove(testClusterName)
	_, ok = srv.Fetch(testClusterName)
	if ok {
		t.Errorf("expected cluster %q to be deleted, but still found it", testClusterName)
	}

	if srv.Count() > 0 {
		t.Errorf("expected zero clusters, but found %d", srv.Count())
	}
}

func TestReplacement(t *testing.T) {
	srv := NewServer()
	defer srv.Stop()

	go srv.Serve()

	existing := newCluster(testClusterName)
	replacement := newCluster(testClusterName)
	replacement.checksum = fmt.Sprintf("totally not %s", testChecksum)

	srv.Store(existing)
	fetched, ok := srv.Fetch(testClusterName)
	if !ok || fetched != existing {
		t.Errorf("expected to fetch cluster %v but got something else: %v", existing, fetched)
		return
	}

	// Checksum is different, so the replacement should replace the existing.
	srv.Store(replacement)

	fetched, ok = srv.Fetch(testClusterName)
	if !ok {
		t.Errorf("expected to fetch cluster %v but got nothing", fetched)
		return
	}

	if fetched != replacement {
		t.Errorf("expected to fetch cluster %v but got %v instead", replacement, fetched)
		return
	}

	_, err := existing.GetChecksum()
	if !shippererrors.IsClusterNotReadyError(err) {
		t.Errorf("expected GetChecksum on replaced cluster to return ClusterNotReady, got %v", err)
	}

	_, err = existing.GetKubeClient("foo")
	if !shippererrors.IsClusterNotReadyError(err) {
		t.Errorf("expected GetKubeClient on replaced cluster to return ClusterNotReady, got %v", err)
	}

	_, err = existing.GetShipperClient("foo")
	if !shippererrors.IsClusterNotReadyError(err) {
		t.Errorf("expected GetShipperClient on replaced cluster to return ClusterNotReady, got %v", err)
	}

	_, err = existing.GetConfig()
	if !shippererrors.IsClusterNotReadyError(err) {
		t.Errorf("expected GetConfig on replaced cluster to return ClusterNotReady, got %v", err)
	}

	_, err = existing.GetKubeInformerFactory()
	if !shippererrors.IsClusterNotReadyError(err) {
		t.Errorf("expected GetKubeInformerFactory on replaced cluster to return ClusterNotReady, got %v", err)
	}

	_, err = existing.GetShipperInformerFactory()
	if !shippererrors.IsClusterNotReadyError(err) {
		t.Errorf("expected GetShipperInformerFactory on replaced cluster to return ClusterNotReady, got %v", err)
	}
}

func newCluster(name string) *Cluster {
	kubeClient := kubefake.NewSimpleClientset()
	shipperClient := shipperfake.NewSimpleClientset()

	const noResyncPeriod time.Duration = 0
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, noResyncPeriod)
	shipperInformerFactory := shipperinformers.NewSharedInformerFactory(shipperClient, noResyncPeriod)
	config := &rest.Config{}

	return NewCluster(
		name, testChecksum, config,
		kubeInformerFactory, shipperInformerFactory,
		func(cluster, ua string, config *rest.Config) (kubernetes.Interface, error) {
			return nil, nil
		},
		func(cluster, ua string, config *rest.Config) (shipperclientset.Interface, error) {
			return nil, nil
		},
		func() {},
	)
}
