package installation

import (
	"fmt"
	"sort"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
	"github.com/bookingcom/shipper/pkg/util/anchor"
	installationutil "github.com/bookingcom/shipper/pkg/util/installation"
	targetutil "github.com/bookingcom/shipper/pkg/util/target"
)

const (
	clusterA = "cluster-a"
	clusterB = "cluster-b"

	// needs to match a file in
	// "testdata/chart-cache/$repoUrl/$chartName-$version.tar.gz"
	chartName = "nginx"
	repoUrl   = "https://charts.example.com"
	version   = "0.1.0"
)

type object struct {
	gvr  schema.GroupVersionResource
	name string
}

type installationTargetTestExpectation struct {
	installationTarget *shipper.InstallationTarget
	status             shipper.InstallationTargetStatus
	objectsByCluster   map[string][]object
}

func init() {
	installationutil.InstallationConditionsShouldDiscardTimestamps = true
	targetutil.ConditionsShouldDiscardTimestamps = true
}

// TestSingleCluster verifies that the installation controller creates the
// objects rendered from the chart and reports  readiness.
func TestSingleCluster(t *testing.T) {
	clusters := []string{clusterA}
	chart := buildChart(chartName, version, repoUrl)
	it := buildInstallationTarget(shippertesting.TestNamespace, shippertesting.TestApp, clusters, &chart)

	runInstallationControllerTest(t,
		clusters,
		[]installationTargetTestExpectation{
			{
				installationTarget: it,
				status:             buildSuccessStatus(clusters),
				objectsByCluster: map[string][]object{
					clusterA: buildExpectedObjects(it),
				},
			},
		},
	)
}

// TestMultipleClusters does the same thing as TestSingleCluster, but does so
// for multiple clusters.
func TestMultipleClusters(t *testing.T) {
	clusters := []string{clusterA, clusterB}
	chart := buildChart(chartName, version, repoUrl)
	it := buildInstallationTarget(shippertesting.TestNamespace, shippertesting.TestApp, clusters, &chart)

	runInstallationControllerTest(t,
		clusters,
		[]installationTargetTestExpectation{
			{
				installationTarget: it,
				status:             buildSuccessStatus(clusters),
				objectsByCluster: map[string][]object{
					clusterA: buildExpectedObjects(it),
					clusterB: buildExpectedObjects(it),
				},
			},
		},
	)
}

// TestInvalidChart verifies that the installation controller updates the
// installation traffic with the correct conditions when a chart is invalid.
func TestInvalidChart(t *testing.T) {
	clusters := []string{}
	chart := buildChart("reviews-api", "invalid-deployment-name", repoUrl)
	it := buildInstallationTarget(shippertesting.TestNamespace, shippertesting.TestApp, clusters, &chart)

	status := shipper.InstallationTargetStatus{
		Conditions: []shipper.TargetCondition{
			{
				Type:    shipper.TargetConditionTypeOperational,
				Status:  corev1.ConditionFalse,
				Reason:  ChartError,
				Message: `Deployment "reviews-api" has invalid name. The name of the Deployment should be templated with {{.Release.Name}}.`,
			},
		},
	}

	runInstallationControllerTest(t,
		clusters,
		[]installationTargetTestExpectation{
			{
				installationTarget: it,
				status:             status,
			},
		},
	)
}

// buildExpectedObjects returns a list of the objects we expect from
// `chartName`. This can be hardcoded for as long as we depend on that one chart.
func buildExpectedObjects(it *shipper.InstallationTarget) []object {
	deployment := appsv1.SchemeGroupVersion.WithResource("deployments")
	service := corev1.SchemeGroupVersion.WithResource("services")

	return []object{
		{deployment, fmt.Sprintf("%s-%s", shippertesting.TestApp, chartName)},
		{service, chartName},
		{service, fmt.Sprintf("%s-%s", chartName, "staging")},
	}
}

func runInstallationControllerTest(
	t *testing.T,
	clusterNames []string,
	expectations []installationTargetTestExpectation,
) {
	clusters := make(map[string][]runtime.Object)
	for _, clusterName := range clusterNames {
		clusters[clusterName] = []runtime.Object{}
	}

	f := newFixture(clusters)

	for _, clusterName := range clusterNames {
		f.ShipperClient.Tracker().Add(buildCluster(clusterName))
	}
	for _, expectation := range expectations {
		f.ShipperClient.Tracker().Add(expectation.installationTarget)
	}

	sort.Strings(clusterNames)

	runController(f)

	itGVR := shipper.SchemeGroupVersion.WithResource("installationtargets")
	for _, expectation := range expectations {
		initialIT := expectation.installationTarget
		itKey := fmt.Sprintf("%s/%s", initialIT.Namespace, initialIT.Name)
		object, err := f.ShipperClient.Tracker().Get(itGVR, initialIT.Namespace, initialIT.Name)
		if err != nil {
			t.Errorf("could not Get InstallationTarget %q: %s", itKey, err)
			continue
		}

		it := object.(*shipper.InstallationTarget)

		actualStatus := it.Status
		eq, diff := shippertesting.DeepEqualDiff(expectation.status, actualStatus)
		if !eq {
			t.Errorf(
				"InstallationTarget %q has Status different from expected:\n%s",
				itKey, diff)
			continue
		}

		for _, clusterName := range clusterNames {
			expectedObjects := expectation.objectsByCluster[clusterName]
			assertClusterObjects(t, it, f.Clusters[clusterName], expectedObjects)
		}
	}
}

func assertClusterObjects(
	t *testing.T,
	it *shipper.InstallationTarget,
	cluster *shippertesting.FakeCluster,
	expectedObjects []object,
) {
	// although we don't pass it in expectedObjects, an anchor configmap
	// should always be present
	configmapGVR := corev1.SchemeGroupVersion.WithResource("configmaps")
	configmapName := anchor.CreateAnchorName(it)
	_, err := cluster.Client.Tracker().Get(configmapGVR, it.Namespace, configmapName)
	if err != nil {
		t.Errorf(
			`expected to get ConfigMap %q in cluster %q, but got error instead: %s`,
			configmapName, cluster.Name, err)
	}

	for _, expected := range expectedObjects {
		gvr := expected.gvr
		name := expected.name
		_, err := cluster.DynamicClient.
			Resource(gvr).
			Namespace(it.Namespace).
			Get(name, metav1.GetOptions{})

		if err != nil {
			t.Errorf(
				`expected to get %s %q in cluster %q, but got error instead: %s`,
				gvr.Resource, name, cluster.Name, err)
		}
	}
}

func runController(f *shippertesting.ControllerTestFixture) {
	controller := NewController(
		f.ShipperClient,
		f.ShipperInformerFactory,
		f.ClusterClientStore,
		f.DynamicClientBuilder,
		localFetchChart,
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
