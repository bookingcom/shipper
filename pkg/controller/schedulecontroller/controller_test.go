package schedulecontroller

import (
	"testing"
	"time"

	shipperV1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	"github.com/bookingcom/shipper/pkg/chart"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"

	corev1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
)

func init() {
	releaseutil.ConditionsShouldDiscardTimestamps = true
}

func newController(fixtures ...runtime.Object) (*Controller, *shipperfake.Clientset) {
	shipperclient := shipperfake.NewSimpleClientset(fixtures...)
	kubeclient := kubefake.NewSimpleClientset()
	informerFactory := shipperinformers.NewSharedInformerFactory(shipperclient, time.Millisecond*0)

	c := NewController(
		kubeclient,
		shipperclient,
		informerFactory,
		chart.FetchRemote(),
		record.NewFakeRecorder(42),
	)

	stopCh := make(chan struct{})
	defer close(stopCh)

	informerFactory.Start(stopCh)
	informerFactory.WaitForCacheSync(stopCh)

	return c, shipperclient
}

func TestControllerComputeTargetClusters(t *testing.T) {
	// Fixtures
	cluster := buildCluster("minikube-a")
	release := buildRelease()
	fixtures := []runtime.Object{cluster, release}

	// Expected values. The release should have, at the end of the business
	// logic, a list of clusters containing the sole cluster we've added to
	// the client.
	expected := release.DeepCopy()
	expected.Annotations[shipperV1.ReleaseClustersAnnotation] = cluster.GetName()

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
	c, clientset := newController(fixtures...)
	c.processNextWorkItem()

	// Check actions
	filteredActions := filterActions(clientset.Actions(), []string{"update"}, []string{"releases"})
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

func TestControllerCreateAssociatedObjects(t *testing.T) {
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
	expectedActions := buildExpectedActions(release.GetNamespace(), expected)

	// Business logic...
	c, clientset := newController(fixtures...)
	c.processNextWorkItem()

	// Check actions
	filteredActions := filterActions(
		clientset.Actions(),
		[]string{"update", "create"},
		[]string{"releases", "installationtargets", "traffictargets", "capacitytargets"},
	)
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

func TestControllerCreateAssociatedObjectsDuplicateInstallationTarget(t *testing.T) {
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
	expectedActions := buildExpectedActions(release.GetNamespace(), expected)

	// Business logic...
	c, clientset := newController(fixtures...)
	c.processNextWorkItem()

	// Check actions
	filteredActions := filterActions(
		clientset.Actions(),
		[]string{"update", "create"},
		[]string{"releases", "installationtargets", "traffictargets", "capacitytargets"},
	)
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

func TestControllerCreateAssociatedObjectsDuplicateTrafficTarget(t *testing.T) {
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
	expectedActions := buildExpectedActions(release.GetNamespace(), expected)

	// Business logic...
	c, clientset := newController(fixtures...)
	c.processNextWorkItem()

	// Check actions
	filteredActions := filterActions(
		clientset.Actions(),
		[]string{"update", "create"},
		[]string{"releases", "installationtargets", "traffictargets", "capacitytargets"},
	)
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

func TestControllerCreateAssociatedObjectsDuplicateCapacityTarget(t *testing.T) {
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
	expectedActions := buildExpectedActions(release.GetNamespace(), expected)

	// Business logic...
	c, clientset := newController(fixtures...)
	c.processNextWorkItem()

	// Check actions
	actions := filterActions(
		clientset.Actions(),
		[]string{"update", "create"},
		[]string{"releases", "installationtargets", "traffictargets", "capacitytargets"},
	)
	shippertesting.CheckActions(expectedActions, actions, t)
}
