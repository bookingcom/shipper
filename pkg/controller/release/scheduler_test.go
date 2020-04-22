package release

import (
	"testing"
	"time"

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

	listers := listers{
		installationTargetLister: informerFactory.Shipper().V1alpha1().InstallationTargets().Lister(),
		capacityTargetLister:     informerFactory.Shipper().V1alpha1().CapacityTargets().Lister(),
		trafficTargetLister:      informerFactory.Shipper().V1alpha1().TrafficTargets().Lister(),
	}

	// TODO(jgreff): we're using clienset twice here
	c := NewScheduler(
		clientset,
		clientset,
		listers,
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

	expectedActions := buildExpectedActions(release, clusters)

	c, clientset := newScheduler(nil)
	if _, err := c.ScheduleRelease(release); err != nil {
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
