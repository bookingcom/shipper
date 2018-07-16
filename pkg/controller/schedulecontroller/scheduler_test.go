package schedulecontroller

import (
	"fmt"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"

	shipperV1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	"github.com/bookingcom/shipper/pkg/chart"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

func init() {
	releaseutil.ConditionsShouldDiscardTimestamps = true
}

func buildRelease() *shipperV1.Release {
	return &shipperV1.Release{
		TypeMeta: metaV1.TypeMeta{
			APIVersion: "shipper.booking.com/v1",
			Kind:       "Release",
		},
		ReleaseMeta: shipperV1.ReleaseMeta{
			ObjectMeta: metaV1.ObjectMeta{
				Name:        "test-release",
				Namespace:   shippertesting.TestNamespace,
				Annotations: map[string]string{},
				OwnerReferences: []metaV1.OwnerReference{
					{
						APIVersion: "shipper.booking.com/v1",
						Kind:       "Application",
						Name:       "test-application",
					},
				},
			},
			Environment: shipperV1.ReleaseEnvironment{
				Chart: shipperV1.Chart{
					Name:    "simple",
					Version: "0.0.1",
					RepoURL: chartRepoURL,
				},
				ClusterRequirements: shipperV1.ClusterRequirements{
					Regions: []shipperV1.RegionRequirement{{Name: "local"}},
				},
			},
		},
	}
}

func buildCluster(name string) *shipperV1.Cluster {
	return &shipperV1.Cluster{
		ObjectMeta: metaV1.ObjectMeta{
			Name: name,
		},
		Spec: shipperV1.ClusterSpec{
			APIMaster:    "https://127.0.0.1",
			Capabilities: []string{},
			Region:       "local",
		},
	}
}

func newScheduler(
	release *shipperV1.Release,
	fixtures []runtime.Object,
) (*Scheduler, *shipperfake.Clientset) {
	clientset := shipperfake.NewSimpleClientset(fixtures...)
	informerFactory := shipperinformers.NewSharedInformerFactory(clientset, time.Millisecond*0)
	clustersLister := informerFactory.Shipper().V1().Clusters().Lister()

	c := NewScheduler(release, clientset, clustersLister, chart.FetchRemote(), record.NewFakeRecorder(42))

	stopCh := make(chan struct{})
	defer close(stopCh)

	informerFactory.Start(stopCh)
	informerFactory.WaitForCacheSync(stopCh)

	return c, clientset
}

// TestSchedule tests the first part of the cluster scheduling,
// which is find out which clusters the release must be installed, and
// persisting it under .status.environment.clusters.
func TestSchedule(t *testing.T) {
	// Fixtures
	clusterA := buildCluster("minikube-a")
	clusterB := buildCluster("minikube-b")
	release := buildRelease()
	fixtures := []runtime.Object{clusterA, clusterB, release}
	// demand two clusters
	release.Environment.ClusterRequirements.Regions[0].Replicas = pint32(2)

	// Expected values. The release should have, at the end of the business
	// logic, a list of clusters containing all clusters we've added to
	// the client in alphabetical order.
	expected := release.DeepCopy()
	expected.Annotations[shipperV1.ReleaseClustersAnnotation] = clusterA.GetName() + "," + clusterB.GetName()

	relWithConditions := expected.DeepCopy()

	condition := releaseutil.NewReleaseCondition(shipperV1.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&relWithConditions.Status, *condition)

	expectedActions := []kubetesting.Action{
		kubetesting.NewUpdateAction(
			shipperV1.SchemeGroupVersion.WithResource("releases"),
			release.GetNamespace(),
			expected),
		kubetesting.NewUpdateAction(
			shipperV1.SchemeGroupVersion.WithResource("releases"),
			release.GetNamespace(),
			relWithConditions),
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

// TestScheduleSkipsDisabled tests the first part of the cluster scheduling,
// which is find out which clusters the release must be installed, and
// persisting it under .status.environment.clusters but skipping disabled
// clusters this time.
func TestScheduleSkipsDisabled(t *testing.T) {
	// Fixtures
	clusterA := buildCluster("minikube-a")
	clusterB := buildCluster("minikube-b")
	clusterB.Spec.Scheduler.Disabled = true
	release := buildRelease()
	fixtures := []runtime.Object{clusterA, clusterB, release}

	// Expected values. The release should have, at the end of the business
	// logic, a list of clusters containing the schedulable cluster we've
	// added to the client.
	expected := release.DeepCopy()
	expected.Annotations[shipperV1.ReleaseClustersAnnotation] = clusterA.GetName()

	relWithConditions := expected.DeepCopy()

	condition := releaseutil.NewReleaseCondition(shipperV1.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&relWithConditions.Status, *condition)

	expectedActions := []kubetesting.Action{
		kubetesting.NewUpdateAction(
			shipperV1.SchemeGroupVersion.WithResource("releases"),
			release.GetNamespace(),
			expected),
		kubetesting.NewUpdateAction(
			shipperV1.SchemeGroupVersion.WithResource("releases"),
			release.GetNamespace(),
			relWithConditions),
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
	capacityTarget := &shipperV1.CapacityTarget{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      release.Name,
			Namespace: ns,
			OwnerReferences: []metaV1.OwnerReference{
				{
					APIVersion: release.APIVersion,
					Kind:       release.Kind,
					Name:       release.Name,
					UID:        release.UID,
				},
			},
		},
		Spec: shipperV1.CapacityTargetSpec{
			Clusters: []shipperV1.ClusterCapacityTarget{
				{
					Name:              "minikube-a",
					Percent:           0,
					TotalReplicaCount: 12,
				},
			},
		},
	}

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
			capacityTarget,
		),
		kubetesting.NewUpdateAction(
			shipperV1.SchemeGroupVersion.WithResource("releases"),
			ns,
			release),
	}
	return actions
}

func TestCreateAssociatedObjects(t *testing.T) {
	// Fixtures
	cluster := buildCluster("minikube-a")
	release := buildRelease()
	release.Annotations[shipperV1.ReleaseClustersAnnotation] = cluster.GetName()
	fixtures := []runtime.Object{release, cluster}

	// Expected release and actions. The release should have, at the end of
	// the business logic, a list of clusters containing the sole cluster
	// we've added to the client, and also a Scheduled condition with True
	// status. Expected actions contain the intent to create all the
	// associated target objects.
	expected := release.DeepCopy()
	expected.Status.Conditions = []shipperV1.ReleaseCondition{
		{Type: shipperV1.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
	}
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
	cluster := buildCluster("minikube-a")
	release := buildRelease()
	release.Annotations[shipperV1.ReleaseClustersAnnotation] = cluster.GetName()

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
	expected.Status.Conditions = []shipperV1.ReleaseCondition{
		{Type: shipperV1.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
	}
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
	cluster := buildCluster("minikube-a")
	release := buildRelease()
	release.Annotations[shipperV1.ReleaseClustersAnnotation] = cluster.GetName()

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
	expected.Status.Conditions = []shipperV1.ReleaseCondition{
		{Type: shipperV1.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
	}
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
	cluster := buildCluster("minikube-a")
	release := buildRelease()
	release.Annotations[shipperV1.ReleaseClustersAnnotation] = cluster.GetName()

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
	expected.Status.Conditions = []shipperV1.ReleaseCondition{
		{Type: shipperV1.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
	}
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

type requirements shipperV1.ClusterRequirements
type clusters []shipperV1.ClusterSpec
type expected []string

const (
	passingCase = false
	errorCase   = true
)

func pint32(i int32) *int32 {
	return &i
}

func pstr(s string) *string {
	return &s
}

// TestComputeTargetClusters works the core of the scheduler logic: matching regions and capabilities between releases and clusters
// NOTE: the "expected" clusters are due to the particular prefList outcomes,
// and as such should be expected to break if we change the hash function for the
// preflist.
func TestComputeTargetClusters(t *testing.T) {
	computeClusterTestCase(t, "basic region match",
		requirements{
			Regions: []shipperV1.RegionRequirement{{Name: "matches"}},
		},
		clusters{
			{Region: "matches", Capabilities: []string{}},
		},
		expected{"cluster-0"},
		passingCase,
	)

	computeClusterTestCase(t, "one region match one no match",
		requirements{
			Regions: []shipperV1.RegionRequirement{{Name: "matches"}},
		},
		clusters{
			{Region: "matches", Capabilities: []string{}},
			{Region: "does not match", Capabilities: []string{}},
		},
		expected{"cluster-0"},
		passingCase,
	)

	computeClusterTestCase(t, "both match",
		requirements{
			Regions: []shipperV1.RegionRequirement{{Name: "matches", Replicas: pint32(2)}},
		},
		clusters{
			{Region: "matches", Capabilities: []string{}},
			{Region: "matches", Capabilities: []string{}},
		},
		expected{"cluster-0", "cluster-1"},
		passingCase,
	)

	computeClusterTestCase(t, "two region matches, one capability match",
		requirements{
			Regions:      []shipperV1.RegionRequirement{{Name: "matches"}},
			Capabilities: []string{"a", "b"},
		},
		clusters{
			{Region: "matches", Capabilities: []string{"a"}},
			{Region: "matches", Capabilities: []string{"a", "b"}},
		},
		expected{"cluster-1"},
		passingCase,
	)

	computeClusterTestCase(t, "two region matches, two capability matches",
		requirements{
			Regions:      []shipperV1.RegionRequirement{{Name: "matches", Replicas: pint32(2)}},
			Capabilities: []string{"a"},
		},
		clusters{
			{Region: "matches", Capabilities: []string{"a"}},
			{Region: "matches", Capabilities: []string{"a", "b"}},
		},
		expected{"cluster-0", "cluster-1"},
		passingCase,
	)

	computeClusterTestCase(t, "no region match",
		requirements{
			Regions:      []shipperV1.RegionRequirement{{Name: "foo"}},
			Capabilities: []string{},
		},
		clusters{
			{Region: "bar", Capabilities: []string{}},
			{Region: "baz", Capabilities: []string{}},
		},
		expected{},
		errorCase,
	)

	computeClusterTestCase(t, "region match, no capability match",
		requirements{
			Regions:      []shipperV1.RegionRequirement{{Name: "foo"}},
			Capabilities: []string{"a"},
		},
		clusters{
			{Region: "foo", Capabilities: []string{"b"}},
			{Region: "foo", Capabilities: []string{"b"}},
		},
		expected{},
		errorCase,
	)

	computeClusterTestCase(t, "reject duplicate capabilities in requirements",
		requirements{
			Regions:      []shipperV1.RegionRequirement{{Name: "foo"}},
			Capabilities: []string{"a", "a"},
		},
		clusters{
			{Region: "foo", Capabilities: []string{"a"}},
		},
		expected{},
		errorCase,
	)

	computeClusterTestCase(t, "more clusters than needed, pick only one from each region",
		requirements{
			Regions: []shipperV1.RegionRequirement{
				{Name: "us-east", Replicas: pint32(1)},
				{Name: "eu-west", Replicas: pint32(1)},
			},
			Capabilities: []string{"a"},
		},
		clusters{
			{Region: "us-east", Capabilities: []string{"a"}},
			{Region: "us-east", Capabilities: []string{"a"}},
			{Region: "eu-west", Capabilities: []string{"a"}},
			{Region: "eu-west", Capabilities: []string{"a"}},
		},
		expected{"cluster-1", "cluster-2"},
		passingCase,
	)

	computeClusterTestCase(t, "different replica counts by region",
		requirements{
			Regions: []shipperV1.RegionRequirement{
				{Name: "us-east", Replicas: pint32(2)},
				{Name: "eu-west", Replicas: pint32(1)},
			},
			Capabilities: []string{"a"},
		},
		clusters{
			{Region: "us-east", Capabilities: []string{"a"}},
			{Region: "us-east", Capabilities: []string{"a"}},
			{Region: "eu-west", Capabilities: []string{"a"}},
			{Region: "eu-west", Capabilities: []string{"a"}},
		},
		expected{"cluster-0", "cluster-1", "cluster-2"},
		passingCase,
	)

	computeClusterTestCase(t, "skip disabled clusters",
		requirements{
			Regions: []shipperV1.RegionRequirement{
				{Name: "us-east", Replicas: pint32(2)},
			},
		},
		clusters{
			{
				Region:    "us-east",
				Scheduler: shipperV1.ClusterSchedulerSettings{Disabled: true},
			},
			{
				Region:    "us-east",
				Scheduler: shipperV1.ClusterSchedulerSettings{Disabled: true},
			},
			{Region: "us-east"},
		},
		expected{},
		errorCase,
	)

	computeClusterTestCase(t, "heavy weight changes normal priority",
		requirements{
			Regions: []shipperV1.RegionRequirement{
				{Name: "us-east", Replicas: pint32(1)},
				{Name: "eu-west", Replicas: pint32(1)},
			},
			Capabilities: []string{"a"},
		},
		clusters{
			{
				Region:       "us-east",
				Capabilities: []string{"a"},
				Scheduler:    shipperV1.ClusterSchedulerSettings{Weight: pint32(900)},
			},
			{Region: "us-east", Capabilities: []string{"a"}},
			{Region: "eu-west", Capabilities: []string{"a"}},
			{
				Region:       "eu-west",
				Capabilities: []string{"a"},
				Scheduler:    shipperV1.ClusterSchedulerSettings{Weight: pint32(900)},
			},
		},
		// this test is identical to "more clusters than needed", and without weight would yield the same result (cluster-1, cluster-2)
		expected{"cluster-0", "cluster-3"},
		passingCase,
	)

	computeClusterTestCase(t, "a little weight doesn't change things",
		requirements{
			Regions: []shipperV1.RegionRequirement{
				{Name: "us-east", Replicas: pint32(1)},
				{Name: "eu-west", Replicas: pint32(1)},
			},
			Capabilities: []string{"a"},
		},
		clusters{
			{
				Region:       "us-east",
				Capabilities: []string{"a"},
				Scheduler:    shipperV1.ClusterSchedulerSettings{Weight: pint32(101)},
			},
			{Region: "us-east", Capabilities: []string{"a"}},
			{Region: "eu-west", Capabilities: []string{"a"}},
			{Region: "eu-west", Capabilities: []string{"a"}},
		},
		// weight doesn't change things unless it is "heavy" enough: it needs
		// to overcome the natural distribution of hash values. This test is
		// identical to "more clusters than needed", and has a minimal
		// (ineffectual) weight applied, so it gives the same result as that test.
		expected{"cluster-1", "cluster-2"},
		passingCase,
	)

	computeClusterTestCase(t, "colliding identity plus a little weight does change things",
		requirements{
			Regions: []shipperV1.RegionRequirement{
				{Name: "us-east", Replicas: pint32(1)},
				{Name: "eu-west", Replicas: pint32(1)},
			},
			Capabilities: []string{"a"},
		},
		clusters{
			// the "identity" means that cluster-0 computes the hash exactly like
			// cluster-1, so a minimal bump in weight puts it in front
			{
				Region:       "us-east",
				Capabilities: []string{"a"},
				Scheduler: shipperV1.ClusterSchedulerSettings{
					Identity: pstr("cluster-1"),
					Weight:   pint32(101),
				},
			},
			{Region: "us-east", Capabilities: []string{"a"}},
			{Region: "eu-west", Capabilities: []string{"a"}},
			{Region: "eu-west", Capabilities: []string{"a"}},
		},
		expected{"cluster-0", "cluster-2"},
		passingCase,
	)
}

func computeClusterTestCase(
	t *testing.T,
	name string,
	reqs requirements,
	clusterSpecs clusters,
	expectedClusters expected,
	expectError bool,
) {

	release := generateReleaseForTestCase(shipperV1.ClusterRequirements(reqs))
	clusters := make([]*shipperV1.Cluster, 0, len(clusterSpecs))
	for i, spec := range clusterSpecs {
		clusters = append(clusters, generateClusterForTestCase(i, spec))
	}

	actualClusters, err := computeTargetClusters(release, clusters)
	if expectError {
		if err == nil {
			t.Errorf("test %q expected an error but didn't get one!", name)
		}
	} else {
		if err != nil {
			t.Errorf("error %q: %q", name, err)
			return
		}
	}

	if strings.Join(expectedClusters, ",") != strings.Join(actualClusters, ",") {
		t.Errorf("%q expected clusters %q, but got %q", name, strings.Join(expectedClusters, ","), strings.Join(actualClusters, ","))
		return
	}
}

func generateClusterForTestCase(name int, spec shipperV1.ClusterSpec) *shipperV1.Cluster {
	return &shipperV1.Cluster{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      fmt.Sprintf("cluster-%d", name),
			Namespace: shippertesting.TestNamespace,
		},
		Spec: spec,
	}
}

func generateReleaseForTestCase(reqs shipperV1.ClusterRequirements) *shipperV1.Release {
	return &shipperV1.Release{
		ReleaseMeta: shipperV1.ReleaseMeta{
			ObjectMeta: metaV1.ObjectMeta{
				Name:      "test-release",
				Namespace: shippertesting.TestNamespace,
				OwnerReferences: []metaV1.OwnerReference{
					{
						APIVersion: "shipper.booking.com/v1",
						Kind:       "Application",
						Name:       "test-application",
					},
				},
			},
			Environment: shipperV1.ReleaseEnvironment{
				ClusterRequirements: reqs,
			},
		},
	}
}
