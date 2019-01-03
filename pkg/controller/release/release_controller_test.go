package release

import (
	"log"
	"os"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/helm/pkg/repo/repotest"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/conditions"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

func init() {
	releaseutil.ConditionsShouldDiscardTimestamps = true
	conditions.StrategyConditionsShouldDiscardTimestamps = true
}

var chartRepoURL string

func TestMain(m *testing.M) {
	srv, hh, err := repotest.NewTempServer("testdata/*.tgz")
	if err != nil {
		log.Fatal(err)
	}

	chartRepoURL = srv.URL()

	result := m.Run()

	// If the test panics for any reason, the test server doesn't get cleaned up.
	os.RemoveAll(hh.String())
	srv.Stop()

	os.Exit(result)
}

func TestControllerComputeTargetClusters(t *testing.T) {
	cluster := buildCluster("minikube-a")
	release := buildRelease()
	release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()

	fixtures := []runtime.Object{release, cluster}

	expected := release.DeepCopy()
	expected.Status.Conditions = []shipper.ReleaseCondition{
		{Type: shipper.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
	}
	//expectedActions := buildExpectedActions(release.GetNamespace(), expected)

	controller := newReleaseController(fixtures...)
	controller.processNextReleaseWorkItem()
}
