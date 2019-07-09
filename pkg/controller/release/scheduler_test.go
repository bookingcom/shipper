package release

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"path"
	"sort"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
	"k8s.io/helm/pkg/chartutil"
	helmchart "k8s.io/helm/pkg/proto/hapi/chart"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

func init() {
	releaseutil.ConditionsShouldDiscardTimestamps = true
}

var localFetchChart = func(chartspec *shipper.Chart) (*helmchart.Chart, error) {
	data, err := ioutil.ReadFile(
		path.Join(
			"testdata",
			fmt.Sprintf("%s-%s.tgz", chartspec.Name, chartspec.Version),
		))
	if err != nil {
		return nil, err
	}
	buf := bytes.NewBuffer(data)

	return chartutil.LoadArchive(buf)
}

func buildRelease() *shipper.Release {
	return &shipper.Release{
		TypeMeta: metav1.TypeMeta{
			APIVersion: shipper.SchemeGroupVersion.String(),
			Kind:       "Release",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-release",
			Namespace:   shippertesting.TestNamespace,
			Annotations: map[string]string{},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: shipper.SchemeGroupVersion.String(),
					Kind:       "Application",
					Name:       "test-application",
				},
			},
			Labels: map[string]string{
				shipper.ReleaseLabel: "test-release",
				shipper.AppLabel:     "test-application",
			},
		},
		Spec: shipper.ReleaseSpec{
			Environment: shipper.ReleaseEnvironment{
				Chart: shipper.Chart{
					Name:    "simple",
					Version: "0.0.1",
				},
				ClusterRequirements: shipper.ClusterRequirements{
					Regions: []shipper.RegionRequirement{{Name: shippertesting.TestRegion}},
				},
			},
		},
	}
}

func buildAssociatedObjects(release *shipper.Release, clusters []*shipper.Cluster) (*shipper.InstallationTarget, *shipper.TrafficTarget, *shipper.CapacityTarget) {

	clusterNames := make([]string, 0, len(clusters))
	for _, cluster := range clusters {
		clusterNames = append(clusterNames, cluster.GetName())
	}
	sort.Strings(clusterNames)

	installationTarget := &shipper.InstallationTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      release.GetName(),
			Namespace: release.GetNamespace(),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: release.APIVersion,
					Kind:       release.Kind,
					Name:       release.Name,
					UID:        release.UID,
				},
			},
			Labels: map[string]string{
				shipper.AppLabel:     release.OwnerReferences[0].Name,
				shipper.ReleaseLabel: release.GetName(),
			},
		},
		Spec: shipper.InstallationTargetSpec{
			Clusters:    clusterNames,
			CanOverride: true,
			Chart:       release.Spec.Environment.Chart.DeepCopy(),
			Values:      release.Spec.Environment.Values,
		},
	}

	clusterTrafficTargets := make([]shipper.ClusterTrafficTarget, 0, len(clusters))
	for _, cluster := range clusters {
		clusterTrafficTargets = append(
			clusterTrafficTargets,
			shipper.ClusterTrafficTarget{
				Name: cluster.GetName(),
			})
	}

	trafficTarget := &shipper.TrafficTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      release.Name,
			Namespace: release.GetNamespace(),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: release.APIVersion,
					Kind:       release.Kind,
					Name:       release.Name,
					UID:        release.UID,
				},
			},
			Labels: map[string]string{
				shipper.AppLabel:     release.OwnerReferences[0].Name,
				shipper.ReleaseLabel: release.GetName(),
			},
		},
		Spec: shipper.TrafficTargetSpec{
			Clusters: clusterTrafficTargets,
		},
	}

	clusterCapacityTargets := make([]shipper.ClusterCapacityTarget, 0, len(clusters))
	for _, cluster := range clusters {
		clusterCapacityTargets = append(
			clusterCapacityTargets,
			shipper.ClusterCapacityTarget{
				Name:              cluster.GetName(),
				Percent:           0,
				TotalReplicaCount: 12,
			})
	}

	capacityTarget := &shipper.CapacityTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      release.Name,
			Namespace: release.GetNamespace(),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: release.APIVersion,
					Kind:       release.Kind,
					Name:       release.Name,
					UID:        release.UID,
				},
			},
			Labels: map[string]string{
				shipper.AppLabel:     release.OwnerReferences[0].Name,
				shipper.ReleaseLabel: release.GetName(),
			},
		},
		Spec: shipper.CapacityTargetSpec{
			Clusters: clusterCapacityTargets,
		},
	}

	return installationTarget, trafficTarget, capacityTarget

}

func newScheduler(
	fixtures []runtime.Object,
) (*Scheduler, *shipperfake.Clientset) {
	clientset := shipperfake.NewSimpleClientset(fixtures...)
	informerFactory := shipperinformers.NewSharedInformerFactory(clientset, time.Millisecond*0)

	clustersLister := informerFactory.Shipper().V1alpha1().Clusters().Lister()
	installationTargetLister := informerFactory.Shipper().V1alpha1().InstallationTargets().Lister()
	capacityTargetLister := informerFactory.Shipper().V1alpha1().CapacityTargets().Lister()
	trafficTargetLister := informerFactory.Shipper().V1alpha1().TrafficTargets().Lister()

	c := NewScheduler(
		clientset,
		clustersLister,
		installationTargetLister,
		capacityTargetLister,
		trafficTargetLister,
		localFetchChart,
		record.NewFakeRecorder(42))

	stopCh := make(chan struct{})
	defer close(stopCh)

	informerFactory.Start(stopCh)
	informerFactory.WaitForCacheSync(stopCh)

	return c, clientset
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

type requirements shipper.ClusterRequirements
type clusters []shipper.ClusterSpec
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

func computeClusterTestCase(
	t *testing.T,
	name string,
	reqs requirements,
	clusterSpecs clusters,
	expectedClusters expected,
	expectError bool,
) {

	release := generateReleaseForTestCase(shipper.ClusterRequirements(reqs))
	clusters := make([]*shipper.Cluster, 0, len(clusterSpecs))
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

	actualClusterNames := make([]string, 0, len(actualClusters))
	for _, cluster := range actualClusters {
		actualClusterNames = append(actualClusterNames, cluster.GetName())
	}
	sort.Strings(actualClusterNames)

	if strings.Join(expectedClusters, ",") != strings.Join(actualClusterNames, ",") {
		t.Errorf("%q expected clusters %q, but got %q", name, strings.Join(expectedClusters, ","), strings.Join(actualClusterNames, ","))
		return
	}
}

func generateClusterForTestCase(name int, spec shipper.ClusterSpec) *shipper.Cluster {
	return &shipper.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("cluster-%d", name),
			Namespace: shippertesting.TestNamespace,
		},
		Spec: spec,
	}
}

func generateReleaseForTestCase(reqs shipper.ClusterRequirements) *shipper.Release {
	return &shipper.Release{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-release",
			Namespace: shippertesting.TestNamespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: shipper.SchemeGroupVersion.String(),
					Kind:       "Application",
					Name:       "test-application",
				},
			},
		},
		Spec: shipper.ReleaseSpec{
			Environment: shipper.ReleaseEnvironment{
				ClusterRequirements: reqs,
			},
		},
	}
}

// TestSchedule tests the first part of the cluster scheduling, which is find
// out which clusters the release must be installed, and persisting it under
// .status.environment.clusters.
func TestSchedule(t *testing.T) {
	// Fixtures
	clusterA := buildCluster("minikube-a")
	clusterB := buildCluster("minikube-b")
	release := buildRelease()
	fixtures := []runtime.Object{clusterA, clusterB, release}
	// Demand two clusters.
	release.Spec.Environment.ClusterRequirements.Regions[0].Replicas = pint32(2)

	// Expected values. The release should have, at the end of the business
	// logic, a list of clusters containing all clusters we've added to
	// the client in alphabetical order.
	expected := release.DeepCopy()
	expected.Annotations[shipper.ReleaseClustersAnnotation] = clusterA.GetName() + "," + clusterB.GetName()

	relWithConditions := expected.DeepCopy()

	// release should be marked as Scheduled
	condition := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&relWithConditions.Status, *condition)

	expectedActions := []kubetesting.Action{
		kubetesting.NewUpdateAction(
			shipper.SchemeGroupVersion.WithResource("releases"),
			release.GetNamespace(),
			expected),
	}

	c, clientset := newScheduler(fixtures)
	if _, err := c.ChooseClusters(release.DeepCopy(), false); err != nil {
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
	fixtures := []runtime.Object{clusterA, clusterB, release}

	// The release should have, at the end of the business logic, a list of
	// clusters containing the schedulable cluster we've added to the client.
	expected := release.DeepCopy()
	expected.Annotations[shipper.ReleaseClustersAnnotation] = clusterA.GetName()

	relWithConditions := expected.DeepCopy()

	condition := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&relWithConditions.Status, *condition)

	expectedActions := []kubetesting.Action{
		kubetesting.NewUpdateAction(
			shipper.SchemeGroupVersion.WithResource("releases"),
			release.GetNamespace(),
			expected),
	}

	c, clientset := newScheduler(fixtures)
	if _, err := c.ChooseClusters(release.DeepCopy(), false); err != nil {
		t.Fatal(err)
	}

	filteredActions := filterActions(clientset.Actions(), []string{"update"}, []string{"releases"})
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

// TestCreateAssociatedObjects checks whether the associated object set is being
// created while a release is being scheduled. In a normal case scenario, all 3
// objects do not exist by the moment of scheduling, therefore 3 extra create
// actions are expected.
func TestCreateAssociatedObjects(t *testing.T) {
	cluster := buildCluster("minikube-a")
	release := buildRelease()
	release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()
	fixtures := []runtime.Object{release, cluster}

	// Expected release and actions. The release should have, at the end of the
	// business logic, a list of clusters containing the sole cluster we've added
	// to the client, and also a Scheduled condition with True status. Expected
	// actions contain the intent to create all the associated target objects.
	expected := release.DeepCopy()
	expected.Status.Conditions = []shipper.ReleaseCondition{
		{Type: shipper.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
	}
	expectedActions := buildExpectedActions(expected.DeepCopy(), []*shipper.Cluster{cluster.DeepCopy()})
	// Release should be marked as Scheduled
	expectedActions = append(expectedActions, kubetesting.NewUpdateAction(
		shipper.SchemeGroupVersion.WithResource("releases"),
		release.GetNamespace(),
		expected))

	c, clientset := newScheduler(fixtures)
	if _, err := c.ScheduleRelease(release.DeepCopy()); err != nil {
		t.Fatal(err)
	}

	filteredActions := filterActions(
		clientset.Actions(),
		[]string{"update", "create"},
		[]string{"releases", "installationtargets", "traffictargets", "capacitytargets"},
	)
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

// TestCreateAssociatedObjectsDuplicateInstallationTargetMismatchingClusters
// tests a case when an installation target already exists but has a mismatching
// set of clusters. The job of the scheduler is to correct the mismatch and
// proceed normally. Instead of creating a new object, the existing one should
// be updated.
func TestCreateAssociatedObjectsDuplicateInstallationTargetMismatchingClusters(t *testing.T) {
	cluster := buildCluster("minikube-a")
	release := buildRelease()
	release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()

	installationtarget := &shipper.InstallationTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      release.GetName(),
			Namespace: release.GetNamespace(),
			OwnerReferences: []metav1.OwnerReference{
				createOwnerRefFromRelease(release),
			},
			Labels: map[string]string{
				shipper.AppLabel:     release.OwnerReferences[0].Name,
				shipper.ReleaseLabel: release.GetName(),
			},
		},
		Spec: shipper.InstallationTargetSpec{
			Chart:       release.Spec.Environment.Chart.DeepCopy(),
			Values:      release.Spec.Environment.Values,
			CanOverride: true,
		},
	}

	fixtures := []runtime.Object{release, installationtarget, cluster}

	expected := release.DeepCopy()
	expected.Status.Conditions = []shipper.ReleaseCondition{
		{Type: shipper.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
	}

	it, tt, ct := buildAssociatedObjects(expected.DeepCopy(), []*shipper.Cluster{cluster.DeepCopy()})
	// installationtarget already exists, expect an update ection. The rest
	// does not exist yet, therefore 2 more create actions.
	expectedActions := []kubetesting.Action{
		kubetesting.NewUpdateAction(
			shipper.SchemeGroupVersion.WithResource("installationtargets"),
			release.GetNamespace(),
			it),
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("traffictargets"),
			release.GetNamespace(),
			tt),
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("capacitytargets"),
			release.GetNamespace(),
			ct,
		),
		kubetesting.NewUpdateAction(
			shipper.SchemeGroupVersion.WithResource("releases"),
			release.GetNamespace(),
			expected),
	}

	c, clientset := newScheduler(fixtures)
	if _, err := c.ScheduleRelease(release.DeepCopy()); err != nil {
		t.Fatal(err)
	}

	filteredActions := filterActions(
		clientset.Actions(),
		[]string{"update", "create"},
		[]string{"releases", "installationtargets", "traffictargets", "capacitytargets"},
	)
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

// TestCreateAssociatedObjectsDuplicateTrafficTargetMismatchingClusters
// tests a case when a traffic target already exists but has a mismatching
// set of clusters. The job of the scheduler is to correct the mismatch and
// proceed normally. Instead of creating a new object, the existing one should
// be updated.
func TestCreateAssociatedObjectsDuplicateTrafficTargetMismatchingClusters(t *testing.T) {
	cluster := buildCluster("minikube-a")
	release := buildRelease()
	release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()

	traffictarget := &shipper.TrafficTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      release.GetName(),
			Namespace: release.GetNamespace(),
			OwnerReferences: []metav1.OwnerReference{
				createOwnerRefFromRelease(release),
			},
			Labels: map[string]string{
				shipper.AppLabel:     release.OwnerReferences[0].Name,
				shipper.ReleaseLabel: release.GetName(),
			},
		},
	}

	fixtures := []runtime.Object{release, traffictarget, cluster}

	expected := release.DeepCopy()
	expected.Status.Conditions = []shipper.ReleaseCondition{
		{Type: shipper.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
	}

	// traffictarget already exists, expect an update ection. The rest
	// does not exist yet, therefore 2 more create actions.
	it, tt, ct := buildAssociatedObjects(expected.DeepCopy(), []*shipper.Cluster{cluster.DeepCopy()})
	expectedActions := []kubetesting.Action{
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("installationtargets"),
			release.GetNamespace(),
			it),
		kubetesting.NewUpdateAction(
			shipper.SchemeGroupVersion.WithResource("traffictargets"),
			release.GetNamespace(),
			tt),
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("capacitytargets"),
			release.GetNamespace(),
			ct,
		),
		kubetesting.NewUpdateAction(
			shipper.SchemeGroupVersion.WithResource("releases"),
			release.GetNamespace(),
			expected),
	}

	c, clientset := newScheduler(fixtures)
	if _, err := c.ScheduleRelease(release.DeepCopy()); err != nil {
		t.Fatal(err)
	}

	filteredActions := filterActions(
		clientset.Actions(),
		[]string{"update", "create"},
		[]string{"releases", "installationtargets", "traffictargets", "capacitytargets"},
	)
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

// TestCreateAssociatedObjectsDuplicateCapacityTargetMismatchingClusters
// tests a case when a capacity target already exists but has a mismatching
// set of clusters. The job of the scheduler is to correct the mismatch and
// proceed normally. Instead of creating a new object, the existing one should
// be updated.
func TestCreateAssociatedObjectsDuplicateCapacityTargetMismatchingClusters(t *testing.T) {
	cluster := buildCluster("minikube-a")
	release := buildRelease()
	release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()

	capacitytarget := &shipper.CapacityTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      release.GetName(),
			Namespace: release.GetNamespace(),
			OwnerReferences: []metav1.OwnerReference{
				createOwnerRefFromRelease(release),
			},
			Labels: map[string]string{
				shipper.AppLabel:     release.OwnerReferences[0].Name,
				shipper.ReleaseLabel: release.GetName(),
			},
		},
	}

	fixtures := []runtime.Object{release, capacitytarget, cluster}

	expected := release.DeepCopy()
	expected.Status.Conditions = []shipper.ReleaseCondition{
		{Type: shipper.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
	}

	it, tt, ct := buildAssociatedObjects(expected.DeepCopy(), []*shipper.Cluster{cluster.DeepCopy()})
	// capacitytarget already exists, expect an update ection. The rest
	// does not exist yet, therefore 2 more create actions.
	expectedActions := []kubetesting.Action{
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("installationtargets"),
			release.GetNamespace(),
			it),
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("traffictargets"),
			release.GetNamespace(),
			tt),
		kubetesting.NewUpdateAction(
			shipper.SchemeGroupVersion.WithResource("capacitytargets"),
			release.GetNamespace(),
			ct,
		),
		kubetesting.NewUpdateAction(
			shipper.SchemeGroupVersion.WithResource("releases"),
			release.GetNamespace(),
			expected),
	}

	c, clientset := newScheduler(fixtures)
	if _, err := c.ScheduleRelease(release.DeepCopy()); err != nil {
		t.Fatal(err)
	}

	filteredActions := filterActions(
		clientset.Actions(),
		[]string{"update", "create"},
		[]string{"releases", "installationtargets", "traffictargets", "capacitytargets"},
	)
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

// TestCreateAssociatedObjectsDuplicateInstallationTargetSameOwner tests a case
// where an installationterget object already exists with the right cluster set
// and belongs to the right release. In this case we expect the scheduler to
// create the missing objects and proceed normally.
func TestCreateAssociatedObjectsDuplicateInstallationTargetSameOwner(t *testing.T) {
	cluster := buildCluster("minikube-a")
	release := buildRelease()
	release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()

	installationtarget := &shipper.InstallationTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      release.GetName(),
			Namespace: release.GetNamespace(),
			OwnerReferences: []metav1.OwnerReference{
				createOwnerRefFromRelease(release),
			},
		},
	}
	setInstallationTargetClusters(installationtarget, []string{cluster.Name})
	fixtures := []runtime.Object{release, cluster, installationtarget}

	// Expected release and actions. Even with an existing installationtarget
	// object for this release, at the end of the business logic the expected
	// release should have its .status.phase set to "WaitingForStrategy". Expected
	// actions contain the intent to create the missing associated target
	// objects and skip the existing one.
	expected := release.DeepCopy()
	expected.Status.Conditions = []shipper.ReleaseCondition{
		{Type: shipper.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
	}

	_, tt, ct := buildAssociatedObjects(expected.DeepCopy(), []*shipper.Cluster{cluster.DeepCopy()})
	expectedActions := []kubetesting.Action{
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("traffictargets"),
			release.GetNamespace(),
			tt),
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("capacitytargets"),
			release.GetNamespace(),
			ct,
		),
		kubetesting.NewUpdateAction(
			shipper.SchemeGroupVersion.WithResource("releases"),
			release.GetNamespace(),
			expected),
	}

	c, clientset := newScheduler(fixtures)
	if _, err := c.ScheduleRelease(release.DeepCopy()); err != nil {
		t.Fatal(err)
	}

	filteredActions := filterActions(
		clientset.Actions(),
		[]string{"update", "create"},
		[]string{"releases", "installationtargets", "traffictargets", "capacitytargets"},
	)
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

// TestCreateAssociatedObjectsDuplicateInstallationTargetNoOwner tests a case
// where an installationtarget object already exists but it does not belong to
// the propper release. This is an exception and we expect a conflict error to
// be returned.
func TestCreateAssociatedObjectsDuplicateInstallationTargetNoOwner(t *testing.T) {
	cluster := buildCluster("minikube-a")
	release := buildRelease()
	release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()

	installationtarget := &shipper.InstallationTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      release.GetName(),
			Namespace: release.GetNamespace(),
			// No explicit owner reference here
		},
	}
	fixtures := []runtime.Object{release, cluster, installationtarget}

	// Expect a release but no actions. This is broken state, the system
	// should never run into this on it's own. Returning a conflict error.
	expected := release.DeepCopy()
	expected.Status.Conditions = []shipper.ReleaseCondition{
		{Type: shipper.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
	}

	c, _ := newScheduler(fixtures)

	_, err := c.CreateOrUpdateInstallationTarget(release.DeepCopy())
	if err == nil {
		t.Fatalf("Expected an error here, none received")
	}

	if !errors.IsConflict(err) {
		t.Fatalf("Expected a conflict error, got: %s", err)
	}
}

// TestCreateAssociatedObjectsDuplicateTrafficTargetSameOwner tests a case where
// a traffictarget object already exists and has a propper cluster set. In this
// case we expect the missing asiociated objects to be created and the release
// to be scheduled.
func TestCreateAssociatedObjectsDuplicateTrafficTargetSameOwner(t *testing.T) {
	// Fixtures
	cluster := buildCluster("minikube-a")
	release := buildRelease()
	release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()

	traffictarget := &shipper.TrafficTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      release.GetName(),
			Namespace: release.GetNamespace(),
			OwnerReferences: []metav1.OwnerReference{
				createOwnerRefFromRelease(release),
			},
		},
	}
	setTrafficTargetClusters(traffictarget, []string{cluster.Name})
	fixtures := []runtime.Object{cluster, release, traffictarget}

	// Expected release and actions. Even with an existing traffictarget
	// object for this release, at the end of the business logic the expected
	// release should have its .status.phase set to "WaitingForStrategy". Expected
	// actions contain the intent to create the missing associated target
	// objects.
	expected := release.DeepCopy()
	expected.Status.Conditions = []shipper.ReleaseCondition{
		{Type: shipper.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
	}

	it, _, ct := buildAssociatedObjects(expected.DeepCopy(), []*shipper.Cluster{cluster.DeepCopy()})
	// 2 create actions: installationtarget and capacitytarget
	expectedActions := []kubetesting.Action{
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("installationtargets"),
			release.GetNamespace(),
			it),
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("capacitytargets"),
			release.GetNamespace(),
			ct,
		),
		kubetesting.NewUpdateAction(
			shipper.SchemeGroupVersion.WithResource("releases"),
			release.GetNamespace(),
			expected),
	}

	c, clientset := newScheduler(fixtures)
	if _, err := c.ScheduleRelease(release.DeepCopy()); err != nil {
		t.Fatal(err)
	}

	filteredActions := filterActions(
		clientset.Actions(),
		[]string{"update", "create"},
		[]string{"releases", "installationtargets", "traffictargets", "capacitytargets"},
	)
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

// TestCreateAssociatedObjectsDuplicateTrafficTargetNoOwner tests a case where
// and existing traffictarget object exists but has a wrong owner reference.
// It's an exception case and we expect a conflict error.
func TestCreateAssociatedObjectsDuplicateTrafficTargetNoOwner(t *testing.T) {
	// Fixtures
	cluster := buildCluster("minikube-a")
	release := buildRelease()
	release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()

	traffictarget := &shipper.TrafficTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      release.GetName(),
			Namespace: release.GetNamespace(),
			// No explicit owner reference here
		},
	}
	fixtures := []runtime.Object{cluster, release, traffictarget}

	// Expected a release but no actions. With an existing traffictarget
	// object but no explicit reference, it's a no-go. Expected an
	// already-exists error.
	expected := release.DeepCopy()
	expected.Status.Conditions = []shipper.ReleaseCondition{
		{Type: shipper.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
	}

	c, _ := newScheduler(fixtures)

	_, err := c.CreateOrUpdateTrafficTarget(release.DeepCopy())
	if err == nil {
		t.Fatalf("Expected an error here, none received")
	}

	if !errors.IsConflict(err) {
		t.Fatalf("Expected a conflict error, got: %s", err)
	}
}

// TestCreateAssociatedObjectsDuplicateCapacityTargetSameOwner tests a case
// where a capacitytarget object already exists and has a right owner reference.
// In this case we expect the missing objects to be created.
func TestCreateAssociatedObjectsDuplicateCapacityTargetSameOwner(t *testing.T) {
	// Fixtures
	cluster := buildCluster("minikube-a")
	release := buildRelease()
	release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()

	var totalReplicaCount int32 = 1

	capacitytarget := &shipper.CapacityTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      release.GetName(),
			Namespace: release.GetNamespace(),
			OwnerReferences: []metav1.OwnerReference{
				createOwnerRefFromRelease(release),
			},
		},
	}
	setCapacityTargetClusters(capacitytarget, []string{cluster.Name}, totalReplicaCount)
	fixtures := []runtime.Object{cluster, release, capacitytarget}

	// Expected release and actions. Even with an existing capacitytarget object
	// for this release, at the end of the business logic the expected release
	// should have its .status.phase set to "WaitingForStrategy". Expected actions
	// contain the intent to create all the associated target objects.
	expected := release.DeepCopy()
	expected.Status.Conditions = []shipper.ReleaseCondition{
		{Type: shipper.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
	}

	it, tt, _ := buildAssociatedObjects(expected.DeepCopy(), []*shipper.Cluster{cluster.DeepCopy()})
	// 2 create actions: installationtarget and traffictarget
	expectedActions := []kubetesting.Action{
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("installationtargets"),
			release.GetNamespace(),
			it),
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("traffictargets"),
			release.GetNamespace(),
			tt,
		),
		kubetesting.NewUpdateAction(
			shipper.SchemeGroupVersion.WithResource("releases"),
			release.GetNamespace(),
			expected),
	}

	c, clientset := newScheduler(fixtures)
	if _, err := c.ScheduleRelease(release.DeepCopy()); err != nil {
		t.Fatal(err)
	}

	actions := filterActions(
		clientset.Actions(),
		[]string{"update", "create"},
		[]string{"releases", "installationtargets", "traffictargets", "capacitytargets"},
	)
	shippertesting.CheckActions(expectedActions, actions, t)
}

// TestCreateAssociatedObjectsDuplicateCapacityTargetNoOwner tests a case where
// a capacitytarget object already exists but it has a wrong owner reference.
// It's an exception and we expect a conflict error.
func TestCreateAssociatedObjectsDuplicateCapacityTargetNoOwner(t *testing.T) {
	// Fixtures
	cluster := buildCluster("minikube-a")
	release := buildRelease()
	release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()

	capacitytarget := &shipper.CapacityTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      release.GetName(),
			Namespace: release.GetNamespace(),
		},
	}
	fixtures := []runtime.Object{cluster, release, capacitytarget}

	// Expected a release but no actions. With an existing capacitytarget
	// object but no explicit reference, it's a no-go. Expected a
	// conflict error.
	expected := release.DeepCopy()
	expected.Status.Conditions = []shipper.ReleaseCondition{
		{Type: shipper.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
	}

	c, _ := newScheduler(fixtures)

	_, err := c.CreateOrUpdateCapacityTarget(release.DeepCopy(), 1)
	if err == nil {
		t.Fatalf("Expected an error here, none received")
	}

	if !errors.IsConflict(err) {
		t.Fatalf("Expected a conflict error, got: %s", err)
	}
}

// TestComputeTargetClusters works the core of the scheduler logic: matching
// regions and capabilities between releases and clusters NOTE: the "expected"
// clusters are due to the particular prefList outcomes, and as such should be
// expected to break if we change the hash function for the preflist.
func TestComputeTargetClusters(t *testing.T) {
	computeClusterTestCase(t, "error when no regions specified",
		requirements{
			Regions: []shipper.RegionRequirement{},
		},
		clusters{
			{Region: shippertesting.TestRegion, Capabilities: []string{}},
		},
		expected{},
		errorCase,
	)

	computeClusterTestCase(t, "basic region match",
		requirements{
			Regions: []shipper.RegionRequirement{{Name: "matches"}},
		},
		clusters{
			{Region: "matches", Capabilities: []string{}},
		},
		expected{"cluster-0"},
		passingCase,
	)

	computeClusterTestCase(t, "one region match one no match",
		requirements{
			Regions: []shipper.RegionRequirement{{Name: "matches"}},
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
			Regions: []shipper.RegionRequirement{{Name: "matches", Replicas: pint32(2)}},
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
			Regions:      []shipper.RegionRequirement{{Name: "matches"}},
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
			Regions:      []shipper.RegionRequirement{{Name: "matches", Replicas: pint32(2)}},
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
			Regions:      []shipper.RegionRequirement{{Name: "foo"}},
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
			Regions:      []shipper.RegionRequirement{{Name: "foo"}},
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
			Regions:      []shipper.RegionRequirement{{Name: "foo"}},
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
			Regions: []shipper.RegionRequirement{
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
			Regions: []shipper.RegionRequirement{
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
			Regions: []shipper.RegionRequirement{
				{Name: "us-east", Replicas: pint32(2)},
			},
		},
		clusters{
			{
				Region:    "us-east",
				Scheduler: shipper.ClusterSchedulerSettings{Unschedulable: true},
			},
			{
				Region:    "us-east",
				Scheduler: shipper.ClusterSchedulerSettings{Unschedulable: true},
			},
			{Region: "us-east"},
		},
		expected{},
		errorCase,
	)

	computeClusterTestCase(t, "heavy weight changes normal priority",
		requirements{
			Regions: []shipper.RegionRequirement{
				{Name: "us-east", Replicas: pint32(1)},
				{Name: "eu-west", Replicas: pint32(1)},
			},
			Capabilities: []string{"a"},
		},
		clusters{
			{
				Region:       "us-east",
				Capabilities: []string{"a"},
				Scheduler:    shipper.ClusterSchedulerSettings{Weight: pint32(900)},
			},
			{Region: "us-east", Capabilities: []string{"a"}},
			{Region: "eu-west", Capabilities: []string{"a"}},
			{
				Region:       "eu-west",
				Capabilities: []string{"a"},
				Scheduler:    shipper.ClusterSchedulerSettings{Weight: pint32(900)},
			},
		},
		// This test is identical to "more clusters than needed", and without weight
		// would yield the same result (cluster-1, cluster-2).
		expected{"cluster-0", "cluster-3"},
		passingCase,
	)

	computeClusterTestCase(t, "a little weight doesn't change things",
		requirements{
			Regions: []shipper.RegionRequirement{
				{Name: "us-east", Replicas: pint32(1)},
				{Name: "eu-west", Replicas: pint32(1)},
			},
			Capabilities: []string{"a"},
		},
		clusters{
			{
				Region:       "us-east",
				Capabilities: []string{"a"},
				Scheduler:    shipper.ClusterSchedulerSettings{Weight: pint32(101)},
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
			Regions: []shipper.RegionRequirement{
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
				Scheduler: shipper.ClusterSchedulerSettings{
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
