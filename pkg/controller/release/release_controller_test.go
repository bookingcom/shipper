package release

import (
	"log"
	"os"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/helm/pkg/repo/repotest"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	//clientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	"github.com/bookingcom/shipper/pkg/conditions"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

func init() {
	releaseutil.ConditionsShouldDiscardTimestamps = true
	conditions.StrategyConditionsShouldDiscardTimestamps = true
}

var chartRepoURL string

func TestMain(m *testing.M) {
	srv, hh, err := repotest.NewTempServer("testdata/*.tgz")
	if err != nil {
		log.Fatal(err)
	}

	chartRepoURL = srv.URL()

	result := m.Run()

	// If the test panics for any reason, the test server doesn't get cleaned up.
	os.RemoveAll(hh.String())
	srv.Stop()

	os.Exit(result)
}

func TestControllerComputeTargetClusters(t *testing.T) {
	cluster := buildCluster("minikube-a")
	release := buildRelease()
	release.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()

	fixtures := []runtime.Object{release, cluster}

	expected := release.DeepCopy()
	expected.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()
	condition := releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeScheduled, corev1.ConditionTrue, "", "")
	releaseutil.SetReleaseCondition(&expected.Status, *condition)

	expectedActions := []kubetesting.Action{
		kubetesting.NewUpdateAction(
			shipper.SchemeGroupVersion.WithResource("releases"),
			release.GetNamespace(),
			expected), // `expected` contains the info about the chosen cluster
	}

	clientset := shipperfake.NewSimpleClientset(fixtures...)
	controller := newReleaseController(clientset)
	controller.processNextReleaseWorkItem()

	filteredActions := filterActions(clientset.Actions(), []string{"update"}, []string{"releases"})
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

func TestControllerCreateAssociatedObjects(t *testing.T) {
	cluster := buildCluster("minikube-a")
	rel := buildRelease()
	rel.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()
	ns := rel.GetNamespace()

	fixtures := []runtime.Object{rel, cluster}

	expected := rel.DeepCopy()
	expected.Status.Conditions = []shipper.ReleaseCondition{
		{Type: shipper.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
	}

	installationTarget := buildInstallationTarget(ns, expected)
	trafficTarget := buildTrafficTarget(ns, expected)
	capacityTarget := buildCapacityTarget(ns, expected)

	expectedActions := []kubetesting.Action{
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("installationtargets"),
			ns,
			installationTarget),
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("traffictargets"),
			ns,
			trafficTarget),
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("capacitytargets"),
			ns,
			capacityTarget,
		),
		kubetesting.NewUpdateAction(
			shipper.SchemeGroupVersion.WithResource("releases"),
			ns,
			expected),
	}

	clientset := shipperfake.NewSimpleClientset(fixtures...)

	controller := newReleaseController(clientset)
	controller.processNextReleaseWorkItem()

	filteredActions := filterActions(
		clientset.Actions(),
		[]string{"update", "create"},
		[]string{"releases", "installationtargets", "traffictargets", "capacitytargets"},
	)
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

func TestControllerCreateAssociatedObjectsDuplicateInstallationTarget(t *testing.T) {
	cluster := buildCluster("minikube-a")
	rel := buildRelease()
	rel.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()
	ns := rel.GetNamespace()

	installationtarget := &shipper.InstallationTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rel.GetName(),
			Namespace: rel.GetNamespace(),
			OwnerReferences: []metav1.OwnerReference{
				createOwnerRefFromRelease(rel),
			},
		},
	}

	expected := rel.DeepCopy()
	expected.Status.Conditions = []shipper.ReleaseCondition{
		{Type: shipper.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
	}

	// Expected release and actions. Even with an existing installationtarget
	// object for this release, at the end of the business logic the expected
	// release should have its .status.phase set to "WaitingForStrategy". Expected
	// actions contain the intent to create all the associated target objects.

	installationTarget := buildInstallationTarget(ns, expected)
	trafficTarget := buildTrafficTarget(ns, expected)
	capacityTarget := buildCapacityTarget(ns, expected)

	expectedActions := []kubetesting.Action{
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("installationtargets"),
			ns,
			installationTarget),
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("traffictargets"),
			ns,
			trafficTarget),
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("capacitytargets"),
			ns,
			capacityTarget,
		),
		kubetesting.NewUpdateAction(
			shipper.SchemeGroupVersion.WithResource("releases"),
			ns,
			expected),
	}

	fixtures := []runtime.Object{rel, cluster, installationtarget}

	clientset := shipperfake.NewSimpleClientset(fixtures...)
	controller := newReleaseController(clientset)
	controller.processNextReleaseWorkItem()

	filteredActions := filterActions(
		clientset.Actions(),
		[]string{"update", "create"},
		[]string{"releases", "installationtargets", "traffictargets", "capacitytargets"},
	)
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

func TestControllerCreateAssociatedObjectsDuplicateTrafficTarget(t *testing.T) {
	cluster := buildCluster("minikube-a")
	rel := buildRelease()
	rel.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()
	ns := rel.GetNamespace()

	traffictarget := &shipper.TrafficTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rel.GetName(),
			Namespace: rel.GetNamespace(),
			OwnerReferences: []metav1.OwnerReference{
				createOwnerRefFromRelease(rel),
			},
		},
	}

	expected := rel.DeepCopy()
	expected.Status.Conditions = []shipper.ReleaseCondition{
		{Type: shipper.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
	}

	installationTarget := buildInstallationTarget(ns, expected)
	trafficTarget := buildTrafficTarget(ns, expected)
	capacityTarget := buildCapacityTarget(ns, expected)

	expectedActions := []kubetesting.Action{
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("installationtargets"),
			ns,
			installationTarget),
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("traffictargets"),
			ns,
			trafficTarget),
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("capacitytargets"),
			ns,
			capacityTarget,
		),
		kubetesting.NewUpdateAction(
			shipper.SchemeGroupVersion.WithResource("releases"),
			ns,
			expected),
	}

	fixtures := []runtime.Object{cluster, rel, traffictarget}

	clientset := shipperfake.NewSimpleClientset(fixtures...)
	controller := newReleaseController(clientset)
	controller.processNextReleaseWorkItem()

	filteredActions := filterActions(
		clientset.Actions(),
		[]string{"update", "create"},
		[]string{"releases", "installationtargets", "traffictargets", "capacitytargets"},
	)

	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

func TestControllerCreateAssociatedObjectsDuplicateCapacityTarget(t *testing.T) {
	cluster := buildCluster("minikube-a")
	rel := buildRelease()
	rel.Annotations[shipper.ReleaseClustersAnnotation] = cluster.GetName()
	ns := rel.GetNamespace()

	capacitytarget := &shipper.CapacityTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rel.GetName(),
			Namespace: rel.GetNamespace(),
			OwnerReferences: []metav1.OwnerReference{
				createOwnerRefFromRelease(rel),
			},
		},
	}

	expected := rel.DeepCopy()
	expected.Status.Conditions = []shipper.ReleaseCondition{
		{Type: shipper.ReleaseConditionTypeScheduled, Status: corev1.ConditionTrue},
	}

	installationTarget := buildInstallationTarget(ns, expected)
	trafficTarget := buildTrafficTarget(ns, expected)
	capacityTarget := buildCapacityTarget(ns, expected)

	expectedActions := []kubetesting.Action{
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("installationtargets"),
			ns,
			installationTarget),
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("traffictargets"),
			ns,
			trafficTarget),
		kubetesting.NewCreateAction(
			shipper.SchemeGroupVersion.WithResource("capacitytargets"),
			ns,
			capacityTarget,
		),
		kubetesting.NewUpdateAction(
			shipper.SchemeGroupVersion.WithResource("releases"),
			ns,
			expected),
	}

	fixtures := []runtime.Object{cluster, rel, capacitytarget}
	clientset := shipperfake.NewSimpleClientset(fixtures...)
	controller := newReleaseController(clientset)
	controller.processNextReleaseWorkItem()

	filteredActions := filterActions(
		clientset.Actions(),
		[]string{"update", "create"},
		[]string{"releases", "installationtargets", "traffictargets", "capacitytargets"},
	)
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

func TestContenderReleasePhaseIsWaitingForCommandForInitialStepState(t *testing.T) {

}
