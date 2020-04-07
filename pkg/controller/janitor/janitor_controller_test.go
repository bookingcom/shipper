package janitor

import (
	"fmt"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

const (
	clusterA = "cluster-a"
	clusterB = "cluster-b"

	present    = true
	notPresent = false
)

// Release exists, doesn't touch anything
// Release does not exist, deletes all objects

// TestReleaseExists tests that a release that's present in a cluster will
// ensure that the janitor controller does not garbage collect any target
// objects.
func TestReleaseExists(t *testing.T) {
	rel := buildRelease(
		shippertesting.TestNamespace,
		shippertesting.TestApp,
		"release",
		[]string{clusterA},
	)
	it, ct, tt := shippertesting.BuildTargetObjectsForRelease(rel)

	mgmtClusterObjects := []runtime.Object{buildCluster(clusterA), rel}
	appClusterObjects := map[string][]runtime.Object{
		clusterA: []runtime.Object{it, ct, tt},
	}
	expectations := map[string]bool{
		clusterA: present,
	}

	runJanitorControllerTest(t,
		rel.Namespace,
		rel.Name,
		mgmtClusterObjects, appClusterObjects,
		expectations)
}

// TestReleaseDoesNotExist tests that target objects for a release that does
// not exist get garbage collected by the janitor controller.
func TestReleaseDoesNotExist(t *testing.T) {
	// we build a release only for help generating target objects, it won't
	// be added to the cluster.
	rel := buildRelease(
		shippertesting.TestNamespace,
		shippertesting.TestApp,
		"release",
		[]string{clusterA},
	)
	it, ct, tt := shippertesting.BuildTargetObjectsForRelease(rel)

	mgmtClusterObjects := []runtime.Object{buildCluster(clusterA)}
	appClusterObjects := map[string][]runtime.Object{
		clusterA: []runtime.Object{it, ct, tt},
	}
	expectations := map[string]bool{
		clusterA: notPresent,
	}

	runJanitorControllerTest(t,
		rel.Namespace,
		rel.Name,
		mgmtClusterObjects, appClusterObjects,
		expectations)
}

// TestReleaseNotInAllClusters tests that we only keep the target objects in
// the clusters selected in the release, and garbage collect the others.
func TestReleaseNotInAllClusters(t *testing.T) {
	rel := buildRelease(
		shippertesting.TestNamespace,
		shippertesting.TestApp,
		"release",
		[]string{clusterA},
	)

	it, ct, tt := shippertesting.BuildTargetObjectsForRelease(rel)

	mgmtClusterObjects := []runtime.Object{
		buildCluster(clusterA),
		buildCluster(clusterB),
		rel,
	}
	appClusterObjects := map[string][]runtime.Object{
		clusterA: []runtime.Object{it.DeepCopy(), ct.DeepCopy(), tt.DeepCopy()},
		clusterB: []runtime.Object{it.DeepCopy(), ct.DeepCopy(), tt.DeepCopy()},
	}
	expectations := map[string]bool{
		clusterA: present,
		clusterB: notPresent,
	}

	runJanitorControllerTest(t,
		rel.Namespace,
		rel.Name,
		mgmtClusterObjects, appClusterObjects,
		expectations)
}

func runJanitorControllerTest(
	t *testing.T,
	namespace, name string,
	mgmtClusterObjects []runtime.Object,
	appClusterObjects map[string][]runtime.Object,
	expectations map[string]bool,
) {
	f := shippertesting.NewManagementControllerTestFixture(
		mgmtClusterObjects, appClusterObjects)

	runController(f)

	for clusterName, shouldBePresent := range expectations {
		client := f.Clusters[clusterName].ShipperClient

		resources := []string{
			"installationtargets",
			"capacitytargets",
			"traffictargets",
		}

		for _, resource := range resources {
			gvr := shipper.SchemeGroupVersion.WithResource(resource)
			key := fmt.Sprintf("%s/%s", namespace, name)

			_, err := client.Tracker().Get(gvr, namespace, name)
			if err != nil && !errors.IsNotFound(err) {
				t.Fatalf("error getting %s: %s", resource, err)
			}

			found := err == nil

			if found && !shouldBePresent {
				t.Errorf("expected %s %q to not be present in cluster %s, but it was found", resource, key, clusterName)
			} else if !found && shouldBePresent {
				t.Errorf("expected %s %q to be present in cluster %s, but it was not found", resource, key, clusterName)
			}
		}
	}
}

func runController(f *shippertesting.ControllerTestFixture) {
	controller := NewController(
		f.ShipperClient,
		f.ClusterClientStore,
		f.ShipperInformerFactory,
		f.Recorder,
	)

	stopCh := make(chan struct{})
	defer close(stopCh)

	f.Run(stopCh)

	for controller.processNextWorkItem() {
		if controller.workqueue.Len() == 0 {
			time.Sleep(20 * time.Millisecond)
		}
		if controller.workqueue.Len() == 0 {
			return
		}
	}
}
