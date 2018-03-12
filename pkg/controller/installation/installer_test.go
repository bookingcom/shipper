package installation

import (
	"testing"

	shippertesting "github.com/bookingcom/shipper/pkg/testing"

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
		kubetesting.NewCreateAction(
			schema.GroupVersionResource{Resource: "deployments", Version: "v1beta1"},
			release.GetNamespace(),
			nil),
	}
	shippertesting.CheckActions(expectedActions, fakeDynamicClient.Actions(), t)
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
