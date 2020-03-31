package release

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

func buildReleaseForSchedulerTest(clusters []*shipper.Cluster) *shipper.Release {
	rel := buildRelease(
		shippertesting.TestNamespace,
		shippertesting.TestApp,
		"contender",
		0,
	)

	setReleaseClusters(rel, clusters)

	return rel
}

func newScheduler(
	fixtures []runtime.Object,
) (*Scheduler, *shipperfake.Clientset) {
	clientset := shipperfake.NewSimpleClientset(fixtures...)
	informerFactory := shipperinformers.NewSharedInformerFactory(clientset, time.Millisecond*0)

	installationTargetLister := informerFactory.Shipper().V1alpha1().InstallationTargets().Lister()
	capacityTargetLister := informerFactory.Shipper().V1alpha1().CapacityTargets().Lister()
	trafficTargetLister := informerFactory.Shipper().V1alpha1().TrafficTargets().Lister()

	c := NewScheduler(
		clientset,
		installationTargetLister,
		capacityTargetLister,
		trafficTargetLister,
		shippertesting.LocalFetchChart,
		record.NewFakeRecorder(42))

	stopCh := make(chan struct{})
	defer close(stopCh)

	informerFactory.Start(stopCh)
	informerFactory.WaitForCacheSync(stopCh)

	return c, clientset
}

// TestCreateAssociatedObjects checks whether the associated object set is being
// created while a release is being scheduled. In a normal case scenario, all 3
// objects do not exist by the moment of scheduling, therefore 3 extra create
// actions are expected.
func TestCreateAssociatedObjects(t *testing.T) {
	clusters := []*shipper.Cluster{buildCluster("minikube-a")}
	release := buildReleaseForSchedulerTest(clusters)

	fixtures := []runtime.Object{release}

	expectedActions := buildExpectedActions(release, clusters)

	c, clientset := newScheduler(fixtures)
	if _, err := c.ScheduleRelease(release); err != nil {
		t.Fatal(err)
	}

	filteredActions := shippertesting.FilterActions(clientset.Actions())
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

// TestCreateAssociatedObjectsDuplicateInstallationTargetMismatchingClusters
// tests a case when an installation target already exists but has a mismatching
// set of clusters. The job of the scheduler is to correct the mismatch and
// proceed normally. Instead of creating a new object, the existing one should
// be updated.
func TestCreateAssociatedObjectsDuplicateInstallationTargetMismatchingClusters(t *testing.T) {
	cluster := buildCluster("minikube-a")
	release := buildReleaseForSchedulerTest([]*shipper.Cluster{cluster})
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
			Chart:       release.Spec.Environment.Chart,
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
	}

	c, clientset := newScheduler(fixtures)
	if _, err := c.ScheduleRelease(release.DeepCopy()); err != nil {
		t.Fatal(err)
	}

	filteredActions := shippertesting.FilterActions(clientset.Actions())
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

// TestCreateAssociatedObjectsDuplicateTrafficTargetMismatchingClusters
// tests a case when a traffic target already exists but has a mismatching
// set of clusters. The job of the scheduler is to correct the mismatch and
// proceed normally. Instead of creating a new object, the existing one should
// be updated.
func TestCreateAssociatedObjectsDuplicateTrafficTargetMismatchingClusters(t *testing.T) {
	clusters := []*shipper.Cluster{buildCluster("minikube-a")}
	release := buildReleaseForSchedulerTest(clusters)

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

	fixtures := []runtime.Object{traffictarget}

	// traffictarget already exists, expect an update ection. The rest
	// does not exist yet, therefore 2 more create actions.
	it, tt, ct := buildAssociatedObjects(release, clusters)
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
	}

	c, clientset := newScheduler(fixtures)
	if _, err := c.ScheduleRelease(release); err != nil {
		t.Fatal(err)
	}

	filteredActions := shippertesting.FilterActions(clientset.Actions())
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

// TestCreateAssociatedObjectsDuplicateCapacityTargetMismatchingClusters
// tests a case when a capacity target already exists but has a mismatching
// set of clusters. The job of the scheduler is to correct the mismatch and
// proceed normally. Instead of creating a new object, the existing one should
// be updated.
func TestCreateAssociatedObjectsDuplicateCapacityTargetMismatchingClusters(t *testing.T) {
	clusters := []*shipper.Cluster{buildCluster("minikube-a")}
	release := buildReleaseForSchedulerTest(clusters)
	release.Annotations[shipper.ReleaseClustersAnnotation] = clusters[0].GetName()

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

	fixtures := []runtime.Object{capacitytarget}

	// capacitytarget already exists, expect an update ection. The rest
	// does not exist yet, therefore 2 more create actions.
	it, tt, ct := buildAssociatedObjects(release, clusters)
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
	}

	c, clientset := newScheduler(fixtures)
	if _, err := c.ScheduleRelease(release); err != nil {
		t.Fatal(err)
	}

	filteredActions := shippertesting.FilterActions(clientset.Actions())
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

// TestCreateAssociatedObjectsDuplicateInstallationTargetSameOwner tests a case
// where an installationterget object already exists with the right cluster set
// and belongs to the right release. In this case we expect the scheduler to
// create the missing objects and proceed normally.
func TestCreateAssociatedObjectsDuplicateInstallationTargetSameOwner(t *testing.T) {
	cluster := buildCluster("minikube-a")
	release := buildReleaseForSchedulerTest([]*shipper.Cluster{cluster})
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
	}

	c, clientset := newScheduler(fixtures)
	if _, err := c.ScheduleRelease(release.DeepCopy()); err != nil {
		t.Fatal(err)
	}

	filteredActions := shippertesting.FilterActions(clientset.Actions())
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

// TestCreateAssociatedObjectsDuplicateInstallationTargetNoOwner tests a case
// where an installationtarget object already exists but it does not belong to
// the propper release. This is an exception and we expect the appropriate
// error to be returned.
func TestCreateAssociatedObjectsDuplicateInstallationTargetNoOwner(t *testing.T) {
	cluster := buildCluster("minikube-a")
	release := buildReleaseForSchedulerTest([]*shipper.Cluster{cluster})
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

	if !shippererrors.IsWrongOwnerReferenceError(err) {
		t.Fatalf("Expected a WrongOwnerReferenceError error, got: %s", err)
	}
}

// TestCreateAssociatedObjectsDuplicateTrafficTargetSameOwner tests a case where
// a traffictarget object already exists and has a propper cluster set. In this
// case we expect the missing asiociated objects to be created and the release
// to be scheduled.
func TestCreateAssociatedObjectsDuplicateTrafficTargetSameOwner(t *testing.T) {
	clusters := []*shipper.Cluster{buildCluster("minikube-a")}
	release := buildReleaseForSchedulerTest(clusters)

	traffictarget := &shipper.TrafficTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      release.GetName(),
			Namespace: release.GetNamespace(),
			OwnerReferences: []metav1.OwnerReference{
				createOwnerRefFromRelease(release),
			},
		},
	}
	setTrafficTargetClusters(traffictarget, []string{clusters[0].Name})

	fixtures := []runtime.Object{traffictarget}

	// 2 create actions: installationtarget and capacitytarget
	it, _, ct := buildAssociatedObjects(release, clusters)
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
	}

	c, clientset := newScheduler(fixtures)
	if _, err := c.ScheduleRelease(release); err != nil {
		t.Fatal(err)
	}

	filteredActions := shippertesting.FilterActions(clientset.Actions())
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

// TestCreateAssociatedObjectsDuplicateTrafficTargetNoOwner tests a case where
// and existing traffictarget object exists but has a wrong owner reference.
// It's an exception case and we expect the appropriate error to be returned.
func TestCreateAssociatedObjectsDuplicateTrafficTargetNoOwner(t *testing.T) {
	clusters := []*shipper.Cluster{buildCluster("minikube-a")}
	release := buildReleaseForSchedulerTest(clusters)

	traffictarget := &shipper.TrafficTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      release.GetName(),
			Namespace: release.GetNamespace(),
			// No explicit owner reference here
		},
	}

	fixtures := []runtime.Object{traffictarget}

	c, _ := newScheduler(fixtures)

	_, err := c.CreateOrUpdateTrafficTarget(release)
	if err == nil {
		t.Fatalf("Expected an error here, none received")
	}

	if !shippererrors.IsWrongOwnerReferenceError(err) {
		t.Fatalf("Expected a WrongOwnerReferenceError error, got: %s", err)
	}
}

// TestCreateAssociatedObjectsDuplicateCapacityTargetSameOwner tests a case
// where a capacitytarget object already exists and has a right owner reference.
// In this case we expect the missing objects to be created.
func TestCreateAssociatedObjectsDuplicateCapacityTargetSameOwner(t *testing.T) {
	clusters := []*shipper.Cluster{buildCluster("minikube-a")}
	release := buildReleaseForSchedulerTest(clusters)

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
	setCapacityTargetClusters(capacitytarget, []string{clusters[0].Name}, totalReplicaCount)

	fixtures := []runtime.Object{capacitytarget}

	// 2 create actions: installationtarget and traffictarget
	it, tt, _ := buildAssociatedObjects(release, clusters)
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
	}

	c, clientset := newScheduler(fixtures)
	if _, err := c.ScheduleRelease(release); err != nil {
		t.Fatal(err)
	}

	actions := shippertesting.FilterActions(clientset.Actions())
	shippertesting.CheckActions(expectedActions, actions, t)
}

// TestCreateAssociatedObjectsDuplicateCapacityTargetNoOwner tests a case where
// a capacitytarget object already exists but it has a wrong owner reference.
// It's an exception and we expect the appropriate error to be returned.
func TestCreateAssociatedObjectsDuplicateCapacityTargetNoOwner(t *testing.T) {
	clusters := []*shipper.Cluster{buildCluster("minikube-a")}
	release := buildReleaseForSchedulerTest(clusters)

	capacitytarget := &shipper.CapacityTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      release.GetName(),
			Namespace: release.GetNamespace(),
		},
	}

	fixtures := []runtime.Object{capacitytarget}

	c, _ := newScheduler(fixtures)

	_, err := c.CreateOrUpdateCapacityTarget(release, 1)
	if err == nil {
		t.Fatalf("Expected an error here, none received")
	}

	if !shippererrors.IsWrongOwnerReferenceError(err) {
		t.Fatalf("Expected a WrongOwnerReferenceError error, got: %s", err)
	}
}
