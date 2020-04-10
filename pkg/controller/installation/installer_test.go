package installation

import (
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
	kubetesting "k8s.io/client-go/testing"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

var (
	configmapGVR = schema.GroupVersionResource{Resource: "configmaps", Version: "v1"}
	svcGVR       = schema.GroupVersionResource{Resource: "services", Version: "v1"}
	baselineSvc  = &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: shippertesting.TestNamespace,
			Name:      fmt.Sprintf("%s-%s", shippertesting.TestApp, reviewsChartName),
			Labels: map[string]string{
				shipper.AppLabel:                     shippertesting.TestApp,
				shipper.LBLabel:                      shipper.LBForProduction,
				shipper.InstallationTargetOwnerLabel: "some-installation-target",
				shipper.HelmWorkaroundLabel:          "true",
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Name:       "nginx",
					Protocol:   "TCP",
					Port:       80,
					TargetPort: intstr.FromInt(80),
				},
			},
			Selector: map[string]string{
				shipper.AppLabel:              shippertesting.TestApp,
				shipper.PodTrafficStatusLabel: shipper.Enabled,
			},
		},
	}
)

// TestInstallerCleanInstall tests that the installer correctly creates the
// objects it was configured to install.
func TestInstallerCleanInstall(t *testing.T) {
	it := buildInstallationTarget(
		shippertesting.TestNamespace,
		shippertesting.TestApp,
		buildChart(reviewsChartName, "0.0.1"))

	kubeObjects := []runtime.Object{}
	anchoredSvc := convertToAnchoredUnstructured(baselineSvc.DeepCopy(), it)

	expectedDynamicActions := []kubetesting.Action{
		kubetesting.NewCreateAction(svcGVR, shippertesting.TestNamespace, anchoredSvc),
	}

	runInstallerTest(t, it, kubeObjects, expectedDynamicActions)
}

// TestInstallerExistingButNoOwners tests that the installer updates existing
// objects to add a new OwnerReference to the related InstallationTarget
func TestInstallerExistingButNoOwners(t *testing.T) {
	it := buildInstallationTarget(
		shippertesting.TestNamespace,
		shippertesting.TestApp,
		buildChart(reviewsChartName, "0.0.1"))

	kubeObjects := []runtime.Object{
		baselineSvc.DeepCopy(),
	}
	anchoredSvc := convertToAnchoredUnstructured(baselineSvc.DeepCopy(), it)

	expectedDynamicActions := []kubetesting.Action{
		kubetesting.NewUpdateAction(svcGVR, shippertesting.TestNamespace, anchoredSvc),
	}

	runInstallerTest(t, it, kubeObjects, expectedDynamicActions)
}

// TestInstallerExistingOwners tests that the installer updates existing
// objects to add a new OwnerReference to the related InstallationTarget. This
// does not replace the previous OwnerReferences, but adds to it.
func TestInstallerExistingOwners(t *testing.T) {
	it := buildInstallationTarget(
		shippertesting.TestNamespace,
		shippertesting.TestApp,
		buildChart(reviewsChartName, "0.0.1"))

	ownedService := baselineSvc.DeepCopy()
	ownedService.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion: shipper.SchemeGroupVersion.String(),
			Kind:       "InstallationTarget",
			Name:       "some-other-installation-target",
			UID:        "deadbeef",
		},
	})

	kubeObjects := []runtime.Object{
		ownedService,
	}

	anchoredSvc := convertToAnchoredUnstructured(ownedService.DeepCopy(), it)

	expectedDynamicActions := []kubetesting.Action{
		kubetesting.NewUpdateAction(svcGVR, shippertesting.TestNamespace, anchoredSvc),
	}

	runInstallerTest(t, it, kubeObjects, expectedDynamicActions)
}

func TestInstallerExistingConfigmap(t *testing.T) {
	it := buildInstallationTarget(
		shippertesting.TestNamespace,
		shippertesting.TestApp,
		buildChart(reviewsChartName, "0.0.1"))

	configmap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: shippertesting.TestNamespace,
			Name:      fmt.Sprintf("%s-anchor", it.Name),
		},
	}

	ownedService := baselineSvc.DeepCopy()
	ownedService.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion: "v1",
			Kind:       "ConfigMap",
			Name:       configmap.Name,
		},
	})

	t.Logf(configmap.GetName())

	kubeObjects := []runtime.Object{configmap, ownedService}

	anchoredSvc := convertToAnchoredUnstructured(ownedService.DeepCopy(), it)
	updatedConfigmap := configmap.DeepCopy()
	updatedConfigmap.OwnerReferences = []metav1.OwnerReference{
		buildInstallationTargetOwnerRef(it),
	}

	expectedActions := []kubetesting.Action{
		kubetesting.NewUpdateAction(configmapGVR, shippertesting.TestNamespace, updatedConfigmap),
	}
	expectedDynamicActions := []kubetesting.Action{
		kubetesting.NewUpdateAction(svcGVR, shippertesting.TestNamespace, anchoredSvc),
	}

	f := runInstallerTest(t, it, kubeObjects, expectedDynamicActions)

	filteredActions := shippertesting.FilterActions(f.KubeClient.Actions())
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

// TestInstallerNoOverride verifies that an InstallationTarget with disabled
// overrides does not try to update existing resources that it does not own.
func TestInstallerNoOverride(t *testing.T) {
	it := buildInstallationTarget(
		shippertesting.TestNamespace,
		shippertesting.TestApp,
		buildChart(reviewsChartName, "0.0.1"))
	it.Spec.CanOverride = false

	svc := baselineSvc.DeepCopy()
	svc.Labels[shipper.InstallationTargetOwnerLabel] = "some-other-installation-target"

	kubeObjects := []runtime.Object{
		svc,
	}

	expectedDynamicActions := []kubetesting.Action{}

	runInstallerTest(t, it, kubeObjects, expectedDynamicActions)
}

// newInstaller returns an installer configured to install a single service
// object. We don't need any more complex objects to be installed, as the logic
// of the installer is to simply put the objects as it receives into the
// cluster.
func newInstaller(it *shipper.InstallationTarget) *Installer {
	var svc = baselineSvc.DeepCopy()
	return NewInstaller(it, []runtime.Object{svc})
}

// convertToAnchoredUnstructured converts a k8s object into an unstructured
// one, and adds an OwnerReference to obj that points to the passed
// InstallationTarget. This is very useful for creating Create/Update dynamic
// actions.
func convertToAnchoredUnstructured(
	obj runtime.Object,
	it *shipper.InstallationTarget,
) *unstructured.Unstructured {
	converted := &unstructured.Unstructured{}
	err := kubescheme.Scheme.Convert(obj, converted, nil)
	if err != nil {
		panic(fmt.Sprintf("error converting object to unstructured: %s", err))
	}

	converted.SetOwnerReferences(append(
		[]metav1.OwnerReference{buildInstallationTargetOwnerRef(it)},
		converted.GetOwnerReferences()...))

	return converted
}

func buildInstallationTargetOwnerRef(it *shipper.InstallationTarget) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: shipper.SchemeGroupVersion.String(),
		Kind:       "InstallationTarget",
		Name:       it.Name,
		UID:        it.UID,
	}
}

func runInstallerTest(
	t *testing.T,
	it *shipper.InstallationTarget,
	objects []runtime.Object,
	dynamicActions []kubetesting.Action,
) *shippertesting.ControllerTestFixture {
	installer := newInstaller(it)

	f := newFixture(objects)

	for _, obj := range objects {
		f.KubeClient.Tracker().Add(obj)
	}

	stopCh := make(chan struct{})
	defer close(stopCh)

	f.Run(stopCh)

	err := installer.install(f.KubeClient, f.DynamicClientBuilder)
	if err != nil {
		t.Fatal(err)
	}

	filteredActions := shippertesting.FilterActions(f.DynamicClient.Actions())
	shippertesting.CheckActions(dynamicActions, filteredActions, t)

	return f
}
