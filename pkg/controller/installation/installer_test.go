package installation

import (
	"testing"

	shipperV1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
				Kind:       "Service",
				Namespaced: true,
				Name:       "services",
			},
			{
				Kind:       "Pod",
				Namespaced: true,
				Name:       "pods",
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
	cluster := loadCluster()
	installer := NewInstaller(release)

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
	if false {
		createDeploymentAction := fakeDynamicClient.Actions()[1].(kubetesting.CreateAction)
		obj := createDeploymentAction.GetObject()
		if deployment, ok := obj.(*appsv1.Deployment); !ok {
			t.Logf("%+v", obj)
			t.Fatal("object is not an appsv1.Deployment")
		} else {
			if _, ok := deployment.Labels[shipperV1.ReleaseLabel]; !ok {
				t.Fatalf("could not find %q in Deployment .metadata.labels", shipperV1.ReleaseLabel)
			}
			if _, ok := deployment.Spec.Selector.MatchLabels[shipperV1.ReleaseLabel]; !ok {
				t.Fatalf("could not find %q in Deployment .spec.selector.matchLabels", shipperV1.ReleaseLabel)
			}
			if _, ok := deployment.Spec.Template.Labels[shipperV1.ReleaseLabel]; !ok {
				t.Fatalf("could not find %q in Deployment .spec.template.labels", shipperV1.ReleaseLabel)
			}
		}
	}
}

// TestInstallerBrokenChart tests if the installation process fails when the
// release contains an invalid serialized chart.
func TestInstallerBrokenChart(t *testing.T) {
	release := loadRelease()
	release.Environment.Chart.Tarball = "deadbeef"

	cluster := loadCluster()
	installer := NewInstaller(release)

	fakeClient, _, _, fakeDynamicClientBuilder, _ := initializeClients(apiResourceList)

	restConfig := &rest.Config{}
	if err := installer.installRelease(cluster, fakeClient, restConfig, fakeDynamicClientBuilder); err == nil {
		t.Fatal("installRelease should fail, invalid tarball")
	}
}
