package installation

import (
	"testing"

	shipperV1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"

	appsV1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	kubetesting "k8s.io/client-go/testing"
)

// apiResourceList contains a list of APIResources containing some of v1 and
// extensions/v1beta1 resources from Kubernetes, since fake clients by default
// don't contain any reference which resources it can handle.
var apiResourceList = []*v1.APIResourceList{
	{
		GroupVersion: "v1",
		APIResources: []v1.APIResource{
			{
				Kind:       "Namespace",
				Namespaced: false,
				Name:       "namespaces",
				Group:      "",
			},
			{
				Kind:       "Service",
				Namespaced: true,
				Name:       "services",
				Group:      "",
			},
			{
				Kind:       "Pod",
				Namespaced: true,
				Name:       "pods",
				Group:      "",
			},
		},
	},
	{
		GroupVersion: "extensions/v1beta1",
		APIResources: []v1.APIResource{

			{
				Kind:       "Deployment",
				Namespaced: true,
				Name:       "deployments",
			},
		},
	},
	{
		GroupVersion: "apps/v1",
		APIResources: []v1.APIResource{

			{
				Kind:       "Deployment",
				Namespaced: true,
				Name:       "deployments",
			},
		},
	},
}

// TestInstaller tests the installation process using a Installer directly,
// using the chart found in testdata/release.yaml.
func TestInstaller(t *testing.T) {

	release := loadRelease()
	cluster := loadCluster("minikube-a")
	installer := newInstaller(release)

	fakeClient, _, fakeDynamicClient, fakeDynamicClientBuilder, _ := initializeClients(apiResourceList)

	restConfig := &rest.Config{}
	if err := installer.installRelease(cluster, fakeClient, restConfig, fakeDynamicClientBuilder); err != nil {
		t.Fatal(err)
	}

	// The chart contained in the test release produces a service and a
	// deployment manifest. The events order should be always the same,
	// since we changed the renderer behavior to always return a
	// consistently ordered list of manifests, according to Kind and
	// Name.
	expectedActions := []kubetesting.Action{
		kubetesting.NewCreateAction(
			schema.GroupVersionResource{Resource: "namespaces", Version: "v1"},
			release.GetNamespace(),
			nil),
		kubetesting.NewCreateAction(
			schema.GroupVersionResource{Resource: "services", Version: "v1"},
			release.GetNamespace(),
			nil),
		kubetesting.NewCreateAction( // TODO: Feed deployment object for comparison
			schema.GroupVersionResource{Resource: "deployments", Version: "v1", Group: "apps"},
			release.GetNamespace(),
			nil),
	}
	shippertesting.CheckActions(expectedActions, fakeDynamicClient.Actions(), t)

	// The following tests are currently disabled. It seems the cast to
	// appsv1.Deployment doesn't work because the runtime.Object stored
	// in the action isn't the object itself, but a serializable
	// representation of it.
	scheme := kubescheme.Scheme

	createServiceAction := fakeDynamicClient.Actions()[1].(kubetesting.CreateAction)
	obj := createServiceAction.GetObject()
	if obj.GetObjectKind().GroupVersionKind().Kind != "Service" {
		t.Logf("%+v", obj)
		t.Fatal("object is not a corev1.Service")
	} else {
		u := &unstructured.Unstructured{}
		err := scheme.Convert(obj, u, nil)
		if err != nil {
			panic(err)
		}
		if _, ok := u.GetLabels()[shipperV1.ReleaseLabel]; !ok {
			t.Fatalf("could not find %q in Deployment .metadata.labels", shipperV1.ReleaseLabel)
		}
	}

	createDeploymentAction := fakeDynamicClient.Actions()[2].(kubetesting.CreateAction)
	obj = createDeploymentAction.GetObject()
	if obj.GetObjectKind().GroupVersionKind().Kind != "Deployment" {
		t.Logf("%+v", obj)
		t.Fatal("object is not an appsv1.Deployment")
	} else {
		u := &unstructured.Unstructured{}
		err := scheme.Convert(obj, u, nil)
		if err != nil {
			panic(err)
		}
		if _, ok := u.GetLabels()[shipperV1.ReleaseLabel]; !ok {
			t.Fatalf("could not find %q in Deployment .metadata.labels", shipperV1.ReleaseLabel)
		}

		deployment := &appsV1.Deployment{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, deployment); err != nil {
			t.Fatalf("could not decode deployment from unstructured: %s", err)
		}

		if _, ok := deployment.Spec.Selector.MatchLabels[shipperV1.ReleaseLabel]; !ok {
			t.Fatal("deployment .spec.selector.matchLabels doesn't contain shipperV1.ReleaseLabel")
		}

		if _, ok := deployment.Spec.Template.Labels[shipperV1.ReleaseLabel]; !ok {
			t.Fatal("deployment .spec.template.labels doesn't contain shipperV1.ReleaseLabel")
		}

		const (
			existingChartLabel      = "app"
			existingChartLabelValue = "reviews-api"
		)

		actualChartLabelValue, ok := deployment.Spec.Template.Labels[existingChartLabel]
		if !ok {
			t.Fatalf("deployment .spec.template.labels doesn't contain a label (%q) which was present in the chart", existingChartLabel)
		}

		if actualChartLabelValue != existingChartLabelValue {
			t.Fatalf(
				"deployment .spec.template.labels has the right previously-existing label (%q) but wrong value. Expected %q but got %q",
				existingChartLabel, existingChartLabelValue, actualChartLabelValue,
			)
		}
	}
}

// TestInstallerBrokenChartTarball tests if the installation process fails when the
// release contains an invalid serialized chart.
func TestInstallerBrokenChartTarball(t *testing.T) {
	release := loadRelease()
	// there is a reviews-api-invalid-tarball.tgz in testdata which contains invalid deployment and service templates
	release.Environment.Chart.Version = "invalid-tarball"

	cluster := loadCluster("minikube-a")
	installer := newInstaller(release)

	fakeClient, _, _, fakeDynamicClientBuilder, _ := initializeClients(apiResourceList)

	restConfig := &rest.Config{}
	if err := installer.installRelease(cluster, fakeClient, restConfig, fakeDynamicClientBuilder); err == nil {
		t.Fatal("installRelease should fail, invalid tarball")
	}
}

// TestInstallerBrokenChartContents tests if the installation process fails when the
// release contains a valid chart tarball with invalid K8s object templates
func TestInstallerBrokenChartContents(t *testing.T) {
	release := loadRelease()
	// there is a reviews-api-invalid-k8s-objects.tgz in testdata which contains invalid deployment and service templates
	release.Environment.Chart.Version = "invalid-k8s-objects"

	cluster := loadCluster("minikube-a")
	installer := newInstaller(release)

	fakeClient, _, _, fakeDynamicClientBuilder, _ := initializeClients(apiResourceList)

	restConfig := &rest.Config{}
	if err := installer.installRelease(cluster, fakeClient, restConfig, fakeDynamicClientBuilder); err == nil {
		t.Fatal("installRelease should fail, invalid k8s objects")
	}
}
