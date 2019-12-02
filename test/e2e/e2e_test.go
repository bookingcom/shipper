package e2e

import (
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	"github.com/bookingcom/shipper/pkg/util/replicas"
)

const (
	appName          = "my-test-app"
	rolloutBlockName = "my-test-rollout-block"
)

var (
	masterURL      = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	runEndToEnd    = flag.Bool("e2e", false, "Set this flag to enable E2E tests against the local minikube")
	testCharts     = flag.String("testcharts", "", "The address of the Helm repository holding the test charts")
	inspectFailed  = flag.Bool("inspectfailed", false, "Set this flag to skip deleting the namespaces for failed tests. Useful for debugging.")
	kubeconfig     = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	appClusterName = flag.String("appcluster", "minikube", "The application cluster that E2E tests will check to determine success/failure")
	timeoutFlag    = flag.String("progresstimeout", "30s", "timeout when waiting for things to change")
)

var (
	appKubeClient kubernetes.Interface
	kubeClient    kubernetes.Interface
	shipperClient shipperclientset.Interface
	chartRepo     string
	testRegion    string
	globalTimeout time.Duration
)

var allIn = shipper.RolloutStrategy{
	Steps: []shipper.RolloutStrategyStep{
		{
			Name:     "full on",
			Capacity: shipper.RolloutStrategyStepValue{Incumbent: 0, Contender: 100},
			Traffic:  shipper.RolloutStrategyStepValue{Incumbent: 0, Contender: 100},
		},
	},
}

var vanguard = shipper.RolloutStrategy{
	Steps: []shipper.RolloutStrategyStep{
		{
			Name:     "staging",
			Capacity: shipper.RolloutStrategyStepValue{Incumbent: 100, Contender: 1},
			Traffic:  shipper.RolloutStrategyStepValue{Incumbent: 100, Contender: 0},
		},
		{
			Name:     "50/50",
			Capacity: shipper.RolloutStrategyStepValue{Incumbent: 50, Contender: 50},
			Traffic:  shipper.RolloutStrategyStepValue{Incumbent: 50, Contender: 50},
		},
		{
			Name:     "full on",
			Capacity: shipper.RolloutStrategyStepValue{Incumbent: 0, Contender: 100},
			Traffic:  shipper.RolloutStrategyStepValue{Incumbent: 0, Contender: 100},
		},
	},
}

func TestMain(m *testing.M) {
	flag.Parse()
	var err error
	if *runEndToEnd {
		globalTimeout, err = time.ParseDuration(*timeoutFlag)
		if err != nil {
			klog.Fatalf("could not parse given timeout duration: %q", err)
		}

		kubeClient, shipperClient, err = buildManagementClients(*masterURL, *kubeconfig)
		if err != nil {
			klog.Fatalf("could not build a client: %v", err)
		}

		appCluster, err := shipperClient.ShipperV1alpha1().Clusters().Get(*appClusterName, metav1.GetOptions{})
		if err != nil {
			klog.Fatalf("could not fetch cluster object for cluster %q: %q", *appClusterName, err)
		}

		testRegion = appCluster.Spec.Region

		appKubeClient = buildApplicationClient(appCluster)
		purgeTestNamespaces()
	}

	chartRepo = *testCharts

	exitCode := m.Run()

	os.Exit(exitCode)
}

func TestNewAppAllIn(t *testing.T) {
	if !*runEndToEnd {
		t.Skip("skipping end-to-end tests: --e2e is false")
	}
	t.Parallel()

	targetReplicas := 4
	ns, err := setupNamespace(t.Name())
	f := newFixture(ns.GetName(), t)
	if err != nil {
		t.Fatalf("could not create namespace %s: %q", ns.GetName(), err)
	}
	defer func() {
		if *inspectFailed && t.Failed() {
			return
		}
		teardownNamespace(ns.GetName())
	}()

	newApp := newApplication(ns.GetName(), appName, &allIn)
	newApp.Spec.Template.Values = &shipper.ChartValues{"replicaCount": targetReplicas}
	newApp.Spec.Template.Chart.Name = "test-nginx"
	newApp.Spec.Template.Chart.Version = "0.0.1"

	_, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Create(newApp)
	if err != nil {
		t.Fatalf("could not create application %q: %q", appName, err)
	}

	t.Logf("waiting for a new release for new application %q", appName)
	rel := f.waitForRelease(appName, 0)
	relName := rel.GetName()
	t.Logf("waiting for release %q to complete", relName)
	f.waitForComplete(rel.GetName())
	t.Logf("checking that release %q has %d pods (strategy step 0 -- finished)", relName, targetReplicas)
	f.checkReadyPods(relName, targetReplicas)
}

func TestNewAppAllInWithRolloutBlockOverride(t *testing.T) {
	if !*runEndToEnd {
		t.Skip("skipping end-to-end tests: --e2e is false")
	}
	t.Parallel()

	targetReplicas := 4
	ns, err := setupNamespace(t.Name())
	f := newFixture(ns.GetName(), t)
	if err != nil {
		t.Fatalf("could not create namespace %s: %q", ns.GetName(), err)
	}
	defer func() {
		if *inspectFailed && t.Failed() {
			return
		}
		teardownNamespace(ns.GetName())
	}()

	rb, err := createRolloutBlock(ns.GetName(), rolloutBlockName)
	if err != nil {
		t.Fatalf("could not create rollout block %q: %q", rolloutBlockName, err)
	}

	newApp := newApplication(ns.GetName(), appName, &allIn)
	newApp.Annotations[shipper.RolloutBlocksOverrideAnnotation] = fmt.Sprintf("%s/%s", rb.GetNamespace(), rb.GetName())
	newApp.Spec.Template.Values = &shipper.ChartValues{"replicaCount": targetReplicas}
	newApp.Spec.Template.Chart.Name = "test-nginx"
	newApp.Spec.Template.Chart.Version = "0.0.1"

	_, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Create(newApp)
	if err != nil {
		t.Fatalf("could not create application %q: %q", appName, err)
	}

	t.Logf("waiting for a new release for new application %q", appName)
	rel := f.waitForRelease(appName, 0)
	relName := rel.GetName()
	t.Logf("waiting for release %q to complete", relName)
	f.waitForComplete(rel.GetName())
	t.Logf("checking that release %q has %d pods (strategy step 0 -- finished)", relName, targetReplicas)
	f.checkReadyPods(relName, targetReplicas)
}

func TestBlockNewAppWithRolloutBlock(t *testing.T) {
	if !*runEndToEnd {
		t.Skip("skipping end-to-end tests: --e2e is false")
	}
	t.Parallel()

	targetReplicas := 4
	ns, err := setupNamespace(t.Name())
	if err != nil {
		t.Fatalf("could not create namespace %s: %q", ns.GetName(), err)
	}
	defer func() {
		if *inspectFailed && t.Failed() {
			return
		}
		teardownNamespace(ns.GetName())
	}()

	_, err = createRolloutBlock(ns.GetName(), rolloutBlockName)
	if err != nil {
		t.Fatalf("could not create rollout block %q: %q", rolloutBlockName, err)
	}

	newApp := newApplication(ns.GetName(), appName, &allIn)
	newApp.Spec.Template.Values = &shipper.ChartValues{"replicaCount": targetReplicas}
	newApp.Spec.Template.Chart.Name = "test-nginx"
	newApp.Spec.Template.Chart.Version = "0.0.1"

	_, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Create(newApp)
	if err != nil {
		t.Logf("successfully did not create application %q: %q", appName, err)
		return
	} else {
		t.Fatalf("did not block application %q", appName)
	}
}

func TestNewAppAllInWithRolloutBlockNonExisting(t *testing.T) {
	if !*runEndToEnd {
		t.Skip("skipping end-to-end tests: --e2e is false")
	}
	t.Parallel()

	targetReplicas := 4
	ns, err := setupNamespace(t.Name())
	_ = newFixture(ns.GetName(), t)
	if err != nil {
		t.Fatalf("could not create namespace %s: %q", ns.GetName(), err)
	}
	defer func() {
		if *inspectFailed && t.Failed() {
			return
		}
		teardownNamespace(ns.GetName())
	}()

	newApp := newApplication(ns.GetName(), appName, &allIn)
	newApp.Annotations[shipper.RolloutBlocksOverrideAnnotation] = fmt.Sprintf("%s/%s", ns.GetName(), rolloutBlockName)
	newApp.Spec.Template.Values = &shipper.ChartValues{"replicaCount": targetReplicas}
	newApp.Spec.Template.Chart.Name = "test-nginx"
	newApp.Spec.Template.Chart.Version = "0.0.1"

	_, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Create(newApp)
	if err != nil {
		t.Logf("successfully did not create application %q: %q", appName, err)
		return
	} else {
		t.Fatalf("created application %q with non-existing rollout block override", appName)
	}
}

func TestBlockNewAppProgressWithRolloutBlock(t *testing.T) {
	if !*runEndToEnd {
		t.Skip("skipping end-to-end tests: --e2e is false")
	}
	t.Parallel()

	targetReplicas := 4
	ns, err := setupNamespace(t.Name())
	f := newFixture(ns.GetName(), t)
	if err != nil {
		t.Fatalf("could not create namespace %s: %q", ns.GetName(), err)
	}
	defer func() {
		if *inspectFailed && t.Failed() {
			return
		}
		teardownNamespace(ns.GetName())
	}()

	newApp := newApplication(ns.GetName(), appName, &allIn)
	newApp.Spec.Template.Values = &shipper.ChartValues{"replicaCount": targetReplicas}
	newApp.Spec.Template.Chart.Name = "test-nginx"
	newApp.Spec.Template.Chart.Version = "0.0.1"

	_, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Create(newApp)
	if err != nil {
		t.Fatalf("could not create application %q: %q", appName, err)
	}

	_, err = createRolloutBlock(ns.GetName(), rolloutBlockName)
	if err != nil {
		t.Fatalf("could not create rollout block %q: %q", rolloutBlockName, err)
	}

	f.checkReadyPods(appName, 0)
}

func TestRolloutAllIn(t *testing.T) {
	if !*runEndToEnd {
		t.Skip("skipping end-to-end tests: --e2e is false")
	}
	t.Parallel()

	targetReplicas := 4
	ns, err := setupNamespace(t.Name())
	f := newFixture(ns.GetName(), t)
	if err != nil {
		t.Fatalf("could not create namespace %s: %q", ns.GetName(), err)
	}
	defer func() {
		if *inspectFailed && t.Failed() {
			return
		}
		teardownNamespace(ns.GetName())
	}()

	app := newApplication(ns.GetName(), appName, &allIn)
	app.Spec.Template.Values = &shipper.ChartValues{"replicaCount": targetReplicas}
	app.Spec.Template.Chart.Name = "test-nginx"
	app.Spec.Template.Chart.Version = "0.0.1"

	_, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Create(app)
	if err != nil {
		t.Fatalf("could not create application %q: %q", appName, err)
	}

	t.Logf("waiting for a new release for new application %q", appName)
	rel := f.waitForRelease(appName, 0)
	relName := rel.GetName()
	t.Logf("waiting for release %q to complete", relName)
	f.waitForComplete(rel.GetName())
	t.Logf("checking that release %q has %d pods (strategy step 0 -- finished)", relName, targetReplicas)
	f.checkReadyPods(relName, targetReplicas)

	// refetch so that the update has a fresh version to work with
	app, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Get(app.GetName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("could not refetch app: %q", err)
	}

	app.Spec.Template.Chart.Version = "0.0.2"
	_, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Update(app)
	if err != nil {
		t.Fatalf("could not update application %q: %q", appName, err)
	}

	t.Logf("waiting for contender release to appear after editing app %q", app.GetName())
	contender := f.waitForRelease(appName, 1)
	t.Logf("waiting for contender %q to complete", contender.GetName())
	f.waitForComplete(contender.GetName())
	t.Logf("checking that release %q has %d pods (strategy step 0 -- finished)", contender.GetName(), targetReplicas)
	f.checkReadyPods(contender.GetName(), targetReplicas)
}

func TestRolloutAllInWithRolloutBlockOverride(t *testing.T) {
	if !*runEndToEnd {
		t.Skip("skipping end-to-end tests: --e2e is false")
	}
	t.Parallel()

	targetReplicas := 4
	ns, err := setupNamespace(t.Name())
	f := newFixture(ns.GetName(), t)
	if err != nil {
		t.Fatalf("could not create namespace %s: %q", ns.GetName(), err)
	}
	defer func() {
		if *inspectFailed && t.Failed() {
			return
		}
		teardownNamespace(ns.GetName())
	}()

	rb, err := createRolloutBlock(ns.GetName(), rolloutBlockName)
	if err != nil {
		t.Fatalf("could not create rollout block %q: %q", rolloutBlockName, err)
	}

	app := newApplication(ns.GetName(), appName, &allIn)
	app.Annotations[shipper.RolloutBlocksOverrideAnnotation] = fmt.Sprintf("%s/%s", rb.GetNamespace(), rb.GetName())
	app.Spec.Template.Values = &shipper.ChartValues{"replicaCount": targetReplicas}
	app.Spec.Template.Chart.Name = "test-nginx"
	app.Spec.Template.Chart.Version = "0.0.1"

	_, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Create(app)
	if err != nil {
		t.Fatalf("could not create application %q: %q", appName, err)
	}

	t.Logf("waiting for a new release for new application %q", appName)
	rel := f.waitForRelease(appName, 0)
	relName := rel.GetName()
	t.Logf("waiting for release %q to complete", relName)
	f.waitForComplete(rel.GetName())
	t.Logf("checking that release %q has %d pods (strategy step 0 -- finished)", relName, targetReplicas)
	f.checkReadyPods(relName, targetReplicas)

	t.Log("refetch so that the update has a fresh version to work with")
	app, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Get(app.GetName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("could not refetch app: %q", err)
	}

	t.Log("changing chart version to 0.0.2")
	app.Spec.Template.Chart.Version = "0.0.2"
	_, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Update(app)
	if err != nil {
		t.Fatalf("could not update application %q: %q", appName, err)
	}

	t.Logf("waiting for contender release to appear after editing app %q", app.GetName())
	contender := f.waitForRelease(appName, 1)
	t.Logf("waiting for contender %q to complete", contender.GetName())
	f.waitForComplete(contender.GetName())
	t.Logf("checking that release %q has %d pods (strategy step 0 -- finished)", contender.GetName(), targetReplicas)
	f.checkReadyPods(contender.GetName(), targetReplicas)
}

func testNewApplicationVanguard(targetReplicas int, t *testing.T) {
	if !*runEndToEnd {
		t.Skip("skipping end-to-end tests: --e2e is false")
	}

	t.Parallel()
	ns, err := setupNamespace(t.Name())
	f := newFixture(ns.GetName(), t)
	if err != nil {
		t.Fatalf("could not create namespace %s: %q", ns.GetName(), err)
	}
	defer func() {
		if *inspectFailed && t.Failed() {
			return
		}
		teardownNamespace(ns.GetName())
	}()

	newApp := newApplication(ns.GetName(), appName, &vanguard)
	newApp.Spec.Template.Values = &shipper.ChartValues{"replicaCount": targetReplicas}
	newApp.Spec.Template.Chart.Name = "test-nginx"
	newApp.Spec.Template.Chart.Version = "0.0.1"

	_, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Create(newApp)
	if err != nil {
		t.Fatalf("could not create application %q: %q", appName, err)
	}

	t.Logf("waiting for a new release for new application %q", appName)
	rel := f.waitForRelease(appName, 0)
	relName := rel.GetName()

	for i, step := range vanguard.Steps {
		t.Logf("setting release %q targetStep to %d", relName, i)
		f.targetStep(i, relName)

		if i == len(vanguard.Steps)-1 {
			t.Logf("waiting for release %q to complete", relName)
			f.waitForComplete(relName)
		} else {
			t.Logf("waiting for release %q to achieve waitingForCommand for targetStep %d", relName, i)
			f.waitForReleaseStrategyState("command", relName, i)
		}

		expectedCapacity := int(replicas.CalculateDesiredReplicaCount(uint(step.Capacity.Contender), float64(targetReplicas)))
		t.Logf("checking that release %q has %d pods (strategy step %d aka %q)", relName, expectedCapacity, i, step.Name)
		f.checkReadyPods(relName, expectedCapacity)
	}
}

func testNewApplicationVanguardWithRolloutBlockOverride(targetReplicas int, t *testing.T) {
	if !*runEndToEnd {
		t.Skip("skipping end-to-end tests: --e2e is false")
	}

	t.Parallel()
	ns, err := setupNamespace(t.Name())
	f := newFixture(ns.GetName(), t)
	if err != nil {
		t.Fatalf("could not create namespace %s: %q", ns.GetName(), err)
	}
	defer func() {
		if *inspectFailed && t.Failed() {
			return
		}
		teardownNamespace(ns.GetName())
	}()

	rb, err := createRolloutBlock(ns.GetName(), rolloutBlockName)
	if err != nil {
		t.Fatalf("could not create rollout block %q: %q", rolloutBlockName, err)
	}

	newApp := newApplication(ns.GetName(), appName, &vanguard)
	newApp.Annotations[shipper.RolloutBlocksOverrideAnnotation] = fmt.Sprintf("%s/%s", rb.GetNamespace(), rb.GetName())
	newApp.Spec.Template.Values = &shipper.ChartValues{"replicaCount": targetReplicas}
	newApp.Spec.Template.Chart.Name = "test-nginx"
	newApp.Spec.Template.Chart.Version = "0.0.1"

	_, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Create(newApp)
	if err != nil {
		t.Fatalf("could not create application %q: %q", appName, err)
	}

	t.Logf("waiting for a new release for new application %q", appName)
	rel := f.waitForRelease(appName, 0)
	relName := rel.GetName()

	for i, step := range vanguard.Steps {
		t.Logf("setting release %q targetStep to %d", relName, i)
		f.targetStep(i, relName)

		if i == len(vanguard.Steps)-1 {
			t.Logf("waiting for release %q to complete", relName)
			f.waitForComplete(relName)
		} else {
			t.Logf("waiting for release %q to achieve waitingForCommand for targetStep %d", relName, i)
			f.waitForReleaseStrategyState("command", relName, i)
		}

		expectedCapacity := int(replicas.CalculateDesiredReplicaCount(uint(step.Capacity.Contender), float64(targetReplicas)))
		t.Logf("checking that release %q has %d pods (strategy step %d aka %q)", relName, expectedCapacity, i, step.Name)
		f.checkReadyPods(relName, expectedCapacity)
	}
}

// TestNewApplicationVanguardMultipleReplicas tests the creation of a new
// application with multiple replicas, marching through the specified vanguard
// strategy until it hopefully converges on the final desired state.
func TestNewApplicationVanguardMultipleReplicas(t *testing.T) {
	testNewApplicationVanguard(3, t)
}

func TestNewApplicationVanguardMultipleReplicasRBOverride(t *testing.T) {
	testNewApplicationVanguardWithRolloutBlockOverride(3, t)
}

// TestNewApplicationVanguardOneReplica tests the creation of a new
// application with one replica, marching through the specified vanguard
// strategy until it hopefully converges on the final desired state.
func TestNewApplicationVanguardOneReplica(t *testing.T) {
	testNewApplicationVanguard(1, t)
}

func TestNewApplicationVanguardOneReplicaRBOverride(t *testing.T) {
	testNewApplicationVanguardWithRolloutBlockOverride(1, t)
}

// testRolloutVanguard tests the creation of a new application with the
// specified number of replicas, marching through the specified vanguard
// strategy until it hopefully converges on the final desired state.
func testRolloutVanguard(targetReplicas int, t *testing.T) {
	if !*runEndToEnd {
		t.Skip("skipping end-to-end tests: --e2e is false")
	}

	t.Parallel()
	ns, err := setupNamespace(t.Name())
	f := newFixture(ns.GetName(), t)
	if err != nil {
		t.Fatalf("could not create namespace %s: %q", ns.GetName(), err)
	}
	defer func() {
		if *inspectFailed && t.Failed() {
			return
		}
		teardownNamespace(ns.GetName())
	}()

	// start with allIn to jump through the first release
	app := newApplication(ns.GetName(), appName, &allIn)
	app.Spec.Template.Values = &shipper.ChartValues{"replicaCount": targetReplicas}
	app.Spec.Template.Chart.Name = "test-nginx"
	app.Spec.Template.Chart.Version = "0.0.1"

	_, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Create(app)
	if err != nil {
		t.Fatalf("could not create application %q: %q", appName, err)
	}

	incumbent := f.waitForRelease(appName, 0)
	incumbentName := incumbent.GetName()
	f.waitForComplete(incumbentName)
	f.checkReadyPods(incumbentName, targetReplicas)

	// Refetch so that the update has a fresh version to work with.
	app, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Get(app.GetName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("could not refetch app: %q", err)
	}

	app.Spec.Template.Strategy = &vanguard
	app.Spec.Template.Chart.Version = "0.0.2"
	_, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Update(app)
	if err != nil {
		t.Fatalf("could not update application %q: %q", appName, err)
	}

	t.Logf("waiting for contender release to appear after editing app %q", app.GetName())
	contender := f.waitForRelease(appName, 1)
	contenderName := contender.GetName()

	for i, step := range vanguard.Steps {
		t.Logf("setting release %q targetStep to %d", contenderName, i)
		f.targetStep(i, contenderName)

		if i == len(vanguard.Steps)-1 {
			t.Logf("waiting for release %q to complete", contenderName)
			f.waitForComplete(contenderName)
		} else {
			t.Logf("waiting for release %q to achieve waitingForCommand for targetStep %d", contenderName, i)
			f.waitForReleaseStrategyState("command", contenderName, i)
		}

		expectedContenderCapacity := replicas.CalculateDesiredReplicaCount(uint(step.Capacity.Contender), float64(targetReplicas))
		expectedIncumbentCapacity := replicas.CalculateDesiredReplicaCount(uint(step.Capacity.Incumbent), float64(targetReplicas))

		t.Logf(
			"checking that incumbent %q has %d pods and contender %q has %d pods (strategy step %d -- %d/%d)",
			incumbentName, expectedIncumbentCapacity, contenderName, expectedContenderCapacity, i, step.Capacity.Incumbent, step.Capacity.Contender,
		)

		f.checkReadyPods(contenderName, int(expectedContenderCapacity))
		f.checkReadyPods(incumbentName, int(expectedIncumbentCapacity))
	}
}

func TestRolloutVanguardMultipleReplicas(t *testing.T) {
	testRolloutVanguard(4, t)
}

func TestRolloutVanguardOneReplica(t *testing.T) {
	testRolloutVanguard(1, t)
}

func TestNewApplicationMovingStrategyBackwards(t *testing.T) {
	if !*runEndToEnd {
		t.Skip("skipping end-to-end tests: --e2e is false")
	}

	t.Parallel()
	targetReplicas := 4
	ns, err := setupNamespace(t.Name())
	f := newFixture(ns.GetName(), t)
	if err != nil {
		t.Fatalf("could not create namespace %s: %q", ns.GetName(), err)
	}
	defer func() {
		if *inspectFailed && t.Failed() {
			return
		}
		teardownNamespace(ns.GetName())
	}()

	app := newApplication(ns.GetName(), appName, &vanguard)
	app.Spec.Template.Values = &shipper.ChartValues{"replicaCount": targetReplicas}
	app.Spec.Template.Chart.Name = "test-nginx"
	app.Spec.Template.Chart.Version = "0.0.1"

	_, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Create(app)
	if err != nil {
		t.Fatalf("could not create application %q: %q", appName, err)
	}

	t.Logf("waiting for a new release for new application %q", appName)
	rel := f.waitForRelease(appName, 0)
	relName := rel.GetName()

	for _, i := range []int{0, 1, 0} {
		step := vanguard.Steps[i]
		t.Logf("setting release %q targetStep to %d", relName, i)
		f.targetStep(i, relName)

		t.Logf("waiting for release %q to achieve waitingForCommand for targetStep %d", relName, i)
		f.waitForReleaseStrategyState("command", relName, i)

		expectedCapacity := replicas.CalculateDesiredReplicaCount(uint(step.Capacity.Contender), float64(targetReplicas))
		t.Logf("checking that release %q has %d pods (strategy step %d aka %q)", relName, expectedCapacity, i, step.Name)
		f.checkReadyPods(relName, int(expectedCapacity))
	}
}

func TestNewApplicationBlockStrategyBackwards(t *testing.T) {
	if !*runEndToEnd {
		t.Skip("skipping end-to-end tests: --e2e is false")
	}

	t.Parallel()
	targetReplicas := 4
	ns, err := setupNamespace(t.Name())
	f := newFixture(ns.GetName(), t)
	if err != nil {
		t.Fatalf("could not create namespace %s: %q", ns.GetName(), err)
	}
	defer func() {
		if *inspectFailed && t.Failed() {
			return
		}
		teardownNamespace(ns.GetName())
	}()

	app := newApplication(ns.GetName(), appName, &vanguard)
	app.Spec.Template.Values = &shipper.ChartValues{"replicaCount": targetReplicas}
	app.Spec.Template.Chart.Name = "test-nginx"
	app.Spec.Template.Chart.Version = "0.0.1"

	_, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Create(app)
	if err != nil {
		t.Fatalf("could not create application %q: %q", appName, err)
	}

	t.Logf("waiting for a new release for new application %q", appName)
	rel := f.waitForRelease(appName, 0)
	relName := rel.GetName()

	var (
		expectedCapacity uint
		step             shipper.RolloutStrategyStep
	)
	for _, i := range []int{0, 1} {
		step = vanguard.Steps[i]
		t.Logf("setting release %q targetStep to %d", relName, i)
		f.targetStep(i, relName)

		t.Logf("waiting for release %q to achieve waitingForCommand for targetStep %d", relName, i)
		f.waitForReleaseStrategyState("command", relName, i)

		expectedCapacity = replicas.CalculateDesiredReplicaCount(uint(step.Capacity.Contender), float64(targetReplicas))
		t.Logf("checking that release %q has %d pods (strategy step %d aka %q)", relName, expectedCapacity, i, step.Name)
		f.checkReadyPods(relName, int(expectedCapacity))
	}

	t.Logf("created a new rollout block object %q", rolloutBlockName)
	_, err = createRolloutBlock(ns.GetName(), rolloutBlockName)
	if err != nil {
		t.Fatalf("could not create rollout block %q: %q", rolloutBlockName, err)
	}

	t.Logf("trying to set release %q targetStep to %d", relName, 0)
	patch := fmt.Sprintf(`{"spec": {"targetStep": %d}}`, 0)
	// Should fail to patch
	_, err = shipperClient.ShipperV1alpha1().Releases(f.namespace).Patch(relName, types.MergePatchType, []byte(patch))
	if err != nil {
		t.Logf("successfully did not patch release with targetStep %v: %q", step, err)
	}

	t.Logf("release %q should stay in waitingForCommand for targetStep %d", relName, 1)
	f.waitForReleaseStrategyState("command", relName, 1)

	t.Logf("checking that release %q still has %d pods (strategy step %d aka %q)", relName, expectedCapacity, 1, step.Name)
	f.checkReadyPods(relName, int(expectedCapacity))
}

func TestRolloutMovingStrategyBackwards(t *testing.T) {
	if !*runEndToEnd {
		t.Skip("skipping end-to-end tests: --e2e is false")
	}

	t.Parallel()
	targetReplicas := 4
	ns, err := setupNamespace(t.Name())
	f := newFixture(ns.GetName(), t)
	if err != nil {
		t.Fatalf("could not create namespace %s: %q", ns.GetName(), err)
	}
	defer func() {
		if *inspectFailed && t.Failed() {
			return
		}
		teardownNamespace(ns.GetName())
	}()

	// start with allIn to jump through the first release
	app := newApplication(ns.GetName(), appName, &allIn)
	app.Spec.Template.Values = &shipper.ChartValues{"replicaCount": targetReplicas}
	app.Spec.Template.Chart.Name = "test-nginx"
	app.Spec.Template.Chart.Version = "0.0.1"

	_, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Create(app)
	if err != nil {
		t.Fatalf("could not create application %q: %q", appName, err)
	}

	incumbent := f.waitForRelease(appName, 0)
	incumbentName := incumbent.GetName()
	f.waitForComplete(incumbentName)
	f.checkReadyPods(incumbentName, targetReplicas)

	// Refetch so that the update has a fresh version to work with.
	app, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Get(app.GetName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("could not refetch app: %q", err)
	}

	app.Spec.Template.Strategy = &vanguard
	app.Spec.Template.Chart.Version = "0.0.2"
	_, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Update(app)
	if err != nil {
		t.Fatalf("could not update application %q: %q", appName, err)
	}

	t.Logf("waiting for contender release to appear after editing app %q", app.GetName())
	contender := f.waitForRelease(appName, 1)
	contenderName := contender.GetName()

	// The strategy emulates deployment all way down to 50/50 and then revert
	// to the previous step (staging).
	for _, i := range []int{0, 1, 0} {
		step := vanguard.Steps[i]
		t.Logf("setting release %q targetStep to %d", contenderName, i)
		f.targetStep(i, contenderName)

		t.Logf("waiting for release %q to achieve waitingForCommand for targetStep %d", contenderName, i)
		f.waitForReleaseStrategyState("command", contenderName, i)

		expectedContenderCapacity := replicas.CalculateDesiredReplicaCount(uint(step.Capacity.Contender), float64(targetReplicas))
		expectedIncumbentCapacity := replicas.CalculateDesiredReplicaCount(uint(step.Capacity.Incumbent), float64(targetReplicas))

		t.Logf(
			"checking that incumbent %q has %d pods and contender %q has %d pods (strategy step %d -- %d/%d)",
			incumbentName, expectedIncumbentCapacity, contenderName, expectedContenderCapacity, i, step.Capacity.Incumbent, step.Capacity.Contender,
		)

		f.checkReadyPods(contenderName, int(expectedContenderCapacity))
		f.checkReadyPods(incumbentName, int(expectedIncumbentCapacity))
	}
}

func TestRolloutBlockMovingStrategyBackwards(t *testing.T) {
	if !*runEndToEnd {
		t.Skip("skipping end-to-end tests: --e2e is false")
	}

	t.Parallel()
	targetReplicas := 4
	ns, err := setupNamespace(t.Name())
	f := newFixture(ns.GetName(), t)
	if err != nil {
		t.Fatalf("could not create namespace %s: %q", ns.GetName(), err)
	}
	defer func() {
		if *inspectFailed && t.Failed() {
			return
		}
		teardownNamespace(ns.GetName())
	}()

	// start with allIn to jump through the first release
	app := newApplication(ns.GetName(), appName, &allIn)
	app.Spec.Template.Values = &shipper.ChartValues{"replicaCount": targetReplicas}
	app.Spec.Template.Chart.Name = "test-nginx"
	app.Spec.Template.Chart.Version = "0.0.1"

	_, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Create(app)
	if err != nil {
		t.Fatalf("could not create application %q: %q", appName, err)
	}

	incumbent := f.waitForRelease(appName, 0)
	incumbentName := incumbent.GetName()
	f.waitForComplete(incumbentName)
	f.checkReadyPods(incumbentName, targetReplicas)

	// Refetch so that the update has a fresh version to work with.
	app, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Get(app.GetName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("could not refetch app: %q", err)
	}

	app.Spec.Template.Strategy = &vanguard
	app.Spec.Template.Chart.Version = "0.0.2"
	_, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Update(app)
	if err != nil {
		t.Fatalf("could not update application %q: %q", appName, err)
	}

	t.Logf("waiting for contender release to appear after editing app %q", app.GetName())
	contender := f.waitForRelease(appName, 1)
	contenderName := contender.GetName()

	// The strategy emulates deployment all way down to 50/50 and then revert
	// to the previous step (staging).
	i := 0
	step := vanguard.Steps[i]
	t.Logf("setting release %q targetStep to %d", contenderName, i)
	f.targetStep(i, contenderName)

	t.Logf("waiting for release %q to achieve waitingForCommand for targetStep %d", contenderName, i)
	f.waitForReleaseStrategyState("command", contenderName, i)

	expectedContenderCapacity := replicas.CalculateDesiredReplicaCount(uint(step.Capacity.Contender), float64(targetReplicas))
	expectedIncumbentCapacity := replicas.CalculateDesiredReplicaCount(uint(step.Capacity.Incumbent), float64(targetReplicas))

	t.Logf(
		"checking that incumbent %q has %d pods and contender %q has %d pods (strategy step %d -- %d/%d)",
		incumbentName, expectedIncumbentCapacity, contenderName, expectedContenderCapacity, i, step.Capacity.Incumbent, step.Capacity.Contender,
	)

	f.checkReadyPods(contenderName, int(expectedContenderCapacity))
	f.checkReadyPods(incumbentName, int(expectedIncumbentCapacity))

	t.Logf("created a new rollout block object %q", rolloutBlockName)
	_, err = createRolloutBlock(ns.GetName(), rolloutBlockName)
	if err != nil {
		t.Fatalf("could not create rollout block %q: %q", rolloutBlockName, err)
	}

	i = 1
	step = vanguard.Steps[i]
	t.Logf("trying to set release %q targetStep to %d", contenderName, i)
	patch := fmt.Sprintf(`{"spec": {"targetStep": %d}}`, i)
	// Should fail to patch
	_, err = shipperClient.ShipperV1alpha1().Releases(f.namespace).Patch(contenderName, types.MergePatchType, []byte(patch))
	if err != nil {
		t.Logf("successfully did not patch release with targetStep %v: %q", step, err)
	}

	t.Logf("release %q should stay in waitingForCommand for targetStep %d", contenderName, 0)
	f.waitForReleaseStrategyState("command", contenderName, 0)

	t.Logf(
		"checking that incumbent %q has %d pods and contender %q has %d pods (strategy step %d -- %d/%d)",
		incumbentName, expectedIncumbentCapacity, contenderName, expectedContenderCapacity, i, step.Capacity.Incumbent, step.Capacity.Contender,
	)

	f.checkReadyPods(contenderName, int(expectedContenderCapacity))
	f.checkReadyPods(incumbentName, int(expectedIncumbentCapacity))
}

// TestNewApplicationAbort emulates a brand new application rollout.
// The rollout strategy includes a few steps, we are creating a new release,
// Next, we are moving 1 step forward (50% of the capacity and 50% of the
// traffic) and delete the release. The expected behavior is:
// * shipper recreates the release object as this is the only release available.
// * the application is being scaled back to step 0.
// * the release is waiting for a command.
func TestNewApplicationAbort(t *testing.T) {
	if !*runEndToEnd {
		t.Skip("skipping end-to-end tests: --e2e is false")
	}

	t.Parallel()
	targetReplicas := 4
	ns, err := setupNamespace(t.Name())
	f := newFixture(ns.GetName(), t)
	if err != nil {
		t.Fatalf("could not create namespace %s: %q", ns.GetName(), err)
	}
	defer func() {
		if *inspectFailed && t.Failed() {
			return
		}
		teardownNamespace(ns.GetName())
	}()

	app := newApplication(ns.GetName(), appName, &vanguard)
	app.Spec.Template.Values = &shipper.ChartValues{"replicaCount": targetReplicas}
	app.Spec.Template.Chart.Name = "test-nginx"
	app.Spec.Template.Chart.Version = "0.0.1"

	_, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Create(app)
	if err != nil {
		t.Fatalf("could not create application %q: %q", appName, err)
	}

	t.Logf("waiting for a new release for new application %q", appName)
	rel := f.waitForRelease(appName, 0)
	relName := rel.GetName()

	for _, i := range []int{0, 1} {
		step := vanguard.Steps[i]
		t.Logf("setting release %q targetStep to %d", relName, i)
		f.targetStep(i, relName)

		t.Logf("waiting for release %q to achieve waitingForCommand for targetStep %d", relName, i)
		f.waitForReleaseStrategyState("command", relName, i)

		expectedCapacity := replicas.CalculateDesiredReplicaCount(uint(step.Capacity.Contender), float64(targetReplicas))
		t.Logf("checking that release %q has %d pods (strategy step %d aka %q)", relName, expectedCapacity, i, step.Name)
		f.checkReadyPods(relName, int(expectedCapacity))
	}

	t.Logf("Preparing to remove the release %q", relName)

	err = shipperClient.ShipperV1alpha1().Releases(ns.GetName()).Delete(relName, &metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("failed to delete release %q", relName)
	}

	// Now the release should be waiting for a command
	f.waitForReleaseStrategyState("command", relName, 0)

	// It's back to step 0, let's check the number of pods
	expectedCapacity := replicas.CalculateDesiredReplicaCount(uint(vanguard.Steps[0].Capacity.Contender), float64(targetReplicas))
	f.checkReadyPods(relName, int(expectedCapacity))
}

func TestRolloutAbort(t *testing.T) {
	if !*runEndToEnd {
		t.Skip("skipping end-to-end tests: --e2e is false")
	}

	t.Parallel()
	targetReplicas := 4
	ns, err := setupNamespace(t.Name())
	f := newFixture(ns.GetName(), t)
	if err != nil {
		t.Fatalf("could not create namespace %s: %q", ns.GetName(), err)
	}
	defer func() {
		if *inspectFailed && t.Failed() {
			return
		}
		teardownNamespace(ns.GetName())
	}()

	// start with allIn to jump through the first release
	app := newApplication(ns.GetName(), appName, &allIn)
	app.Spec.Template.Values = &shipper.ChartValues{"replicaCount": targetReplicas}
	app.Spec.Template.Chart.Name = "test-nginx"
	app.Spec.Template.Chart.Version = "0.0.1"

	_, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Create(app)
	if err != nil {
		t.Fatalf("could not create application %q: %q", appName, err)
	}

	incumbent := f.waitForRelease(appName, 0)
	incumbentName := incumbent.GetName()
	f.waitForComplete(incumbentName)
	f.checkReadyPods(incumbentName, targetReplicas)

	// Refetch so that the update has a fresh version to work with.
	app, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Get(app.GetName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("could not refetch app: %q", err)
	}

	app.Spec.Template.Strategy = &vanguard
	app.Spec.Template.Chart.Version = "0.0.2"
	_, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Update(app)
	if err != nil {
		t.Fatalf("could not update application %q: %q", appName, err)
	}

	t.Logf("waiting for contender release to appear after editing app %q", app.GetName())
	contender := f.waitForRelease(appName, 1)
	contenderName := contender.GetName()

	// The strategy emulates deployment all way down to 50/50 (steps 0 and 1)
	for _, i := range []int{0, 1} {
		step := vanguard.Steps[i]
		t.Logf("setting contender release %q targetStep to %d", contenderName, i)
		f.targetStep(i, contenderName)

		t.Logf("waiting for contender release %q to achieve waitingForCommand for targetStep %d", contenderName, i)
		f.waitForReleaseStrategyState("command", contenderName, i)

		expectedContenderCapacity := replicas.CalculateDesiredReplicaCount(uint(step.Capacity.Contender), float64(targetReplicas))
		expectedIncumbentCapacity := replicas.CalculateDesiredReplicaCount(uint(step.Capacity.Incumbent), float64(targetReplicas))

		t.Logf(
			"checking that incumbent %q has %d pods and contender %q has %d pods (strategy step %d -- %d/%d)",
			incumbentName, expectedIncumbentCapacity, contenderName, expectedContenderCapacity, i, step.Capacity.Incumbent, step.Capacity.Contender,
		)

		f.checkReadyPods(contenderName, int(expectedContenderCapacity))
		f.checkReadyPods(incumbentName, int(expectedIncumbentCapacity))
	}

	err = shipperClient.ShipperV1alpha1().Releases(ns.GetName()).Delete(contenderName, &metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("failed to remove the old release %q: %q", contenderName, err)
	}

	// The test emulates an interruption in the middle of the rollout, which
	// means the incumbent becomes a new contender and it will stay in 50%
	// capacity state (step 1 according to the vanguard definition) for a bit
	// until shipper detects the need for capacity and spins up the missing
	// pods
	f.waitForReleaseStrategyState("capacity", incumbentName, 0)

	// Once the need for capacity triggers, the test waits for all-clear state
	// (all 4 strategy states indicate no demand).
	f.waitForReleaseStrategyState("none", incumbentName, 0)

	// By this moment shipper is expected to have recovered the missing capacity
	// and get all pods up and running
	expectedCapacity := replicas.CalculateDesiredReplicaCount(uint(allIn.Steps[0].Capacity.Contender), float64(targetReplicas))
	f.checkReadyPods(incumbentName, int(expectedCapacity))
}

func TestNewRolloutBlockAddOverrides(t *testing.T) {
	if !*runEndToEnd {
		t.Skip("skipping end-to-end tests: --e2e is false")
	}
	t.Parallel()

	targetReplicas := 1
	ns, err := setupNamespace(t.Name())
	namespace := ns.GetName()
	f := newFixture(namespace, t)
	if err != nil {
		t.Fatalf("could not create namespace %s: %q", namespace, err)
	}
	defer func() {
		if *inspectFailed && t.Failed() {
			return
		}
		teardownNamespace(namespace)
	}()

	_, err = createRolloutBlock(namespace, rolloutBlockName)
	if err != nil {
		t.Fatalf("could not create rollout block %q: %q", rolloutBlockName, err)
	}

	rb, err := shipperClient.ShipperV1alpha1().RolloutBlocks(namespace).Get(rolloutBlockName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("could not refetch rollout block: %q", err)
	}

	if len(rb.Status.Overrides.Application) > 0 || len(rb.Status.Overrides.Release) > 0 {
		t.Fatalf("rollout block has unexpected overrides: %v", rb)
	}

	newApp := newApplication(namespace, appName, &allIn)
	newApp.Annotations[shipper.RolloutBlocksOverrideAnnotation] = fmt.Sprintf("%s/%s", rb.GetNamespace(), rb.GetName())
	newApp.Spec.Template.Values = &shipper.ChartValues{"replicaCount": targetReplicas}
	newApp.Spec.Template.Chart.Name = "test-nginx"
	newApp.Spec.Template.Chart.Version = "0.0.1"

	_, err = shipperClient.ShipperV1alpha1().Applications(namespace).Create(newApp)
	if err != nil {
		t.Fatalf("could not create application %q: %q", appName, err)
	}

	t.Logf("waiting for a new release for new application %q", appName)
	rel := f.waitForRelease(appName, 0)
	relName := rel.GetName()
	t.Logf("waiting for release %q to complete", relName)
	f.waitForComplete(rel.GetName())
	t.Logf("checking that release %q has %d pods (strategy step 0 -- finished)", relName, targetReplicas)
	f.checkReadyPods(relName, targetReplicas)

	t.Logf("waiting for rollout block %q/%q status to be updated ", namespace, rolloutBlockName)
	f.waitForStatus(
		rolloutBlockName,
		namespace,
		fmt.Sprintf("%s/%s", newApp.GetNamespace(), newApp.GetName()),
		fmt.Sprintf("%s/%s", newApp.GetNamespace(), relName),
	)
}

func TestNewGlobalRolloutBlockAddOverrides(t *testing.T) {
	if !*runEndToEnd {
		t.Skip("skipping end-to-end tests: --e2e is false")
	}

	targetReplicas := 1
	ns, err := setupNamespace(t.Name())
	f := newFixture(ns.GetName(), t)
	if err != nil {
		t.Fatalf("could not create namespace %s: %q", ns.GetName(), err)
	}
	defer func() {
		if *inspectFailed && t.Failed() {
			return
		}
		teardownNamespace(ns.GetName())
	}()

	_, err = createRolloutBlock(shipper.GlobalRolloutBlockNamespace, rolloutBlockName)
	if err != nil {
		t.Fatalf("could not create rollout block %q: %q", rolloutBlockName, err)
	}

	rb, err := shipperClient.ShipperV1alpha1().RolloutBlocks(shipper.GlobalRolloutBlockNamespace).Get(rolloutBlockName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("could not refetch rollout block: %q", err)
	}
	defer func() {
		err = shipperClient.ShipperV1alpha1().RolloutBlocks(rb.GetNamespace()).Delete(rb.GetName(), &metav1.DeleteOptions{})
		if err != nil {
			t.Fatalf("could not DELETE rollout block %q: %q", rolloutBlockName, err)
		}
	}()

	if len(rb.Status.Overrides.Application) > 0 || len(rb.Status.Overrides.Release) > 0 {
		t.Fatalf("rollout block has unexpected overrides: %v", rb)
	}

	newApp := newApplication(ns.GetName(), appName, &allIn)
	newApp.Annotations[shipper.RolloutBlocksOverrideAnnotation] = fmt.Sprintf("%s/%s", rb.GetNamespace(), rb.GetName())
	newApp.Spec.Template.Values = &shipper.ChartValues{"replicaCount": targetReplicas}
	newApp.Spec.Template.Chart.Name = "test-nginx"
	newApp.Spec.Template.Chart.Version = "0.0.1"

	_, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Create(newApp)
	if err != nil {
		t.Fatalf("could not create application %q: %q", appName, err)
	}

	t.Logf("waiting for a new release for new application %q", appName)
	rel := f.waitForRelease(appName, 0)
	relName := rel.GetName()
	t.Logf("waiting for release %q to complete", relName)
	f.waitForComplete(rel.GetName())
	t.Logf("checking that release %q has %d pods (strategy step 0 -- finished)", relName, targetReplicas)
	f.checkReadyPods(relName, targetReplicas)

	t.Logf("waiting for rollout block %q/%q status to be updated ", rb.GetNamespace(), rolloutBlockName)
	f.waitForStatus(
		rolloutBlockName,
		rb.GetNamespace(),
		fmt.Sprintf("%s/%s", newApp.GetNamespace(), newApp.GetName()),
		fmt.Sprintf("%s/%s", newApp.GetNamespace(), relName),
	)
}

func TestNewRolloutBlockRemoveRelease(t *testing.T) {
	if !*runEndToEnd {
		t.Skip("skipping end-to-end tests: --e2e is false")
	}
	t.Parallel()

	targetReplicas := 1
	ns, err := setupNamespace(t.Name())
	f := newFixture(ns.GetName(), t)
	if err != nil {
		t.Fatalf("could not create namespace %s: %q", ns.GetName(), err)
	}
	defer func() {
		if *inspectFailed && t.Failed() {
			return
		}
		teardownNamespace(ns.GetName())
	}()

	_, err = createRolloutBlock(ns.GetName(), rolloutBlockName)
	if err != nil {
		t.Fatalf("could not create rollout block %q: %q", rolloutBlockName, err)
	}

	rb, err := shipperClient.ShipperV1alpha1().RolloutBlocks(ns.GetName()).Get(rolloutBlockName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("could not refetch rollout block: %q", err)
	}

	if len(rb.Status.Overrides.Application) > 0 || len(rb.Status.Overrides.Release) > 0 {
		t.Fatalf("rollout block has unexpected overrides: %v", rb)
	}

	app := newApplication(ns.GetName(), appName, &allIn)
	app.Annotations[shipper.RolloutBlocksOverrideAnnotation] = fmt.Sprintf("%s/%s", rb.GetNamespace(), rb.GetName())
	app.Spec.Template.Values = &shipper.ChartValues{"replicaCount": targetReplicas}
	app.Spec.Template.Chart.Name = "test-nginx"
	app.Spec.Template.Chart.Version = "0.0.1"

	_, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Create(app)
	if err != nil {
		t.Fatalf("could not create application %q: %q", appName, err)
	}

	t.Logf("waiting for a new release for new application %q", appName)
	rel := f.waitForRelease(appName, 0)
	relName := rel.GetName()
	t.Logf("waiting for release %q to complete", relName)
	f.waitForComplete(rel.GetName())
	t.Logf("checking that release %q has %d pods (strategy step 0 -- finished)", relName, targetReplicas)
	f.checkReadyPods(relName, targetReplicas)

	t.Logf("waiting for rollout block %q/%q status to be updated ", rb.GetNamespace(), rolloutBlockName)
	f.waitForStatus(
		rolloutBlockName,
		ns.GetName(),
		fmt.Sprintf("%s/%s", app.GetNamespace(), app.GetName()),
		fmt.Sprintf("%s/%s", rel.GetNamespace(), rel.GetName()),
	)

	// refetch so that the update has a fresh version to work with
	rel, err = shipperClient.ShipperV1alpha1().Releases(ns.GetName()).Get(rel.GetName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("could not refetch release: %q", err)
	}

	rel.Annotations[shipper.RolloutBlocksOverrideAnnotation] = ""
	_, err = shipperClient.ShipperV1alpha1().Releases(ns.GetName()).Update(rel)
	if err != nil {
		t.Fatalf("could not update release %q: %q", relName, err)
	}

	t.Logf("waiting for rollout block status %q to be updated ", rolloutBlockName)
	f.waitForStatus(
		rolloutBlockName,
		ns.GetName(),
		fmt.Sprintf("%s/%s", app.GetNamespace(), app.GetName()),
		"",
	)
}

func TestNewGlobalRolloutBlockRemoveRelease(t *testing.T) {
	if !*runEndToEnd {
		t.Skip("skipping end-to-end tests: --e2e is false")
	}

	targetReplicas := 1
	ns, err := setupNamespace(t.Name())
	testNamespace := ns.GetName()
	f := newFixture(testNamespace, t)
	if err != nil {
		t.Fatalf("could not create namespace %s: %q", testNamespace, err)
	}
	defer func() {
		if *inspectFailed && t.Failed() {
			return
		}
		teardownNamespace(testNamespace)
	}()

	globalNamespace := shipper.GlobalRolloutBlockNamespace
	_, err = createRolloutBlock(globalNamespace, rolloutBlockName)
	if err != nil {
		t.Fatalf("could not create rollout block %q: %q", rolloutBlockName, err)
	}

	rb, err := shipperClient.ShipperV1alpha1().RolloutBlocks(globalNamespace).Get(rolloutBlockName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("could not refetch rollout block: %q", err)
	}

	defer func() {
		err = shipperClient.ShipperV1alpha1().RolloutBlocks(rb.GetNamespace()).Delete(rb.GetName(), &metav1.DeleteOptions{})
		if err != nil {
			t.Fatalf("could not DELETE rollout block %q: %q", rolloutBlockName, err)
		}
	}()

	if len(rb.Status.Overrides.Application) > 0 || len(rb.Status.Overrides.Release) > 0 {
		t.Fatalf("rollout block has unexpected overrides: %v", rb)
	}

	app := newApplication(testNamespace, appName, &allIn)
	app.Annotations[shipper.RolloutBlocksOverrideAnnotation] = fmt.Sprintf("%s/%s", rb.GetNamespace(), rb.GetName())
	app.Spec.Template.Values = &shipper.ChartValues{"replicaCount": targetReplicas}
	app.Spec.Template.Chart.Name = "test-nginx"
	app.Spec.Template.Chart.Version = "0.0.1"

	_, err = shipperClient.ShipperV1alpha1().Applications(testNamespace).Create(app)
	if err != nil {
		t.Fatalf("could not create application %q: %q", appName, err)
	}

	t.Logf("waiting for a new release for new application %q", appName)
	rel := f.waitForRelease(appName, 0)
	relName := rel.GetName()
	t.Logf("waiting for release %q to complete", relName)
	f.waitForComplete(rel.GetName())
	t.Logf("checking that release %q has %d pods (strategy step 0 -- finished)", relName, targetReplicas)
	f.checkReadyPods(relName, targetReplicas)

	t.Logf("waiting for rollout block %q/%q status to be updated ", rb.GetNamespace(), rolloutBlockName)
	f.waitForStatus(
		rolloutBlockName,
		globalNamespace,
		fmt.Sprintf("%s/%s", app.GetNamespace(), app.GetName()),
		fmt.Sprintf("%s/%s", rel.GetNamespace(), rel.GetName()),
	)

	// refetch so that the update has a fresh version to work with
	rel, err = shipperClient.ShipperV1alpha1().Releases(testNamespace).Get(rel.GetName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("could not refetch release: %q", err)
	}

	rel.Annotations[shipper.RolloutBlocksOverrideAnnotation] = ""
	_, err = shipperClient.ShipperV1alpha1().Releases(testNamespace).Update(rel)
	if err != nil {
		t.Fatalf("could not update release %q: %q", relName, err)
	}

	t.Logf("waiting for rollout block %q/%q status to be updated ", rb.GetNamespace(), rolloutBlockName)
	f.waitForStatus(
		rolloutBlockName,
		globalNamespace,
		fmt.Sprintf("%s/%s", app.GetNamespace(), app.GetName()),
		"",
	)
}

// TestDeletedDeploymentsAreReinstalled test that shipper reinstalls the relevant deployments if they're deleted.
func TestDeletedDeploymentsAreReinstalled(t *testing.T) {
	if !*runEndToEnd {
		t.Skip("skipping end-to-end tests: --e2e is false")
	}
	t.Parallel()

	targetReplicas := 4
	ns, err := setupNamespace(t.Name())
	f := newFixture(ns.GetName(), t)
	if err != nil {
		t.Fatalf("could not create namespace %s: %q", ns.GetName(), err)
	}
	defer func() {
		if *inspectFailed && t.Failed() {
			return
		}
		teardownNamespace(ns.GetName())
	}()

	newApp := newApplication(ns.GetName(), appName, &allIn)
	newApp.Spec.Template.Values = &shipper.ChartValues{"replicaCount": targetReplicas}
	newApp.Spec.Template.Chart.Name = "test-nginx"
	newApp.Spec.Template.Chart.Version = "0.0.1"

	_, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Create(newApp)
	if err != nil {
		t.Fatalf("could not create application %q: %q", appName, err)
	}

	t.Logf("waiting for a new release for new application %q", appName)
	rel := f.waitForRelease(appName, 0)
	relName := rel.GetName()
	t.Logf("waiting for release %q to complete", relName)
	f.waitForComplete(rel.GetName())

	deploymentName := fmt.Sprintf("%s-%s", rel.GetName(), newApp.Spec.Template.Chart.Name)
	t.Logf("deleting deployment %q", deploymentName)
	err = kubeClient.AppsV1().Deployments(ns.GetName()).Delete(deploymentName, nil)
	if err != nil {
		t.Fatalf("could not delete deployment %q: %q", deploymentName, err)
	}

	f.waitForReleaseStrategyState("capacity", rel.GetName(), 0)

	t.Logf("waiting for release %q to complete again", relName)
	f.waitForReleaseStrategyState("none", rel.GetName(), 0)
	t.Logf("checking that release %q has %d pods (strategy step 0 -- finished)", relName, targetReplicas)
	f.checkReadyPods(relName, targetReplicas)
}

func TestConsistentTrafficBalanceOnStraightFullOn(t *testing.T) {
	if !*runEndToEnd {
		t.Skip("skipping end-to-end tests: --e2e is false")
	}
	t.Parallel()

	ns, err := setupNamespace(t.Name())
	f := newFixture(ns.GetName(), t)
	if err != nil {
		t.Fatalf("could not create namespace %s: %q", ns.GetName(), err)
	}
	defer func() {
		if *inspectFailed && t.Failed() {
			return
		}
		teardownNamespace(ns.GetName())
	}()

	app := newApplication(ns.GetName(), appName, &allIn)
	app.Spec.Template.Chart.Name = "test-nginx"
	app.Spec.Template.Chart.Version = "0.0.1"
	app.Spec.Template.Values = &shipper.ChartValues{"replicaCount": 0}

	_, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Create(app)
	if err != nil {
		t.Fatalf("could not create application %q: %q", appName, err)
	}

	t.Logf("waiting for a new release for new application %q", appName)
	incumbent := f.waitForRelease(appName, 0)
	incumbentName := incumbent.GetName()
	t.Logf("waiting for incumbent release %q to complete", incumbentName)
	f.waitForComplete(incumbent.GetName())
	t.Logf("checking that incumbent release %q has %d pods (strategy step 0 -- finished)", incumbentName, 0)
	f.checkReadyPods(incumbentName, 0)

	app, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Get(app.GetName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("could not fetch application %q: %q", appName, err)
	}

	app.Spec.Template.Strategy = &vanguard
	app.Spec.Template.Values = &shipper.ChartValues{"replicaCount": 2}
	if _, err := shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Update(app); err != nil {
		t.Fatalf("could not update app %q: %q", app.GetName(), err)
	}

	contender := f.waitForRelease(appName, 1)
	contenderName := contender.GetName()
	f.targetStep(2, contenderName)
	f.waitForComplete(contender.GetName())

	pods := f.checkReadyPods(contenderName, 2)
	f.checkEndpointActivePods("test-nginx-prod", pods)
}

func TestMultipleAppsInNamespace(t *testing.T) {
	if !*runEndToEnd {
		t.Skip("skipping end-to-end tests: --e2e is false")
	}
	t.Parallel()

	targetReplicas := 1
	ns, err := setupNamespace(t.Name())
	f := newFixture(ns.GetName(), t)
	if err != nil {
		t.Fatalf("could not create namespace %s: %q", ns.GetName(), err)
	}
	defer func() {
		if *inspectFailed && t.Failed() {
			return
		}
		teardownNamespace(ns.GetName())
	}()

	appNames := []string{
		fmt.Sprintf("%s-1", appName),
		fmt.Sprintf("%s-2", appName),
	}

	for _, appName := range appNames {
		newApp := newApplication(ns.GetName(), appName, &allIn)
		newApp.Spec.Template.Values = &shipper.ChartValues{
			"replicaCount": targetReplicas,
			"nameOverride": appName,
		}
		newApp.Spec.Template.Chart.Name = "test-nginx"
		newApp.Spec.Template.Chart.Version = "0.0.1"

		_, err = shipperClient.ShipperV1alpha1().Applications(ns.GetName()).Create(newApp)
		if err != nil {
			t.Fatalf("could not create application %q: %q", appName, err)
		}
	}

	for _, appName := range appNames {
		t.Logf("waiting for a new release for new application %q", appName)
		rel := f.waitForRelease(appName, 0)

		relName := rel.GetName()
		t.Logf("waiting for release %q to complete", relName)

		f.waitForComplete(rel.GetName())
		t.Logf("checking that release %q has %d pods (strategy step 0 -- finished)", relName, targetReplicas)

		pods := f.checkReadyPods(relName, targetReplicas)

		svcName := fmt.Sprintf("%s-prod", appName)
		f.checkEndpointActivePods(svcName, pods)
	}
}

// TODO(btyler): cover a variety of broken chart cases as soon as we report
// those outcomes somewhere other than stderr.

/*
func TestInvalidChartApp(t *testing.T) { }
func TestBadChartUrl(t *testing.T) { }
*/
