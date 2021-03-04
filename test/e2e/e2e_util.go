package e2e

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

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
	"k8s.io/klog"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
	"github.com/bookingcom/shipper/pkg/util/rolloutblock"
)

const (
	appName          = "my-test-app"
	rolloutBlockName = "my-test-rollout-block"
)

var (
	appKubeClient kubernetes.Interface
	kubeClient    kubernetes.Interface
	shipperClient shipperclientset.Interface
	chartRepo     string
	testRegion    string
	globalTimeout time.Duration
)

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
	_, err := shipperClient.ShipperV1alpha1().Releases(f.namespace).Patch(relName, types.MergePatchType, []byte(patch))
	if err != nil {
		f.t.Fatalf("could not patch release with targetStep %v: %q", step, err)
	}
}

func (f *fixture) checkReadyPods(relName string, expectedCount int) []corev1.Pod {
	selector := labels.Set{shipper.ReleaseLabel: relName}.AsSelector()
	podList, err := appKubeClient.CoreV1().Pods(f.namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		f.t.Fatalf("could not list pods %q: %q", f.namespace, err)
	}

	readyPods := []corev1.Pod{}
	for _, pod := range podList.Items {
		for _, condition := range pod.Status.Conditions {
			// This line imitates how ReplicaSets calculate 'ready replicas'; Shipper
			// uses 'availableReplicas' but the minReadySeconds in this test is 0.
			// There's no handy library for this because the functionality is split
			// between k8s' controller_util.go and api v1 podUtil.
			if condition.Type == "Ready" && condition.Status == "True" && pod.DeletionTimestamp == nil {
				readyPods = append(readyPods, pod)
				break
			}
		}
	}

	if len(readyPods) != expectedCount {
		f.t.Fatalf("checking pods on release %q: expected %d but got %d", relName, expectedCount, len(readyPods))
	}

	return readyPods
}

func (f *fixture) checkEndpointActivePods(epName string, pods []corev1.Pod) {
	ep, err := appKubeClient.CoreV1().Endpoints(f.namespace).Get(context.TODO(), epName, metav1.GetOptions{})
	if err != nil {
		f.t.Fatalf("could not fetch endpoint %q: %q", epName, err)
	}

	podMap := make(map[string]corev1.Pod)
	for _, pod := range pods {
		podMap[pod.Name] = pod
	}

	readyPods := make([]string, 0, len(pods))
	for _, subset := range ep.Subsets {
		for _, addr := range subset.Addresses {
			pod, ok := podMap[addr.TargetRef.Name]
			if !ok {
				f.t.Fatalf("unexpected pod in the endpoint: %q", pod.Name)
			}
			readyPods = append(readyPods, pod.Name)
		}
	}

	if len(readyPods) != len(pods) {
		f.t.Fatalf("wrong address count in the endpoint %q: want: %d, got: %d", epName, len(readyPods), len(pods))
	}
}

func (f *fixture) waitForRelease(appName string, historyIndex int) *shipper.Release {
	var newRelease *shipper.Release
	start := time.Now()
	// Not logging because we poll pretty fast and that'd be a bunch of garbage to
	// read through.
	var state string
	err := poll(globalTimeout, func() (bool, error) {
		app, err := shipperClient.ShipperV1alpha1().Applications(f.namespace).Get(appName, metav1.GetOptions{})
		if err != nil {
			f.t.Fatalf("failed to fetch app: %q", appName)
		}

		if len(app.Status.History) != historyIndex+1 {
			state = fmt.Sprintf("wrong number of entries in history: expected %v but got %v", historyIndex+1, len(app.Status.History))
			return false, nil
		}

		relName := app.Status.History[historyIndex]
		rel, err := shipperClient.ShipperV1alpha1().Releases(f.namespace).Get(relName, metav1.GetOptions{})
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

	f.t.Logf("waiting for release %q took %s", newRelease.Name, time.Since(start))
	return newRelease
}

func (f *fixture) waitForReleaseStrategyState(waitingFor string, releaseName string, step int) {
	var state, newState string
	start := time.Now()
	err := poll(globalTimeout, func() (bool, error) {
		defer func() {
			if state != newState {
				f.t.Logf("release strategy state transition: %q -> %q", state, newState)
				state = newState
			}
		}()
		rel, err := shipperClient.ShipperV1alpha1().Releases(f.namespace).Get(releaseName, metav1.GetOptions{})
		if err != nil {
			f.t.Fatalf("failed to fetch release: %q: %q", releaseName, err)
		}

		if rel.Status.Strategy == nil {
			newState = fmt.Sprintf("release %q has no strategy status reported yet", releaseName)
			return false, nil
		}

		if rel.Status.AchievedStep == nil {
			newState = fmt.Sprintf("release %q has no achievedStep reported yet", releaseName)
			return false, nil
		}

		condAchieved := false
		switch waitingFor {
		case "installation":
			condAchieved = rel.Status.Strategy.State.WaitingForInstallation == shipper.StrategyStateTrue
		case "capacity":
			condAchieved = rel.Status.Strategy.State.WaitingForCapacity == shipper.StrategyStateTrue
		case "traffic":
			condAchieved = rel.Status.Strategy.State.WaitingForTraffic == shipper.StrategyStateTrue
		case "command":
			condAchieved = rel.Status.Strategy.State.WaitingForCommand == shipper.StrategyStateTrue
		case "none":
			condAchieved = rel.Status.Strategy.State.WaitingForInstallation == shipper.StrategyStateFalse &&
				rel.Status.Strategy.State.WaitingForCapacity == shipper.StrategyStateFalse &&
				rel.Status.Strategy.State.WaitingForTraffic == shipper.StrategyStateFalse &&
				rel.Status.Strategy.State.WaitingForCommand == shipper.StrategyStateFalse

		}

		newState = fmt.Sprintf("{installation: %s, capacity: %s, traffic: %s, command: %s}",
			rel.Status.Strategy.State.WaitingForInstallation,
			rel.Status.Strategy.State.WaitingForCapacity,
			rel.Status.Strategy.State.WaitingForTraffic,
			rel.Status.Strategy.State.WaitingForCommand,
		)

		if condAchieved && rel.Status.AchievedStep.Step == int32(step) {
			return true, nil
		}

		return false, nil
	})

	if err != nil {
		if err == wait.ErrWaitTimeout {
			f.t.Fatalf("timed out waiting for release to be waiting for %s: waited %s. final state: %s", waitingFor, globalTimeout, state)
		}
		f.t.Fatalf("error waiting for release to be waiting for %s: %q", waitingFor, err)
	}

	f.t.Logf("waiting for %s took %s", waitingFor, time.Since(start))
}

func (f *fixture) waitForComplete(releaseName string) {
	start := time.Now()
	err := poll(globalTimeout, func() (bool, error) {
		rel, err := shipperClient.ShipperV1alpha1().Releases(f.namespace).Get(releaseName, metav1.GetOptions{})
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
	f.t.Logf("waiting for completion of %q took %s", releaseName, time.Since(start))
}

func (f *fixture) waitForStatus(rbName string, rbNamespace string, apps string, rels string) {
	start := time.Now()
	var state string
	err := poll(globalTimeout, func() (bool, error) {
		rb, err := shipperClient.ShipperV1alpha1().RolloutBlocks(rbNamespace).Get(rolloutBlockName, metav1.GetOptions{})
		if err != nil {
			f.t.Fatalf("could not create rollout block %q: %q", rbName, err)
		}

		if len(rb.Status.Overrides.Application) != len(apps) {
			state = fmt.Sprintf("wrong number of apps overrides in status: expected %s but got %s", apps, rb.Status.Overrides.Application)
			return false, nil
		}

		if len(rb.Status.Overrides.Release) != len(rels) {
			state = fmt.Sprintf("wrong number of releases overrides in status: expected %s but got %s", rels, rb.Status.Overrides.Release)
			return false, nil
		}

		expectedApps := rolloutblock.NewObjectNameList(apps)
		expectedRels := rolloutblock.NewObjectNameList(rels)

		actualApps := rolloutblock.NewObjectNameList(rb.Status.Overrides.Application)
		actualRels := rolloutblock.NewObjectNameList(rb.Status.Overrides.Release)

		if expectedApps.String() != actualApps.String() {
			state = fmt.Sprintf("wrong apps overrides in status: expected %s but got %s", apps, rb.Status.Overrides.Application)
			return false, nil
		}

		if expectedRels.String() != actualRels.String() {
			state = fmt.Sprintf("wrong number of releases overrides in status: expected %s but got %s", rels, rb.Status.Overrides.Release)
			return false, nil
		}

		return true, nil
	})
	if err != nil {
		if err == wait.ErrWaitTimeout {
			f.t.Fatalf("timed out waiting for rollout block status to be updated (waited %s). final state %q", globalTimeout, state)
		}
		f.t.Fatalf("error waiting for rollout block status to be updated: %q", err)
	}

	f.t.Logf("waiting for rollout block %q/%q status took %s", rbNamespace, rbName, time.Since(start))
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

	// We create a test namespace once in the management cluster, and once
	// in the application cluster. Sometimes, in development, both of these
	// happen to be the same, so the second time we try to create the ns
	// it'll error out with "namespace foobar already exists", so if that
	// happens we'll just ignore it.

	ns, err := appKubeClient.CoreV1().Namespaces().Create(context.TODO(), newNs)
	if err != nil {
		return nil, err
	}

	_, err = kubeClient.CoreV1().Namespaces().Create(context.TODO(), newNs)
	if err != nil && !errors.IsAlreadyExists(err) {
		return nil, err
	}

	return ns, nil
}

func teardownNamespace(name string) {
	teardownNamespaceFromCluster(kubeClient, name)
	teardownNamespaceFromCluster(appKubeClient, name)
}

func teardownNamespaceFromCluster(client kubernetes.Interface, name string) {
	err := client.CoreV1().Namespaces().Delete(name, &metav1.DeleteOptions{})
	if err != nil {
		klog.Fatalf("failed to clean up namespace %q: %q", name, err)
	}
}

func purgeTestNamespaces() {
	req, err := labels.NewRequirement(
		shippertesting.E2ETestNamespaceLabel,
		selection.Exists,
		[]string{},
	)

	if err != nil {
		panic("a static label for deleting namespaces failed to parse. please fix the tests")
	}

	selector := labels.NewSelector().Add(*req)
	listOptions := metav1.ListOptions{LabelSelector: selector.String()}

	purgeNamespaceFromCluster(kubeClient, listOptions)
	purgeNamespaceFromCluster(appKubeClient, listOptions)
}

func purgeNamespaceFromCluster(client kubernetes.Interface, listOptions metav1.ListOptions) {
	list, err := client.CoreV1().Namespaces().List(listOptions)
	if err != nil {
		klog.Fatalf("failed to list namespaces: %q", err)
	}

	if len(list.Items) == 0 {
		return
	}

	for _, namespace := range list.Items {
		err = client.CoreV1().Namespaces().Delete(namespace.GetName(), &metav1.DeleteOptions{})
		if err != nil {
			if errors.IsConflict(err) {
				// this means the namespace is still cleaning up from some other delete, so we should poll and wait
				continue
			}
			klog.Fatalf("failed to delete namespace %q: %q", namespace.GetName(), err)
		}
	}

	err = poll(globalTimeout, func() (bool, error) {
		list, listErr := client.CoreV1().Namespaces().List(listOptions)
		if listErr != nil {
			klog.Fatalf("failed to list namespaces: %q", listErr)
		}

		if len(list.Items) == 0 {
			return true, nil
		}

		return false, nil
	})

	if err != nil {
		klog.Fatalf("timed out waiting for namespaces to be cleaned up before testing")
	}
}

func testNamespace(name string) *corev1.Namespace {
	name = kebabCaseName(name)
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				shippertesting.E2ETestNamespaceLabel: name,
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

func buildApplicationClient(cluster *shipper.Cluster) kubernetes.Interface {
	secret, err := kubeClient.CoreV1().Secrets(shipper.ShipperNamespace).Get(cluster.Name, metav1.GetOptions{})
	if err != nil {
		klog.Fatalf("could not build target kubeclient for cluster %q: problem fetching secret: %q", cluster.Name, err)
	}

	config := &rest.Config{
		Host: cluster.Spec.APIMaster,
	}

	_, tokenOK := secret.Data["token"]
	if tokenOK {
		ca := secret.Data["ca.crt"]
		config.CAData = ca

		token := secret.Data["token"]
		config.BearerToken = string(token)
	} else {
		// The cluster secret controller does not include the CA in the secret: you end
		// up using the system CA trust store. However, it's much handier for
		// integration testing to be able to create a secret that is independent of the
		// underlying system trust store.
		if ca, ok := secret.Data["tls.ca"]; ok {
			config.CAData = ca
		}

		config.CertData = secret.Data["tls.crt"]
		config.KeyData = secret.Data["tls.key"]

		if encodedInsecureSkipTlsVerify, ok := secret.Annotations[shipper.SecretClusterSkipTlsVerifyAnnotation]; ok {
			if insecureSkipTlsVerify, err := strconv.ParseBool(encodedInsecureSkipTlsVerify); err == nil {
				klog.Infof("found %q annotation with value %q", shipper.SecretClusterSkipTlsVerifyAnnotation, encodedInsecureSkipTlsVerify)
				config.Insecure = insecureSkipTlsVerify
			} else {
				klog.Infof("found %q annotation with value %q, failed to decode a bool from it, ignoring it", shipper.SecretClusterSkipTlsVerifyAnnotation, encodedInsecureSkipTlsVerify)
			}
		}
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Fatalf("could not build target kubeclient for cluster %q: problem fetching cluster: %q", cluster.Name, err)
	}
	return client
}

func newApplication(namespace, name string, strategy *shipper.RolloutStrategy) *shipper.Application {
	return &shipper.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: map[string]string{},
		},
		Spec: shipper.ApplicationSpec{
			Template: shipper.ReleaseEnvironment{
				Chart: shipper.Chart{
					RepoURL: chartRepo,
				},
				Strategy: strategy,
				// TODO(btyler): implement enough cluster selector stuff to only pick the
				// target cluster we care about (or just panic if that cluster isn't
				// listed).
				ClusterRequirements: shipper.ClusterRequirements{Regions: []shipper.RegionRequirement{{Name: testRegion}}},
				Values:              &shipper.ChartValues{},
			},
		},
	}
}

func createRolloutBlock(namespace, name string) (*shipper.RolloutBlock, error) {
	rb, err := shipperClient.ShipperV1alpha1().RolloutBlocks(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			return nil, err
		}

		rolloutBlock := &shipper.RolloutBlock{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: shipper.RolloutBlockSpec{
				Message: "Simple test rollout block",
				Author: shipper.RolloutBlockAuthor{
					Type: "user",
					Name: "testUser",
				},
			},
		}

		rb, err = shipperClient.ShipperV1alpha1().RolloutBlocks(namespace).Create(rolloutBlock)
		if err != nil {
			return nil, err
		}

		// RolloutBlocks frequently take a little bit to propagate to
		// Shipper's informers, making tests fail for no good reason.
		// Since we have found no way to verify that they have
		// propagated, we use the good ol' sleep as the second best
		// thing.
		time.Sleep(1 * time.Second)
	}

	return rb, nil
}
