package release

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
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
