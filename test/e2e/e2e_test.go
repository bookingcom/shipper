package e2e

import (
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/golang/glog"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/helm/pkg/repo/repotest"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

const (
	appName = "my-test-app"
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
	timeoutFlag   = flag.String("timeout", "30s", "timeout when waiting for things to change")
)

var (
	targetKubeClient kubernetes.Interface
	kubeClient       kubernetes.Interface
	shipperClient    shipperclientset.Interface
	chartRepo        string
	globalTimeout    time.Duration
)

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

func TestNewApplicationVanguard(t *testing.T) {
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

	newApp := newApplication(ns.GetName(), appName)
	newApp.Spec.Template.Values = &shipperv1.ChartValues{"replicaCount": targetReplicas}
	newApp.Spec.Template.Chart.Name = "test-nginx"
	newApp.Spec.Template.Chart.Version = "0.0.1"
	newApp.Spec.Template.Strategy.Name = "vanguard"

	_, err = shipperClient.ShipperV1().Applications(ns.GetName()).Create(newApp)
	if err != nil {
		t.Fatalf("could not create application %q: %q", appName, err)
	}

	// I think this could be significantly simplified if we had access to the
	// strategy's instance; we could iterate over it until step n-1 and do this
	// work repeatedly. To do this properly we'd probably want to change the
	// strategy controller to read objects rather than use a hardcoded struct
	t.Logf("waiting for a new release for new application %q", appName)
	rel := f.waitForRelease(appName, 0)
	relName := rel.GetName()
	t.Logf("waiting for new release %q to reach state 'waiting for command'", relName)
	f.waitForCommand(relName)
	t.Logf("checking that release %q has 1 pod (strategy step 0)", relName)
	f.checkPods(relName, 1)

	t.Logf("setting release %q targetStep to 1", relName)
	f.targetStep(1, relName)
	t.Logf("waiting for release %q to achieve waitingForCommand for targetStep 1", relName)
	f.waitForCommand(relName)
	t.Logf("checking that release %q has %d pods (strategy step 1)", relName, targetReplicas/2)
	f.checkPods(relName, targetReplicas/2)

	t.Logf("setting release %q targetStep to 1", relName)
	f.targetStep(2, rel.GetName())
	t.Logf("waiting for release %q to reach phase 'installed'", relName)
	f.waitForInstalled(rel.GetName())
	t.Logf("checking that release %q has %d pods (strategy step 2 -- finished)", relName, targetReplicas)
	f.checkPods(relName, targetReplicas)
}

func TestRolloutVanguard(t *testing.T) {
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

	app := newApplication(ns.GetName(), appName)
	app.Spec.Template.Values = &shipperv1.ChartValues{"replicaCount": targetReplicas}
	app.Spec.Template.Chart.Name = "test-nginx"
	app.Spec.Template.Chart.Version = "0.0.1"
	app.Spec.Template.Strategy.Name = "vanguard"

	_, err = shipperClient.ShipperV1().Applications(ns.GetName()).Create(app)
	if err != nil {
		t.Fatalf("could not create application %q: %q", appName, err)
	}

	incumbent := f.waitForRelease(appName, 0)
	f.waitForCommand(incumbent.GetName())
	// immediately to step 2 to install ASAP
	f.targetStep(2, incumbent.GetName())
	f.waitForInstalled(incumbent.GetName())
	f.checkPods(incumbent.GetName(), targetReplicas)

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
	t.Logf("waiting for contender %q to reach 'waiting for command' (strategy step 0)", contender.GetName())
	f.waitForCommand(contender.GetName())
	t.Logf(
		"checking that incumbent %q has %d pods and contender %q has %d pods (strategy step 0 -- 100/1)",
		incumbent.GetName(), targetReplicas, contender.GetName(), 1,
	)
	f.checkPods(incumbent.GetName(), targetReplicas)
	f.checkPods(contender.GetName(), 1)

	f.targetStep(1, contender.GetName())
	t.Logf("waiting for contender %q to reach 'waiting for command' (strategy step 1)", contender.GetName())
	f.waitForCommand(contender.GetName())
	t.Logf(
		"checking that incumbent %q has %d pods and contender %q has %d pods (strategy step 1 -- 50/50)",
		incumbent.GetName(), targetReplicas/2, contender.GetName(), targetReplicas/2,
	)
	f.checkPods(contender.GetName(), targetReplicas/2)
	// TODO(btyler): this is disabled because the conditions indicate that the incumbent has achieved capacity for step 1 when it hasn't yet
	// f.checkPods(incumbent.GetName(), targetReplicas/2)

	f.targetStep(2, contender.GetName())
	t.Logf("waiting for contender %q to reach phase 'installed'", contender.GetName())
	f.waitForInstalled(contender.GetName())
	t.Logf(
		"checking that incumbent %q has %d pods and contender %q has %d pods (strategy step 2 -- finished)",
		incumbent.GetName(), 0, contender.GetName(), targetReplicas,
	)
	// TODO(btyler): this is disabled because the conditions indicate that the incumbent has achieved capacity for step 2 when it hasn't yet
	// f.checkPods(incumbent.GetName(), 0)
	f.checkPods(contender.GetName(), targetReplicas)
}

//TODO(btyler): cover a variety of broken chart cases as soon as we report those outcomes somewhere other than stderr

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
	rel, err := shipperClient.ShipperV1().Releases(f.namespace).Get(relName, metav1.GetOptions{})
	if err != nil {
		f.t.Fatalf("release for targetStep could not be fetched: %q: %q", relName, err)
	}
	rel.Spec.TargetStep = step
	_, err = shipperClient.ShipperV1().Releases(f.namespace).Update(rel)
	if err != nil {
		f.t.Fatalf("could not update release with targetStep %v: %q", step, err)
	}
}

func (f *fixture) checkPods(relName string, count int) {
	selector := labels.Set{shipperv1.ReleaseLabel: relName}.AsSelector()
	podList, err := targetKubeClient.CoreV1().Pods(f.namespace).List(metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		f.t.Fatalf("could not list pods %q: %q", f.namespace, err)
	}

	if len(podList.Items) != count {
		f.t.Fatalf("checking pods on release %q: expected %d but got %d", relName, count, len(podList.Items))
	}
}

func (f *fixture) waitForRelease(appName string, historyIndex int) *shipperv1.Release {
	var newRelease *shipperv1.Release
	// not logging because we poll pretty fast and that'd be a bunch of garbage to read through
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

		if app.Status.History[historyIndex].Status != shipperv1.ReleaseRecordObjectCreated {
			state = fmt.Sprintf("history entry for index %v is not ObjectCreated", historyIndex)
			return false, nil
		}

		relName := app.Status.History[historyIndex].Name
		rel, err := shipperClient.ShipperV1().Releases(f.namespace).Get(relName, metav1.GetOptions{})
		if err != nil {
			f.t.Fatalf("release which was marked as created in app history was not fetched: %q: %q", relName, err)
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

	return newRelease
}

func (f *fixture) waitForCommand(releaseName string) {
	var state string
	err := poll(globalTimeout, func() (bool, error) {
		rel, err := shipperClient.ShipperV1().Releases(f.namespace).Get(releaseName, metav1.GetOptions{})
		if err != nil {
			f.t.Fatalf("failed to fetch release: %q", releaseName)
		}

		if rel.Status.Strategy == nil {
			state = fmt.Sprintf("release %q has no strategy status reported yet", releaseName)
			return false, nil
		}

		if rel.Status.Strategy.State.WaitingForCommand == shipperv1.StrategyStateTrue {
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
}

func (f *fixture) waitForInstalled(releaseName string) {
	err := poll(globalTimeout, func() (bool, error) {
		rel, err := shipperClient.ShipperV1().Releases(f.namespace).Get(releaseName, metav1.GetOptions{})
		if err != nil {
			f.t.Fatalf("failed to fetch release: %q", releaseName)
		}

		if rel.Status.Phase == shipperv1.ReleasePhaseInstalled {
			return true, nil
		}

		return false, nil
	})

	if err != nil {
		f.t.Fatalf("error waiting for release to be installed: %q", err)
	}
}

func poll(timeout time.Duration, waitCondition func() (bool, error)) error {
	return wait.PollUntil(
		50*time.Millisecond,
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

	/* 'server does not allow this method on the requested resource'
	err = kubeClient.CoreV1().Namespaces().DeleteCollection(&metav1.DeleteOptions{}, listOptions)
	if err != nil {
		glog.Fatalf("failed to delete namespaces matching %q: %q", selector.String(), err)
	}
	*/

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

	// the cluster secret controller does not include the CA in the secret:
	// you end up using the system CA trust store. However, it's much handier
	// for integration testing to be able to create a secret that is
	// independent of the underlying system trust store.
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

func newApplication(namespace, name string) *shipperv1.Application {
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
				Strategy: shipperv1.ReleaseStrategy{},
				// TODO(btyler) implement enough cluster selector stuff to only pick the target cluster we care about
				// (or just panic if that cluster isn't listed)
				ClusterSelectors: []shipperv1.ClusterSelector{},
				Values:           &shipperv1.ChartValues{},
			},
		},
	}
}
