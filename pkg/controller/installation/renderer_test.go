package installation

import (
	"fmt"
	"regexp"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

var clusters = []string{"cluster-a"}

// TestRendererSingleServiceLB tests that the renderer successfully adds the LB
// label to a chart with a single service, both to services that do have a
// label already, and also to ones that don't. It also checks that selectors
// are correctly set.
func TestRendererSingleService(t *testing.T) {
	chartVersions := []string{
		"single-service-no-lb",
		"single-service-with-lb",
		"no-service-selector",
	}
	svcName := fmt.Sprintf("%s-%s", shippertesting.TestApp, reviewsChartName)

	for _, chartVersion := range chartVersions {
		it := buildInstallationTarget(
			shippertesting.TestNamespace,
			shippertesting.TestApp,
			clusters,
			buildChart(reviewsChartName, chartVersion))

		objects, err := FetchAndRenderChart(shippertesting.LocalFetchChart, it)
		if err != nil {
			t.Fatalf("expected rendered chart %q, got error instead: %s", chartVersion, err.Error())
		}

		err = validatePrimaryService(objects, svcName)
		if err != nil {
			t.Fatalf("chart %q failed to render a valid primary service: %s", chartVersion, err.Error())
		}
	}
}

// TestRendererMultiService tests that the renderer doesn't touch the LB label
// on any services in a chart containing multiple services with one of them
// correctly labeled, and that selectors are correctly set.
func TestRendererMultiService(t *testing.T) {
	tests := []struct {
		chartName        string
		chartVersion     string
		primarySvcName   string
		secondarySvcName string
	}{
		{
			reviewsChartName,
			"multi-service-with-lb",
			fmt.Sprintf("%s-%s", shippertesting.TestApp, reviewsChartName),
			fmt.Sprintf("%s-%s-staging", shippertesting.TestApp, reviewsChartName),
		},
		{
			nginxChartName,
			"0.1.0",
			"nginx",
			"nginx-staging",
		},
	}

	for _, test := range tests {
		it := buildInstallationTarget(
			shippertesting.TestNamespace,
			shippertesting.TestApp,
			clusters,
			buildChart(test.chartName, test.chartVersion))

		objects, err := FetchAndRenderChart(shippertesting.LocalFetchChart, it)
		if err != nil {
			t.Fatalf("expected rendered chart %q, got error instead: %s", test.chartVersion, err.Error())
		}

		err = validatePrimaryService(objects, test.primarySvcName)
		if err != nil {
			t.Fatalf("chart %q failed to render a valid primary service: %s", test.chartVersion, err.Error())
		}

		err = validateSecondaryService(objects, test.secondarySvcName)
		if err != nil {
			t.Fatalf("chart %q failed to render a valid secondary service: %s", test.chartVersion, err.Error())
		}
	}
}

// TestRendererBrokenChartTarball tests if the renderer returns an error for a
// chart that points to a broken tarball.
func TestRendererBrokenChartTarball(t *testing.T) {
	it := buildInstallationTarget(
		shippertesting.TestNamespace,
		shippertesting.TestApp,
		clusters,
		buildChart("reviews-api", "broken-tarball"))

	_, err := FetchAndRenderChart(shippertesting.LocalFetchChart, it)

	if err == nil {
		t.Fatal("FetchAndRenderChart should return error, invalid tarball")
	}

	if _, ok := err.(shippererrors.RenderManifestError); !ok {
		t.Fatalf("FetchAndRenderChart should fail with RenderManifestError, got %v instead", err)
	}
}

// TestRendererBrokenObjects tests if the renderer returns an error when the
// the chart has manifests that were encoded improperly.
func TestRendererBrokenObjects(t *testing.T) {
	it := buildInstallationTarget(
		shippertesting.TestNamespace,
		shippertesting.TestApp,
		clusters,
		buildChart(reviewsChartName, "broken-k8s-objects"))

	_, err := FetchAndRenderChart(shippertesting.LocalFetchChart, it)

	if err == nil {
		t.Fatal("FetchAndRenderChart should return error, broken serialization")
	}

	if _, ok := err.(shippererrors.RenderManifestError); !ok {
		t.Fatalf("FetchAndRenderChart should fail with RenderManifestError, got %v instead", err)
	}
}

// TestRendererInvalidDeploymentName tests if the renderer returns an error
// when the chart renders a deployment that doesn't have a name templated with
// the release's name.
func TestRendererInvalidDeploymentName(t *testing.T) {
	it := buildInstallationTarget(
		shippertesting.TestNamespace,
		shippertesting.TestApp,
		clusters,
		buildChart(reviewsChartName, "invalid-deployment-name"))

	_, err := FetchAndRenderChart(shippertesting.LocalFetchChart, it)

	if err == nil {
		t.Fatal("FetchAndRenderChart should fail, invalid deployment name")
	}

	if _, ok := err.(shippererrors.InvalidChartError); !ok {
		t.Fatalf("FetchAndRenderChart should fail with InvalidChartError, got %v instead", err)
	}

	t.Logf("FetchAndRenderChart failed as expected. errors was: %s", err.Error())
}

// TestRendererMultiServiceNoLB tests if the renderer returns an error when the
// chart renders multiple services, but none with LBLabel to denote the one
// that Shipper should use for traffic shifting.
func TestRendererMultiServiceNoLB(t *testing.T) {
	it := buildInstallationTarget(
		shippertesting.TestNamespace,
		shippertesting.TestApp,
		clusters,
		buildChart(reviewsChartName, "multi-service-no-lb"))

	_, err := FetchAndRenderChart(shippertesting.LocalFetchChart, it)

	if err == nil {
		t.Fatal("FetchAndRenderChart should fail, chart has multiple services but none with LBLabel")
	}

	expected := fmt.Sprintf(
		`one and only one v1.Service object with label %q is required, but 0 found instead`,
		shipper.LBLabel)

	if err.Error() != expected {
		t.Fatalf(
			"FetchAndRenderChart should fail with %q, got different error instead: %s",
			expected, err)
	}
}

func TestRendererServiceWithReleaseNoWorkaround(t *testing.T) {
	it := buildInstallationTarget(
		shippertesting.TestNamespace,
		shippertesting.TestApp,
		clusters,
		buildChart(reviewsChartName, "0.0.1"))

	// Disabling the helm workaround
	delete(it.ObjectMeta.Labels, shipper.HelmWorkaroundLabel)

	_, err := FetchAndRenderChart(shippertesting.LocalFetchChart, it)

	if err == nil {
		t.Fatal("Expected error, none raised")
	}
	if matched, regexErr := regexp.MatchString("This will break shipper traffic shifting logic", err.Error()); regexErr != nil {
		t.Fatalf("Failed to match the error message against the regex: %s", regexErr)
	} else if !matched {
		t.Fatalf("Unexpected error: %s", err)
	}
}

func validatePrimaryService(objects []runtime.Object, name string) error {
	svcObj := findKubeObject(objects, "Service", name)
	if svcObj == nil {
		return fmt.Errorf("service %q not found", name)
	}

	svc := svcObj.(*corev1.Service)
	lbLabel, ok := svc.Labels[shipper.LBLabel]
	if !ok {
		return fmt.Errorf("label %q missing", shipper.LBLabel)
	} else if lbLabel != shipper.LBForProduction {
		return fmt.Errorf("label %q expected to be %q, was %q", shipper.LBLabel, shipper.LBForProduction, lbLabel)
	}

	expectedSelector := map[string]string{
		shipper.AppLabel:              shippertesting.TestApp,
		shipper.PodTrafficStatusLabel: shipper.Enabled,
	}
	for k, v := range expectedSelector {
		selector := svc.Spec.Selector[k]
		if selector != v {
			return fmt.Errorf("selector %q expected to be %q, was %q", k, v, selector)
		}
	}

	return nil
}

func validateSecondaryService(objects []runtime.Object, name string) error {
	svcObj := findKubeObject(objects, "Service", name)
	if svcObj == nil {
		return fmt.Errorf("service %q not found", name)
	}

	svc := svcObj.(*corev1.Service)
	lbLabel, ok := svc.Labels[shipper.LBLabel]
	if ok {
		return fmt.Errorf("label %q expected to absent, was %q", shipper.LBLabel, lbLabel)
	}

	unexpectedSelector := []string{
		shipper.AppLabel,
		shipper.PodTrafficStatusLabel,
	}
	for _, k := range unexpectedSelector {
		selector, ok := svc.Spec.Selector[k]
		if ok {
			return fmt.Errorf("selector %q expected to be absent, was %q", k, selector)
		}
	}

	return nil
}

func findKubeObject(objects []runtime.Object, kind, name string) runtime.Object {
	for _, obj := range objects {
		objmeta := obj.(metav1.Object)
		objkind := obj.GetObjectKind().GroupVersionKind().Kind
		if objkind == kind && objmeta.GetName() == name {
			return obj
		}
	}

	return nil
}
