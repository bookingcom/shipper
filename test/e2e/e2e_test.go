package e2e

import (
	"flag"
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/helm/pkg/repo/repotest"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

const (
	appName                      = "my-test-app"
	regionFromMinikubePerlScript = "eu-west"
)

var (
	masterURL = flag.String(
		"master",
		"",
		"The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.",
	)
	runEndToEnd   = flag.Bool("e2e", false, "Set this flag to enable E2E tests against the local minikube")
	testCharts    = flag.String("testcharts", "testdata/*.tgz", "Glob expression (escape the *!) pointing to the charts for the test")
	inspectFailed = flag.Bool("inspectfailed", false, "Set this flag to skip deleting the namespaces for failed tests. Useful for debugging.")
	kubeconfig    = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	targetCluster = flag.String("targetcluster", "minikube", "The application cluster that E2E tests will check to determine success/failure")
	timeoutFlag   = flag.String("progresstimeout", "30s", "timeout when waiting for things to change")
)

var (
	targetKubeClient kubernetes.Interface
	kubeClient       kubernetes.Interface
	shipperClient    shipperclientset.Interface
	chartRepo        string
	globalTimeout    time.Duration
)

var allIn = shipperv1.RolloutStrategy{
	Steps: []shipperv1.RolloutStrategyStep{
		{
			Name:     "full on",
			Capacity: shipperv1.RolloutStrategyStepValue{Incumbent: 0, Contender: 100},
			Traffic:  shipperv1.RolloutStrategyStepValue{Incumbent: 0, Contender: 100},
		},
	},
}

var vanguard = shipperv1.RolloutStrategy{
	Steps: []shipperv1.RolloutStrategyStep{
		{
			Name:     "staging",
			Capacity: shipperv1.RolloutStrategyStepValue{Incumbent: 100, Contender: 1},
			Traffic:  shipperv1.RolloutStrategyStepValue{Incumbent: 100, Contender: 0},
		},
		{
			Name:     "50/50",
			Capacity: shipperv1.RolloutStrategyStepValue{Incumbent: 50, Contender: 50},
			Traffic:  shipperv1.RolloutStrategyStepValue{Incumbent: 50, Contender: 50},
		},
		{
			Name:     "full on",
			Capacity: shipperv1.RolloutStrategyStepValue{Incumbent: 0, Contender: 100},
			Traffic:  shipperv1.RolloutStrategyStepValue{Incumbent: 0, Contender: 100},
		},
	},
}

func TestMain(m *testing.M) {
	flag.Parse()
	var err error
	if *runEndToEnd {
		globalTimeout, err = time.ParseDuration(*timeoutFlag)
		if err != nil {
			glog.Fatalf("could not parse given timeout duration: %q", err)
		}

		kubeClient, shipperClient, err = buildManagementClients(*masterURL, *kubeconfig)
		if err != nil {
			glog.Fatalf("could not build a client: %v", err)
		}

		targetKubeClient = buildTargetClient(*targetCluster)
		purgeTestNamespaces()
	}

	srv, hh, err := repotest.NewTempServer(*testCharts)
	if err != nil {
		glog.Fatalf("failed to start helm repo server: %v", err)
	}

	chartRepo = srv.URL()
	glog.Infof("serving test charts on %q", chartRepo)

	exitCode := m.Run()

	os.RemoveAll(hh.String())
	srv.Stop()

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
	newApp.Spec.Template.Values = &shipperv1.ChartValues{"replicaCount": targetReplicas}
	newApp.Spec.Template.Chart.Name = "test-nginx"
	newApp.Spec.Template.Chart.Version = "0.0.1"

	_, err = shipperClient.ShipperV1().Applications(ns.GetName()).Create(newApp)
	if err != nil {
		t.Fatalf("could not create application %q: %q", appName, err)
	}

	t.Logf("waiting for a new release for new application %q", appName)
	rel := f.waitForRelease(appName, 0)
	relName := rel.GetName()
	t.Logf("waiting for release %q to complete", relName)
	f.waitForComplete(rel.GetName())
	t.Logf("checking that release %q has %d pods (strategy step 0 -- finished)", relName, targetReplicas)
	f.checkPods(relName, targetReplicas)
	err = shipperClient.ShipperV1().Applications(ns.GetName()).Delete(newApp.GetName(), &metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("could not DELETE application %q: %q", appName, err)
	}
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
	app.Spec.Template.Values = &shipperv1.ChartValues{"replicaCount": targetReplicas}
	app.Spec.Template.Chart.Name = "test-nginx"
	app.Spec.Template.Chart.Version = "0.0.1"

	_, err = shipperClient.ShipperV1().Applications(ns.GetName()).Create(app)
	if err != nil {
		t.Fatalf("could not create application %q: %q", appName, err)
	}

	t.Logf("waiting for a new release for new application %q", appName)
	rel := f.waitForRelease(appName, 0)
	relName := rel.GetName()
	t.Logf("waiting for release %q to complete", relName)
	f.waitForComplete(rel.GetName())
	t.Logf("checking that release %q has %d pods (strategy step 0 -- finished)", relName, targetReplicas)
	f.checkPods(relName, targetReplicas)

	// refetch so that the update has a fresh version to work with
	app, err = shipperClient.ShipperV1().Applications(ns.GetName()).Get(app.GetName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("could not refetch app: %q", err)
	}

	app.Spec.Template.Chart.Version = "0.0.2"
	_, err = shipperClient.ShipperV1().Applications(ns.GetName()).Update(app)
	if err != nil {
		t.Fatalf("could not update application %q: %q", appName, err)
	}

	t.Logf("waiting for contender release to appear after editing app %q", app.GetName())
	contender := f.waitForRelease(appName, 1)
	t.Logf("waiting for contender %q to complete", contender.GetName())
	f.waitForComplete(contender.GetName())
	t.Logf("checking that release %q has %d pods (strategy step 0 -- finished)", contender.GetName(), targetReplicas)
	f.checkPods(contender.GetName(), targetReplicas)
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
	newApp.Spec.Template.Values = &shipperv1.ChartValues{"replicaCount": targetReplicas}
	newApp.Spec.Template.Chart.Name = "test-nginx"
	newApp.Spec.Template.Chart.Version = "0.0.1"

	_, err = shipperClient.ShipperV1().Applications(ns.GetName()).Create(newApp)
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
			f.waitForCommand(relName, i)
		}

		expectedCapacity := capacityInPods(step.Capacity.Contender, targetReplicas)
		t.Logf("checking that release %q has %d pods (strategy step %d aka %q)", relName, expectedCapacity, i, step.Name)
		f.checkPods(relName, expectedCapacity)
	}
}

func TestNewApplicationVanguardMultipleReplicas(t *testing.T) {
	testNewApplicationVanguard(4, t)
}

func TestNewApplicationVanguardOneReplica(t *testing.T) {
	testNewApplicationVanguard(1, t)
}

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
	app.Spec.Template.Values = &shipperv1.ChartValues{"replicaCount": targetReplicas}
	app.Spec.Template.Chart.Name = "test-nginx"
	app.Spec.Template.Chart.Version = "0.0.1"

	_, err = shipperClient.ShipperV1().Applications(ns.GetName()).Create(app)
	if err != nil {
		t.Fatalf("could not create application %q: %q", appName, err)
	}

	incumbent := f.waitForRelease(appName, 0)
	incumbentName := incumbent.GetName()
	f.waitForComplete(incumbentName)
	f.checkPods(incumbentName, targetReplicas)

	// Refetch so that the update has a fresh version to work with.
	app, err = shipperClient.ShipperV1().Applications(ns.GetName()).Get(app.GetName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("could not refetch app: %q", err)
	}

	app.Spec.Template.Strategy = &vanguard
	app.Spec.Template.Chart.Version = "0.0.2"
	_, err = shipperClient.ShipperV1().Applications(ns.GetName()).Update(app)
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
			f.waitForCommand(contenderName, i)
		}

		expectedContenderCapacity := capacityInPods(step.Capacity.Contender, targetReplicas)
		expectedIncumbentCapacity := capacityInPods(step.Capacity.Incumbent, targetReplicas)

		t.Logf(
			"checking that incumbent %q has %d pods and contender %q has %d pods (strategy step %d -- %d/%d)",
			incumbentName, expectedIncumbentCapacity, contenderName, expectedContenderCapacity, i, step.Capacity.Incumbent, step.Capacity.Contender,
		)

		f.checkPods(contenderName, expectedContenderCapacity)
		f.checkPods(incumbentName, expectedIncumbentCapacity)
	}
}

func TestRolloutVanguardMultipleReplicas(t *testing.T) {
	testRolloutVanguard(4, t)
}

func TestRolloutVanguardOneReplica(t *testing.T) {
	testRolloutVanguard(1, t)
}

// TODO(btyler): cover a variety of broken chart cases as soon as we report
// those outcomes somewhere other than stderr.

/*
func TestInvalidChartApp(t *testing.T) { }
func TestBadChartUrl(t *testing.T) { }
*/

type fixture struct {
	t         *testing.T
	namespace string
}

func newFixture(ns string, t *testing.T) *fixture {
	return &fixture{
		t:         t,
		namespace: ns,
	}
}

func (f *fixture) targetStep(step int, relName string) {
	patch := fmt.Sprintf(`{"spec": {"targetStep": %d}}`, step)
	_, err := shipperClient.ShipperV1().Releases(f.namespace).Patch(relName, types.MergePatchType, []byte(patch))
	if err != nil {
		f.t.Fatalf("could not patch release with targetStep %v: %q", step, err)
	}
}

func (f *fixture) checkPods(relName string, expectedCount int) {
	selector := labels.Set{shipperv1.ReleaseLabel: relName}.AsSelector()
	podList, err := targetKubeClient.CoreV1().Pods(f.namespace).List(metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		f.t.Fatalf("could not list pods %q: %q", f.namespace, err)
	}

	readyCount := 0
	for _, pod := range podList.Items {
		for _, condition := range pod.Status.Conditions {
			// This line imitates how ReplicaSets calculate 'ready replicas'; Shipper
			// uses 'availableReplicas' but the minReadySeconds in this test is 0.
			// There's no handy library for this because the functionality is split
			// between k8s' controller_util.go and api v1 podUtil.
			if condition.Type == "Ready" && condition.Status == "True" && pod.DeletionTimestamp == nil {
				readyCount++
			}
		}
	}

	if readyCount != expectedCount {
		f.t.Fatalf("checking pods on release %q: expected %d but got %d", relName, expectedCount, readyCount)
	}
}

func (f *fixture) waitForRelease(appName string, historyIndex int) *shipperv1.Release {
	var newRelease *shipperv1.Release
	start := time.Now()
	// Not logging because we poll pretty fast and that'd be a bunch of garbage to
	// read through.
	var state string
	err := poll(globalTimeout, func() (bool, error) {
		app, err := shipperClient.ShipperV1().Applications(f.namespace).Get(appName, metav1.GetOptions{})
		if err != nil {
			f.t.Fatalf("failed to fetch app: %q", appName)
		}

		if len(app.Status.History) != historyIndex+1 {
			state = fmt.Sprintf("wrong number of entries in history: expected %v but got %v", historyIndex+1, len(app.Status.History))
			return false, nil
		}

		relName := app.Status.History[historyIndex]
		rel, err := shipperClient.ShipperV1().Releases(f.namespace).Get(relName, metav1.GetOptions{})
		if err != nil {
			f.t.Fatalf("release which was in app history was not fetched: %q: %q", relName, err)
		}

		newRelease = rel
		return true, nil
	})

	if err != nil {
		if err == wait.ErrWaitTimeout {
			f.t.Fatalf("timed out waiting for release to be scheduled (waited %s). final state %q", globalTimeout, state)
		}
		f.t.Fatalf("error waiting for release to be scheduled: %q", err)
	}

	f.t.Logf("waiting for release took %s", time.Since(start))
	return newRelease
}

func (f *fixture) waitForCommand(releaseName string, step int) {
	var state string
	start := time.Now()
	err := poll(globalTimeout, func() (bool, error) {
		rel, err := shipperClient.ShipperV1().Releases(f.namespace).Get(releaseName, metav1.GetOptions{})
		if err != nil {
			f.t.Fatalf("failed to fetch release: %q", releaseName)
		}

		if rel.Status.Strategy == nil {
			state = fmt.Sprintf("release %q has no strategy status reported yet", releaseName)
			return false, nil
		}

		if rel.Status.AchievedStep == nil {
			state = fmt.Sprintf("release %q has no achievedStep reported yet", releaseName)
			return false, nil
		}

		if rel.Status.Strategy.State.WaitingForCommand == shipperv1.StrategyStateTrue &&
			rel.Status.AchievedStep.Step == int32(step) {
			return true, nil
		}

		state = fmt.Sprintf("release %q status strategy state WaitingForCommand is false", releaseName)
		return false, nil
	})

	if err != nil {
		if err == wait.ErrWaitTimeout {
			f.t.Fatalf("timed out waiting for release to be 'waitingForCommand': waited %s. final state: %s", globalTimeout, state)
		}
		f.t.Fatalf("error waiting for release to be 'waitingForCommand': %q", err)
	}

	f.t.Logf("waiting for command took %s", time.Since(start))
}

func (f *fixture) waitForComplete(releaseName string) {
	start := time.Now()
	err := poll(globalTimeout, func() (bool, error) {
		rel, err := shipperClient.ShipperV1().Releases(f.namespace).Get(releaseName, metav1.GetOptions{})
		if err != nil {
			f.t.Fatalf("failed to fetch release: %q", releaseName)
		}

		if releaseutil.ReleaseComplete(rel) {
			return true, nil
		}

		return false, nil
	})

	if err != nil {
		f.t.Fatalf("error waiting for release to complete: %q", err)
	}
	f.t.Logf("waiting for completion took %s", time.Since(start))
}

func poll(timeout time.Duration, waitCondition func() (bool, error)) error {
	return wait.PollUntil(
		25*time.Millisecond,
		waitCondition,
		stopAfter(timeout),
	)
}

func setupNamespace(name string) (*corev1.Namespace, error) {
	newNs := testNamespace(name)
	return kubeClient.CoreV1().Namespaces().Create(newNs)
}

func teardownNamespace(name string) {
	err := kubeClient.CoreV1().Namespaces().Delete(name, &metav1.DeleteOptions{})
	if err != nil {
		glog.Fatalf("failed to clean up namespace %q: %q", name, err)
	}
}

func purgeTestNamespaces() {
	req, err := labels.NewRequirement(
		shippertesting.TestLabel,
		selection.Exists,
		[]string{},
	)

	if err != nil {
		panic("a static label for deleting namespaces failed to parse. please fix the tests")
	}

	selector := labels.NewSelector().Add(*req)
	listOptions := metav1.ListOptions{LabelSelector: selector.String()}

	list, err := kubeClient.CoreV1().Namespaces().List(listOptions)
	if err != nil {
		glog.Fatalf("failed to list namespaces: %q", err)
	}

	if len(list.Items) == 0 {
		return
	}

	for _, namespace := range list.Items {
		err = kubeClient.CoreV1().Namespaces().Delete(namespace.GetName(), &metav1.DeleteOptions{})
		if err != nil {
			if errors.IsConflict(err) {
				// this means the namespace is still cleaning up from some other delete, so we should poll and wait
				continue
			}
			glog.Fatalf("failed to delete namespace %q: %q", namespace.GetName(), err)
		}
	}

	err = poll(globalTimeout, func() (bool, error) {
		list, listErr := kubeClient.CoreV1().Namespaces().List(listOptions)
		if listErr != nil {
			glog.Fatalf("failed to list namespaces: %q", listErr)
		}

		if len(list.Items) == 0 {
			return true, nil
		}

		return false, nil
	})

	if err != nil {
		glog.Fatalf("timed out waiting for namespaces to be cleaned up before testing")
	}
}

func testNamespace(name string) *corev1.Namespace {
	name = kebabCaseName(name)
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				shippertesting.TestLabel: name,
			},
		},
	}
}

func buildManagementClients(masterURL, kubeconfig string) (kubernetes.Interface, shipperclientset.Interface, error) {
	restCfg, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
	if err != nil {
		return nil, nil, err
	}

	newKubeClient, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, nil, err
	}

	newShipperClient, err := shipperclientset.NewForConfig(restCfg)
	if err != nil {
		return nil, nil, err
	}

	return newKubeClient, newShipperClient, nil
}

func buildTargetClient(clusterName string) kubernetes.Interface {
	secret, err := kubeClient.CoreV1().Secrets(shipperv1.ShipperNamespace).Get(clusterName, metav1.GetOptions{})
	if err != nil {
		glog.Fatalf("could not build target kubeclient for cluster %q: problem fetching secret: %q", clusterName, err)
	}

	cluster, err := shipperClient.ShipperV1().Clusters().Get(clusterName, metav1.GetOptions{})
	if err != nil {
		glog.Fatalf("could not build target kubeclient for cluster %q: problem fetching cluster: %q", clusterName, err)
	}

	config := &rest.Config{
		Host: cluster.Spec.APIMaster,
	}

	// The cluster secret controller does not include the CA in the secret: you end
	// up using the system CA trust store. However, it's much handier for
	// integration testing to be able to create a secret that is independent of the
	// underlying system trust store.
	if ca, ok := secret.Data["tls.ca"]; ok {
		config.CAData = ca
	}

	config.CertData = secret.Data["tls.crt"]
	config.KeyData = secret.Data["tls.key"]

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		glog.Fatalf("could not build target kubeclient for cluster %q: problem fetching cluster: %q", clusterName, err)
	}
	return client
}

func newApplication(namespace, name string, strategy *shipperv1.RolloutStrategy) *shipperv1.Application {
	return &shipperv1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: shipperv1.ApplicationSpec{
			Template: shipperv1.ReleaseEnvironment{
				Chart: shipperv1.Chart{
					RepoURL: chartRepo,
				},
				Strategy: strategy,
				// TODO(btyler): implement enough cluster selector stuff to only pick the
				// target cluster we care about (or just panic if that cluster isn't
				// listed).
				ClusterRequirements: shipperv1.ClusterRequirements{Regions: []shipperv1.RegionRequirement{{Name: regionFromMinikubePerlScript}}},
				Values:              &shipperv1.ChartValues{},
			},
		},
	}
}

func capacityInPods(percentage int32, targetReplicas int) int {
	mult := float64(percentage) / 100
	// Round up to nearest whole pod.
	return int(math.Ceil(mult * float64(targetReplicas)))
}
