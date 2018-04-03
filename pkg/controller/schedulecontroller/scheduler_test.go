package schedulecontroller

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"testing"
	"time"

	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"

	shipperV1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

func loadObject(obj runtime.Object, path ...string) (runtime.Object, error) {
	yamlPath := filepath.Join(path...)
	if releaseRaw, err := ioutil.ReadFile(yamlPath); err != nil {
		return nil, err
	} else if _, _, err = scheme.Codecs.UniversalDeserializer().Decode(releaseRaw, nil, obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func loadRelease() *shipperV1.Release {
	if obj, err := loadObject(&shipperV1.Release{}, "testdata", "release.yaml"); err != nil {
		panic(err)
	} else {
		return obj.(*shipperV1.Release)
	}
}

func loadCluster(name string) *shipperV1.Cluster {
	fileName := fmt.Sprintf("cluster-%s.yaml", name)
	if obj, err := loadObject(&shipperV1.Cluster{}, "testdata", fileName); err != nil {
		panic(err)
	} else {
		return obj.(*shipperV1.Cluster)
	}
}

func newScheduler(
	release *shipperV1.Release,
	fixtures []runtime.Object,
) (*Scheduler, *shipperfake.Clientset) {
	clientset := shipperfake.NewSimpleClientset(fixtures...)
	informerFactory := shipperinformers.NewSharedInformerFactory(clientset, time.Millisecond*0)
	clustersLister := informerFactory.Shipper().V1().Clusters().Lister()

	c := NewScheduler(release, clientset, clustersLister, record.NewFakeRecorder(42))

	stopCh := make(chan struct{})
	defer close(stopCh)

	informerFactory.Start(stopCh)
	informerFactory.WaitForCacheSync(stopCh)

	return c, clientset
}

// TestComputeTargetClusters tests the first part of the cluster scheduling,
// which is find out which clusters the release must be installed, and
// persisting it under .status.environment.clusters.
func TestComputeTargetClusters(t *testing.T) {
	// Fixtures
	clusterA := loadCluster("minikube-a")
	clusterB := loadCluster("minikube-b")
	release := loadRelease()
	fixtures := []runtime.Object{clusterA, clusterB, release}

	// Expected values. The release should have, at the end of the business
	// logic, a list of clusters containing all clusters we've added to
	// the client in alphabetical order.
	expected := release.DeepCopy()
	expected.Environment.Clusters = []string{
		clusterA.GetName(),
		clusterB.GetName(),
	}
	expectedActions := []kubetesting.Action{
		kubetesting.NewUpdateAction(
			shipperV1.SchemeGroupVersion.WithResource("releases"),
			release.GetNamespace(),
			expected),
	}

	// Business logic...
	c, clientset := newScheduler(release, fixtures)
	if err := c.scheduleRelease(); err != nil {
		t.Fatal(err)
	}

	// Check actions
	filteredActions := filterActions(clientset.Actions(), []string{"update"}, []string{"releases"})
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

// TestComputeTargetClustersSkipUnscheduled tests the first part of the cluster scheduling,
// which is find out which clusters the release must be installed, and
// persisting it under .status.environment.clusters but skipping unschedulable
// clusters this time.
func TestComputeTargetClustersSkipUnscheduled(t *testing.T) {
	// Fixtures
	clusterA := loadCluster("minikube-a")
	clusterB := loadCluster("minikube-b")
	clusterB.Spec.Unschedulable = true
	release := loadRelease()
	fixtures := []runtime.Object{clusterA, clusterB, release}

	// Expected values. The release should have, at the end of the business
	// logic, a list of clusters containing the schedulable cluster we've
	// added to the client.
	expected := release.DeepCopy()
	expected.Environment.Clusters = []string{
		clusterA.GetName(),
	}
	expectedActions := []kubetesting.Action{
		kubetesting.NewUpdateAction(
			shipperV1.SchemeGroupVersion.WithResource("releases"),
			release.GetNamespace(),
			expected),
	}

	// Business logic...
	c, clientset := newScheduler(release, fixtures)
	if err := c.scheduleRelease(); err != nil {
		t.Fatal(err)
	}

	// Check actions
	filteredActions := filterActions(clientset.Actions(), []string{"update"}, []string{"releases"})
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

func expectedActions(ns string, release *shipperV1.Release) []kubetesting.Action {
	actions := []kubetesting.Action{
		kubetesting.NewCreateAction(
			shipperV1.SchemeGroupVersion.WithResource("installationtargets"),
			ns,
			nil),
		kubetesting.NewCreateAction(
			shipperV1.SchemeGroupVersion.WithResource("traffictargets"),
			ns,
			nil),
		kubetesting.NewCreateAction(
			shipperV1.SchemeGroupVersion.WithResource("capacitytargets"),
			ns,
			nil),
		kubetesting.NewUpdateAction(
			shipperV1.SchemeGroupVersion.WithResource("releases"),
			ns,
			release),
	}
	return actions
}

func TestCreateAssociatedObjects(t *testing.T) {
	// Fixtures
	cluster := loadCluster("minikube-a")
	release := loadRelease()
	release.Environment.Clusters = []string{cluster.GetName()}
	fixtures := []runtime.Object{release, cluster}

	// Expected release and actions. The release should have, at the end of
	// the business logic, a list of clusters containing the sole cluster
	// we've added to the client, and also its .status.phase key set to
	// "WaitingForStrategy". Expected actions contain the intent to create
	// all the associated target objects.
	expected := release.DeepCopy()
	expected.Status.Phase = shipperV1.ReleasePhaseWaitingForStrategy
	expectedActions := expectedActions(release.GetNamespace(), expected)

	// Business logic...
	c, clientset := newScheduler(release, fixtures)
	if err := c.scheduleRelease(); err != nil {
		t.Fatal(err)
	}

	// Check actions
	filteredActions := filterActions(
		clientset.Actions(),
		[]string{"update", "create"},
		[]string{"releases", "installationtargets", "traffictargets", "capacitytargets"},
	)
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

func TestCreateAssociatedObjectsDuplicateInstallationTarget(t *testing.T) {
	// Fixtures
	cluster := loadCluster("minikube-a")
	release := loadRelease()
	release.Environment.Clusters = []string{cluster.GetName()}
	installationtarget := &shipperV1.InstallationTarget{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      release.GetName(),
			Namespace: release.GetNamespace(),
		},
	}
	fixtures := []runtime.Object{release, cluster, installationtarget}

	// Expected release and actions. Even with an existing installationtarget
	// object for this release, at the end of the business logic the expected
	// release should have its .status.phase set to "WaitingForStrategy".
	// Expected actions contain the intent to create all the associated target
	// objects.
	expected := release.DeepCopy()
	expected.Status.Phase = shipperV1.ReleasePhaseWaitingForStrategy
	expectedActions := expectedActions(release.GetNamespace(), expected)

	// Business logic...
	c, clientset := newScheduler(release, fixtures)
	if err := c.scheduleRelease(); err != nil {
		t.Fatal(err)
	}

	// Check actions
	filteredActions := filterActions(
		clientset.Actions(),
		[]string{"update", "create"},
		[]string{"releases", "installationtargets", "traffictargets", "capacitytargets"},
	)
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

func TestCreateAssociatedObjectsDuplicateTrafficTarget(t *testing.T) {
	// Fixtures
	cluster := loadCluster("minikube-a")
	release := loadRelease()
	release.Environment.Clusters = []string{cluster.GetName()}
	traffictarget := &shipperV1.TrafficTarget{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      release.GetName(),
			Namespace: release.GetNamespace(),
		},
	}
	fixtures := []runtime.Object{cluster, release, traffictarget}

	// Expected release and actions. Even with an existing installationtarget
	// object for this release, at the end of the business logic the expected
	// release should have its .status.phase set to "WaitingForStrategy".
	// Expected actions contain the intent to create all the associated target
	// objects.
	expected := release.DeepCopy()
	expected.Status.Phase = shipperV1.ReleasePhaseWaitingForStrategy
	expectedActions := expectedActions(release.GetNamespace(), expected)

	// Business logic...
	c, clientset := newScheduler(release, fixtures)
	if err := c.scheduleRelease(); err != nil {
		t.Fatal(err)
	}

	// Check actions
	filteredActions := filterActions(
		clientset.Actions(),
		[]string{"update", "create"},
		[]string{"releases", "installationtargets", "traffictargets", "capacitytargets"},
	)
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

func TestCreateAssociatedObjectsDuplicateCapacityTarget(t *testing.T) {
	// Fixtures
	cluster := loadCluster("minikube-a")
	release := loadRelease()
	release.Environment.Clusters = []string{cluster.GetName()}
	capacitytarget := &shipperV1.CapacityTarget{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      release.GetName(),
			Namespace: release.GetNamespace(),
		},
	}
	fixtures := []runtime.Object{cluster, release, capacitytarget}

	// Expected release and actions. Even with an existing capacitytarget
	// object for this release, at the end of the business logic the expected
	// release should have its .status.phase set to "WaitingForStrategy".
	// Expected actions contain the intent to create all the associated target
	// objects.
	expected := release.DeepCopy()
	expected.Status.Phase = shipperV1.ReleasePhaseWaitingForStrategy
	expectedActions := expectedActions(release.GetNamespace(), expected)

	// Business logic...
	c, clientset := newScheduler(release, fixtures)
	if err := c.scheduleRelease(); err != nil {
		t.Fatal(err)
	}

	// Check actions
	actions := filterActions(
		clientset.Actions(),
		[]string{"update", "create"},
		[]string{"releases", "installationtargets", "traffictargets", "capacitytargets"},
	)
	shippertesting.CheckActions(expectedActions, actions, t)
}

func filterActions(
	actions []kubetesting.Action,
	verbs []string,
	resources []string,
) []kubetesting.Action {
	ignore := func(action kubetesting.Action) bool {
		for _, v := range verbs {
			for _, r := range resources {
				if action.Matches(v, r) {
					return false
				}
			}
		}

		return true
	}

	var ret []kubetesting.Action
	for _, action := range actions {
		if ignore(action) {
			continue
		}

		ret = append(ret, action)
	}

	return ret
}
