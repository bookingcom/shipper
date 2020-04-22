package installation

import (
	"fmt"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
	targetutil "github.com/bookingcom/shipper/pkg/util/target"
	testutil "github.com/bookingcom/shipper/pkg/util/testing"
)

type object struct {
	gvr  schema.GroupVersionResource
	name string
}

func init() {
	targetutil.ConditionsShouldDiscardTimestamps = true
}

// TestSuccess verifies that the installation controller creates the objects
// rendered from the chart and reports readiness.
func TestSuccess(t *testing.T) {
	it := buildInstallationTarget(
		shippertesting.TestNamespace,
		shippertesting.TestApp,
		buildChart(nginxChartName, "0.1.0"))

	runInstallationControllerTest(t, it, SuccessStatus,
		buildExpectedObjects(it))
}

// TestInvalidChart verifies that the installation controller updates the
// installation traffic with the correct conditions when a chart is invalid.
func TestInvalidChart(t *testing.T) {
	it := buildInstallationTarget(
		shippertesting.TestNamespace,
		shippertesting.TestApp,
		buildChart(reviewsChartName, "invalid-deployment-name"))

	status := shipper.InstallationTargetStatus{
		Conditions: []shipper.TargetCondition{
			{
				Type:    shipper.TargetConditionTypeOperational,
				Status:  corev1.ConditionFalse,
				Reason:  ChartError,
				Message: fmt.Sprintf(`Deployment %q has invalid name. The name of the Deployment should be templated with {{.Release.Name}}.`, reviewsChartName),
			},
			TargetConditionReadyUnknown,
		},
	}

	runInstallationControllerTest(t, it, status, nil)
}

// buildExpectedObjects returns a list of the objects we expect from
// `nginxChartName`. This can be hardcoded for as long as we depend on that one
// chart.
func buildExpectedObjects(it *shipper.InstallationTarget) []object {
	deployment := appsv1.SchemeGroupVersion.WithResource("deployments")
	service := corev1.SchemeGroupVersion.WithResource("services")

	return []object{
		{deployment, fmt.Sprintf("%s-%s", shippertesting.TestApp, nginxChartName)},
		{service, nginxChartName},
		{service, fmt.Sprintf("%s-%s", nginxChartName, "staging")},
	}
}

func runInstallationControllerTest(
	t *testing.T,
	it *shipper.InstallationTarget,
	status shipper.InstallationTargetStatus,
	objects []object,
) {
	f := newFixture([]runtime.Object{})
	f.ShipperClient.Tracker().Add(it)

	runController(f)

	itGVR := shipper.SchemeGroupVersion.WithResource("installationtargets")
	itKey := fmt.Sprintf("%s/%s", it.Namespace, it.Name)
	object, err := f.ShipperClient.Tracker().Get(itGVR, it.Namespace, it.Name)
	if err != nil {
		t.Fatalf("could not Get InstallationTarget %q: %s", itKey, err)
	}

	actualIT := object.(*shipper.InstallationTarget)
	actualStatus := actualIT.Status
	eq, diff := testutil.DeepEqualDiff(status, actualStatus)
	if !eq {
		t.Fatalf(
			"InstallationTarget %q has Status different from expected:\n%s",
			itKey, diff)
	}

	for _, expected := range objects {
		gvr := expected.gvr
		name := expected.name
		_, err := f.DynamicClient.
			Resource(gvr).
			Namespace(it.Namespace).
			Get(name, metav1.GetOptions{})

		if err != nil {
			t.Errorf(
				`expected to get %s %q, but got error instead: %s`,
				gvr.Resource, name, err)
		}
	}
}

func runController(f *shippertesting.ControllerTestFixture) {
	controller := NewController(
		f.KubeClient,
		f.KubeInformerFactory,
		f.ShipperClient,
		f.ShipperInformerFactory,
		f.DynamicClientBuilder,
		shippertesting.LocalFetchChart,
		f.Recorder,
	)

	stopCh := make(chan struct{})
	defer close(stopCh)

	f.Run(stopCh)

	for controller.processNextWorkItem() {
		if controller.workqueue.Len() == 0 {
			time.Sleep(20 * time.Millisecond)
		}
		if controller.workqueue.Len() == 0 {
			return
		}
	}
}
