package schedulecontroller

import (
	"fmt"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
	helmchart "k8s.io/helm/pkg/proto/hapi/chart"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shipperchart "github.com/bookingcom/shipper/pkg/chart"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

var chartFetchFunc = shipperchart.FetchRemoteWithCache("testdata/chart-cache", chart.DefaultCacheLimit)

func init() {
	releaseutil.ConditionsShouldDiscardTimestamps = true
}

func buildRelease() *shipperv1.Release {
	return &shipperv1.Release{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "shipper.booking.com/v1",
			Kind:       "Release",
		},
		ReleaseMeta: shipperv1.ReleaseMeta{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "test-release",
				Namespace:   shippertesting.TestNamespace,
				Annotations: map[string]string{},
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "shipper.booking.com/v1",
						Kind:       "Application",
						Name:       "test-application",
					},
				},
			},
			Environment: shipperv1.ReleaseEnvironment{
				Chart: shipperv1.Chart{
					Name:    "reviews-api",
					Version: "0.0.1",
					RepoURL: chartRepoURL,
				},
				ClusterRequirements: shipperv1.ClusterRequirements{
					Regions: []shipperv1.RegionRequirement{{Name: shippertesting.TestRegion}},
				},
			},
		},
	}
}

func buildCluster(name string) *shipperv1.Cluster {
	return &shipperv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: shipperv1.ClusterSpec{
			APIMaster:    "https://127.0.0.1",
			Capabilities: []string{},
			Region:       shippertesting.TestRegion,
		},
	}
}

func newScheduler(
	release *shipperv1.Release,
	fixtures []runtime.Object,
	fetchFn shipperchart.FetchFunc,
) (*Scheduler, *shipperfake.Clientset) {
	clientset := shipperfake.NewSimpleClientset(fixtures...)
	informerFactory := shipperinformers.NewSharedInformerFactory(clientset, time.Millisecond*0)
	clustersLister := informerFactory.Shipper().V1().Clusters().Lister()

	c := NewScheduler(release, clientset, clustersLister, fetchFn, record.NewFakeRecorder(42))

	stopCh := make(chan struct{})
	defer close(stopCh)

	informerFactory.Start(stopCh)
	informerFactory.WaitForCacheSync(stopCh)

	return c, clientset
}

// TestSchedule tests the first part of the cluster scheduling, which is find
// out which clusters the release must be installed, and persisting it under
// .status.environment.clusters.
func TestSchedule(t *testing.T) {
	// Fixtures
	clusterA := buildCluster("minikube-a")
	clusterB := buildCluster("minikube-b")
	release := buildRelease()
	release.Environment.Chart.RepoURL = "localhost"
	fixtures := []runtime.Object{clusterA, clusterB, release}
	// Demand two clusters.
	release.Environment.ClusterRequirements.Regions[0].Replicas = pint32(2)

	// Expected values. The release should have, at the end of the business
	// logic, a list of clusters containing all clusters we've added to
	// the client in alphabetical order.
	expected := release.DeepCopy()
	expected.Annotations[shipperv1.ReleaseClustersAnnotation] = clusterA.GetName() + "," + clusterB.GetName()

	relWithConditions := expected.DeepCopy()

	condition := releaseutil.NewReleaseCondition(shipperv1.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&relWithConditions.Status, *condition)

	expectedActions := []kubetesting.Action{
		kubetesting.NewUpdateAction(
			shipperv1.SchemeGroupVersion.WithResource("releases"),
			release.GetNamespace(),
			expected),
		kubetesting.NewUpdateAction(
			shipperv1.SchemeGroupVersion.WithResource("releases"),
			release.GetNamespace(),
			relWithConditions),
	}

	c, clientset := newScheduler(release, fixtures, chartFetchFunc)
	if err := c.scheduleRelease(); err != nil {
		t.Fatal(err)
	}

	filteredActions := filterActions(clientset.Actions(), []string{"update"}, []string{"releases"})
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

// TestScheduleSkipsUnschedulable tests the first part of the cluster
// scheduling, which is find out which clusters the release must be installed,
// and persisting it under .status.environment.clusters but skipping
// unschedulable clusters this time.
func TestScheduleSkipsUnschedulable(t *testing.T) {
	// Fixtures
	clusterA := buildCluster("minikube-a")
	clusterB := buildCluster("minikube-b")
	clusterB.Spec.Scheduler.Unschedulable = true
	release := buildRelease()
	release.Environment.Chart.RepoURL = "localhost"
	fixtures := []runtime.Object{clusterA, clusterB, release}

	// The release should have, at the end of the business logic, a list of
	// clusters containing the schedulable cluster we've added to the client.
	expected := release.DeepCopy()
	expected.Annotations[shipperv1.ReleaseClustersAnnotation] = clusterA.GetName()

	relWithConditions := expected.DeepCopy()

	condition := releaseutil.NewReleaseCondition(shipperv1.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&relWithConditions.Status, *condition)

	expectedActions := []kubetesting.Action{
		kubetesting.NewUpdateAction(
			shipperv1.SchemeGroupVersion.WithResource("releases"),
			release.GetNamespace(),
			expected),
		kubetesting.NewUpdateAction(
			shipperv1.SchemeGroupVersion.WithResource("releases"),
			release.GetNamespace(),
			relWithConditions),
	}

	c, clientset := newScheduler(release, fixtures, chartFetchFunc)
	if err := c.scheduleRelease(); err != nil {
		t.Fatal(err)
	}

	filteredActions := filterActions(clientset.Actions(), []string{"update"}, []string{"releases"})
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

func buildExpectedActions(ns string, release *shipperv1.Release) []kubetesting.Action {
	installationTarget := &shipperv1.InstallationTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      release.Name,
			Namespace: ns,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: release.APIVersion,
					Kind:       release.Kind,
					Name:       release.Name,
					UID:        release.UID,
				},
			},
		},
		Spec: shipperv1.InstallationTargetSpec{
			Clusters: []string{"minikube-a"},
		},
	}

	capacityTarget := &shipperv1.CapacityTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      release.Name,
			Namespace: ns,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: release.APIVersion,
					Kind:       release.Kind,
					Name:       release.Name,
					UID:        release.UID,
				},
			},
		},
		Spec: shipperv1.CapacityTargetSpec{
			Clusters: []shipperv1.ClusterCapacityTarget{
				{
					Name:              "minikube-a",
					Percent:           0,
					TotalReplicaCount: 1,
				},
			},
		},
	}

	trafficTarget := &shipperv1.TrafficTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      release.Name,
			Namespace: ns,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: release.APIVersion,
					Kind:       release.Kind,
					Name:       release.Name,
					UID:        release.UID,
				},
			},
		},
		Spec: shipperv1.TrafficTargetSpec{
			Clusters: []shipperv1.ClusterTrafficTarget{
				shipperv1.ClusterTrafficTarget{
					Name: "minikube-a",
				},
			},
		},
	}

	actions := []kubetesting.Action{
		kubetesting.NewCreateAction(
			shipperv1.SchemeGroupVersion.WithResource("installationtargets"),
			ns,
			installationTarget),
		kubetesting.NewCreateAction(
			shipperv1.SchemeGroupVersion.WithResource("traffictargets"),
			ns,
			trafficTarget),
		kubetesting.NewCreateAction(
			shipperv1.SchemeGroupVersion.WithResource("capacitytargets"),
			ns,
			capacityTarget,
		),
		kubetesting.NewUpdateAction(
			shipperv1.SchemeGroupVersion.WithResource("releases"),
			ns,
			release),
	}
	return actions
}

func TestCreateAssociatedObjects(t *testing.T) {
	cluster := buildCluster("minikube-a")
	release := buildRelease()
	release.Environment.Chart.RepoURL = "localhost"
	release.Annotations[shipperv1.ReleaseClustersAnnotation] = cluster.GetName()
	fixtures := []runtime.Object{release, cluster}

	// Expected release and actions. The release should have, at the end of the
	// business logic, a list of clusters containing the sole cluster we've added
	// to the client, and also a Scheduled condition with True status. Expected
	// actions contain the intent to create all the associated target objects.
	expected := release.DeepCopy()
	expected.Status.Conditions = []shipperv1.ReleaseCondition{
		{Type: shipperv1.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
	}
	expectedActions := buildExpectedActions(release.GetNamespace(), expected)

	c, clientset := newScheduler(release, fixtures, chartFetchFunc)
	if err := c.scheduleRelease(); err != nil {
		t.Fatal(err)
	}

	filteredActions := filterActions(
		clientset.Actions(),
		[]string{"update", "create"},
		[]string{"releases", "installationtargets", "traffictargets", "capacitytargets"},
	)
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

func TestCreateAssociatedObjectsDuplicateInstallationTarget(t *testing.T) {
	cluster := buildCluster("minikube-a")
	release := buildRelease()
	release.Environment.Chart.RepoURL = "localhost"
	release.Annotations[shipperv1.ReleaseClustersAnnotation] = cluster.GetName()

	installationtarget := &shipperv1.InstallationTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      release.GetName(),
			Namespace: release.GetNamespace(),
		},
	}
	fixtures := []runtime.Object{release, cluster, installationtarget}

	// Expected release and actions. Even with an existing installationtarget
	// object for this release, at the end of the business logic the expected
	// release should have its .status.phase set to "WaitingForStrategy". Expected
	// actions contain the intent to create all the associated target objects.
	expected := release.DeepCopy()
	expected.Status.Conditions = []shipperv1.ReleaseCondition{
		{Type: shipperv1.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
	}
	expectedActions := buildExpectedActions(release.GetNamespace(), expected)

	c, clientset := newScheduler(release, fixtures, chartFetchFunc)
	if err := c.scheduleRelease(); err != nil {
		t.Fatal(err)
	}

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
	release.Environment.Chart.RepoURL = "localhost"
	release.Annotations[shipperv1.ReleaseClustersAnnotation] = cluster.GetName()

	traffictarget := &shipperv1.TrafficTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      release.GetName(),
			Namespace: release.GetNamespace(),
		},
	}
	fixtures := []runtime.Object{cluster, release, traffictarget}

	// Expected release and actions. Even with an existing installationtarget
	// object for this release, at the end of the business logic the expected
	// release should have its .status.phase set to "WaitingForStrategy". Expected
	// actions contain the intent to create all the associated target objects.
	expected := release.DeepCopy()
	expected.Status.Conditions = []shipperv1.ReleaseCondition{
		{Type: shipperv1.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
	}
	expectedActions := buildExpectedActions(release.GetNamespace(), expected)

	c, clientset := newScheduler(release, fixtures, chartFetchFunc)
	if err := c.scheduleRelease(); err != nil {
		t.Fatal(err)
	}

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
	release.Environment.Chart.RepoURL = "localhost"
	release.Annotations[shipperv1.ReleaseClustersAnnotation] = cluster.GetName()

	capacitytarget := &shipperv1.CapacityTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      release.GetName(),
			Namespace: release.GetNamespace(),
		},
	}
	fixtures := []runtime.Object{cluster, release, capacitytarget}

	// Expected release and actions. Even with an existing capacitytarget object
	// for this release, at the end of the business logic the expected release
	// should have its .status.phase set to "WaitingForStrategy". Expected actions
	// contain the intent to create all the associated target objects.
	expected := release.DeepCopy()
	expected.Status.Conditions = []shipperv1.ReleaseCondition{
		{Type: shipperv1.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
	}
	expectedActions := buildExpectedActions(release.GetNamespace(), expected)

	c, clientset := newScheduler(release, fixtures, chartFetchFunc)
	if err := c.scheduleRelease(); err != nil {
		t.Fatal(err)
	}

	actions := filterActions(
		clientset.Actions(),
		[]string{"update", "create"},
		[]string{"releases", "installationtargets", "traffictargets", "capacitytargets"},
	)
	shippertesting.CheckActions(expectedActions, actions, t)
}

func TestChartBadSyntax(t *testing.T) {
	cluster := buildCluster("minikube-a")
	release := buildRelease()
	release.Annotations[shipperv1.ReleaseClustersAnnotation] = cluster.Name
	release.Environment.Chart.Version = "0.0.1-broken-k8s-objects"
	release.Environment.Chart.RepoURL = "localhost"

	fixtures := []runtime.Object{cluster, release}

	expectedRelease := release.DeepCopy()
	expectedRelease.Status.Conditions = []shipperv1.ReleaseCondition{
		{Type: shipperv1.ReleaseConditionTypeChart, Status: corev1.ConditionFalse, Reason: releaseutil.BadSyntaxReason, Message: ""},
		{Type: shipperv1.ReleaseConditionTypeScheduled, Status: corev1.ConditionFalse, Reason: "ChartNotReachable", Message: ""},
	}

	c, clientset := newScheduler(release, fixtures, chartFetchFunc)
	if err := c.scheduleRelease(); err == nil {
		t.Fatal(fmt.Errorf(`should return an error here`))
	} else if !IsBrokenChartError(err) {
		t.Fatal(fmt.Errorf(`must be a broken chart failure: %v`, err))
	} else {
		// Instead of using the release conditions API, stuff the message in
		// the proper condition.
		expectedRelease.Status.Conditions[0].Message = err.Error()
	}

	expectedActions := []kubetesting.Action{
		kubetesting.NewUpdateAction(
			shipperv1.SchemeGroupVersion.WithResource("releases"),
			release.GetNamespace(),
			expectedRelease),
	}

	filteredActions := filterActions(
		clientset.Actions(),
		[]string{"update"},
		[]string{"releases"},
	)

	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

func TestChartBadObjects(t *testing.T) {
	cluster := buildCluster("minikube-a")
	release := buildRelease()
	release.Environment.Chart.RepoURL = "localhost"
	release.Annotations[shipperv1.ReleaseClustersAnnotation] = cluster.Name
	release.Environment.Chart.Version = "0.0.1-missing-labels"
	release.Environment.Chart.RepoURL = "localhost"

	fixtures := []runtime.Object{cluster, release}

	expectedRelease := release.DeepCopy()
	expectedRelease.Status.Conditions = []shipperv1.ReleaseCondition{
		{Type: shipperv1.ReleaseConditionTypeChart, Status: corev1.ConditionFalse, Reason: releaseutil.BadObjectsReason, Message: ""},
		{Type: shipperv1.ReleaseConditionTypeScheduled, Status: corev1.ConditionFalse, Reason: "ChartNotReachable", Message: ""},
	}

	c, clientset := newScheduler(release, fixtures, chartFetchFunc)
	if err := c.scheduleRelease(); err == nil {
		t.Fatal(fmt.Errorf(`should return an error here`))
	} else if !IsBrokenChartError(err) {
		t.Fatal(fmt.Errorf(`must be a broken chart failure: %v`, err))
	} else {
		// Instead of using the release conditions API, stuff the message in
		// the proper condition.
		expectedRelease.Status.Conditions[0].Message = err.Error()
	}

	expectedActions := []kubetesting.Action{
		kubetesting.NewUpdateAction(
			shipperv1.SchemeGroupVersion.WithResource("releases"),
			release.GetNamespace(),
			expectedRelease),
	}

	filteredActions := filterActions(
		clientset.Actions(),
		[]string{"update"},
		[]string{"releases"},
	)

	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

func TestChartNotReachable(t *testing.T) {
	cluster := buildCluster("minikube-a")
	release := buildRelease()
	release.Annotations[shipperv1.ReleaseClustersAnnotation] = cluster.Name

	fixtures := []runtime.Object{cluster, release}

	var chartUnreachableFunc shipperchart.FetchFunc = func(chart shipperv1.Chart) (*helmchart.Chart, error) {
		// Return some error when fetching the chart from the repository.
		return nil, fmt.Errorf("chart repository is unreachable")
	}

	expectedRelease := release.DeepCopy()
	expectedRelease.Status.Conditions = []shipperv1.ReleaseCondition{
		{Type: shipperv1.ReleaseConditionTypeChart, Status: corev1.ConditionFalse, Reason: releaseutil.NotReachableReason, Message: ""},
		{Type: shipperv1.ReleaseConditionTypeScheduled, Status: corev1.ConditionFalse, Reason: "ChartNotReachable", Message: ""},
	}

	c, clientset := newScheduler(release, fixtures, chartUnreachableFunc)
	if err := c.scheduleRelease(); err == nil {
		t.Fatal(fmt.Errorf(`should return an error here`))
	} else if !IsChartFetchFailureError(err) {
		t.Fatal(fmt.Errorf(`must be a chart fetch failure: %v`, err))
	} else {
		// Instead of using the release conditions API, stuff the message in
		// the proper condition.
		expectedRelease.Status.Conditions[0].Message = err.Error()
	}

	expectedActions := []kubetesting.Action{
		kubetesting.NewUpdateAction(
			shipperv1.SchemeGroupVersion.WithResource("releases"),
			release.GetNamespace(),
			expectedRelease),
	}

	filteredActions := filterActions(
		clientset.Actions(),
		[]string{"update"},
		[]string{"releases"},
	)

	shippertesting.CheckActions(expectedActions, filteredActions, t)
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

type requirements shipperv1.ClusterRequirements
type clusters []shipperv1.ClusterSpec
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

// TestComputeTargetClusters works the core of the scheduler logic: matching
// regions and capabilities between releases and clusters NOTE: the "expected"
// clusters are due to the particular prefList outcomes, and as such should be
// expected to break if we change the hash function for the preflist.
func TestComputeTargetClusters(t *testing.T) {
	computeClusterTestCase(t, "error when no regions specified",
		requirements{
			Regions: []shipperv1.RegionRequirement{},
		},
		clusters{
			{Region: shippertesting.TestRegion, Capabilities: []string{}},
		},
		expected{},
		errorCase,
	)

	computeClusterTestCase(t, "basic region match",
		requirements{
			Regions: []shipperv1.RegionRequirement{{Name: "matches"}},
		},
		clusters{
			{Region: "matches", Capabilities: []string{}},
		},
		expected{"cluster-0"},
		passingCase,
	)

	computeClusterTestCase(t, "one region match one no match",
		requirements{
			Regions: []shipperv1.RegionRequirement{{Name: "matches"}},
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
			Regions: []shipperv1.RegionRequirement{{Name: "matches", Replicas: pint32(2)}},
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
			Regions:      []shipperv1.RegionRequirement{{Name: "matches"}},
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
			Regions:      []shipperv1.RegionRequirement{{Name: "matches", Replicas: pint32(2)}},
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
			Regions:      []shipperv1.RegionRequirement{{Name: "foo"}},
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
			Regions:      []shipperv1.RegionRequirement{{Name: "foo"}},
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
			Regions:      []shipperv1.RegionRequirement{{Name: "foo"}},
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
			Regions: []shipperv1.RegionRequirement{
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
			Regions: []shipperv1.RegionRequirement{
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

	computeClusterTestCase(t, "skip unschedulable clusters",
		requirements{
			Regions: []shipperv1.RegionRequirement{
				{Name: "us-east", Replicas: pint32(2)},
			},
		},
		clusters{
			{
				Region:    "us-east",
				Scheduler: shipperv1.ClusterSchedulerSettings{Unschedulable: true},
			},
			{
				Region:    "us-east",
				Scheduler: shipperv1.ClusterSchedulerSettings{Unschedulable: true},
			},
			{Region: "us-east"},
		},
		expected{},
		errorCase,
	)

	computeClusterTestCase(t, "heavy weight changes normal priority",
		requirements{
			Regions: []shipperv1.RegionRequirement{
				{Name: "us-east", Replicas: pint32(1)},
				{Name: "eu-west", Replicas: pint32(1)},
			},
			Capabilities: []string{"a"},
		},
		clusters{
			{
				Region:       "us-east",
				Capabilities: []string{"a"},
				Scheduler:    shipperv1.ClusterSchedulerSettings{Weight: pint32(900)},
			},
			{Region: "us-east", Capabilities: []string{"a"}},
			{Region: "eu-west", Capabilities: []string{"a"}},
			{
				Region:       "eu-west",
				Capabilities: []string{"a"},
				Scheduler:    shipperv1.ClusterSchedulerSettings{Weight: pint32(900)},
			},
		},
		// This test is identical to "more clusters than needed", and without weight
		// would yield the same result (cluster-1, cluster-2).
		expected{"cluster-0", "cluster-3"},
		passingCase,
	)

	computeClusterTestCase(t, "a little weight doesn't change things",
		requirements{
			Regions: []shipperv1.RegionRequirement{
				{Name: "us-east", Replicas: pint32(1)},
				{Name: "eu-west", Replicas: pint32(1)},
			},
			Capabilities: []string{"a"},
		},
		clusters{
			{
				Region:       "us-east",
				Capabilities: []string{"a"},
				Scheduler:    shipperv1.ClusterSchedulerSettings{Weight: pint32(101)},
			},
			{Region: "us-east", Capabilities: []string{"a"}},
			{Region: "eu-west", Capabilities: []string{"a"}},
			{Region: "eu-west", Capabilities: []string{"a"}},
		},
		// Weight doesn't change things unless it is "heavy" enough: it needs to
		// overcome the natural distribution of hash values. This test is identical to
		// "more clusters than needed", and has a minimal (ineffectual) weight
		// applied, so it gives the same result as that test.
		expected{"cluster-1", "cluster-2"},
		passingCase,
	)

	computeClusterTestCase(t, "colliding identity plus a little weight does change things",
		requirements{
			Regions: []shipperv1.RegionRequirement{
				{Name: "us-east", Replicas: pint32(1)},
				{Name: "eu-west", Replicas: pint32(1)},
			},
			Capabilities: []string{"a"},
		},
		clusters{
			// The "identity" means that cluster-0 computes the hash exactly like
			// cluster-1, so a minimal bump in weight puts it in front.
			{
				Region:       "us-east",
				Capabilities: []string{"a"},
				Scheduler: shipperv1.ClusterSchedulerSettings{
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

	release := generateReleaseForTestCase(shipperv1.ClusterRequirements(reqs))
	clusters := make([]*shipperv1.Cluster, 0, len(clusterSpecs))
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

func generateClusterForTestCase(name int, spec shipperv1.ClusterSpec) *shipperv1.Cluster {
	return &shipperv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("cluster-%d", name),
			Namespace: shippertesting.TestNamespace,
		},
		Spec: spec,
	}
}

func generateReleaseForTestCase(reqs shipperv1.ClusterRequirements) *shipperv1.Release {
	return &shipperv1.Release{
		ReleaseMeta: shipperv1.ReleaseMeta{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-release",
				Namespace: shippertesting.TestNamespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "shipper.booking.com/v1",
						Kind:       "Application",
						Name:       "test-application",
					},
				},
			},
			Environment: shipperv1.ReleaseEnvironment{
				ClusterRequirements: reqs,
			},
		},
	}
}
