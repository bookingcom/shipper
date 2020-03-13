package janitor

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
	"github.com/bookingcom/shipper/pkg/util/anchor"
)

// TestSuccessfulDeleteInstallationTarget exercises syncInstallationTarget(),
// which is triggered by deleting an installation target object from the
// management cluster.
func TestSuccessfulDeleteInstallationTarget(t *testing.T) {
	installationTarget := buildInstallationTarget()

	f := shippertesting.NewControllerTestFixture()
	cluster := f.AddNamedCluster(shippertesting.TestCluster)

	c := runController(f)

	key, err := cache.MetaNamespaceKeyFunc(installationTarget)
	if err != nil {
		t.Fatal(err)
	}

	item := &InstallationTargetWorkItem{
		AnchorName: anchor.CreateAnchorName(installationTarget),
		Clusters:   installationTarget.Spec.Clusters,
		Key:        key,
		Name:       installationTarget.Name,
		Namespace:  installationTarget.Namespace,
	}

	if err := c.syncInstallationTarget(item); err != nil {
		t.Fatal(err)
	}

	expectedActions := []kubetesting.Action{
		kubetesting.NewDeleteAction(
			schema.GroupVersionResource{Resource: string(corev1.ResourceConfigMaps), Version: "v1"},
			item.Namespace,
			item.AnchorName,
		),
	}

	actual := shippertesting.FilterActions(cluster.KubeClient.Actions())
	shippertesting.CheckActions(expectedActions, actual, t)
}

// TestDeleteConfigMapAnchorInstallationTargetMatch should not delete anything,
// since the installation target object's UID matches the anchor config map
// synced from an application cluster.
func TestDeleteConfigMapAnchorInstallationTargetMatch(t *testing.T) {
	f := shippertesting.NewControllerTestFixture()
	cluster := f.AddNamedCluster(shippertesting.TestCluster)

	installationTarget := buildInstallationTarget()
	f.ShipperClient.Tracker().Add(installationTarget)

	configMap := anchor.CreateConfigMapAnchor(installationTarget)
	cluster.AddOne(configMap)

	c := runController(f)

	key, err := cache.MetaNamespaceKeyFunc(configMap)
	if err != nil {
		t.Fatal(err)
	}

	item := &AnchorWorkItem{
		ClusterName:           cluster.Name,
		InstallationTargetUID: configMap.Data[InstallationTargetUID],
		Key:                   key,
		Name:                  configMap.Name,
		Namespace:             configMap.GetNamespace(),
		ReleaseName:           configMap.GetLabels()[shipper.ReleaseLabel],
	}

	if err := c.syncAnchor(item); err != nil {
		t.Fatal(err)
	}

	// Nothing should happen, ConfigMap's InstallationTargetUID key matches
	// existing installation target.
	expectedActions := []kubetesting.Action{}

	actual := shippertesting.FilterActions(cluster.KubeClient.Actions())
	shippertesting.CheckActions(expectedActions, actual, t)
}

// TestDeleteConfigMapAnchorInstallationTargetUIDDoNotMatch exercises
// syncAnchor() for the case where an existing installation target object's
// UID differs from the installation target UID present in the anchor config
// map.
func TestDeleteConfigMapAnchorInstallationTargetUIDDoNotMatch(t *testing.T) {
	f := shippertesting.NewControllerTestFixture()
	cluster := f.AddNamedCluster(shippertesting.TestCluster)

	installationTarget := buildInstallationTarget()
	f.ShipperClient.Tracker().Add(installationTarget)

	// Change the installation target object's UID present in the config map.
	configMap := anchor.CreateConfigMapAnchor(installationTarget)
	configMap.Data[InstallationTargetUID] = "some-other-installation-target-uid"

	c := runController(f)

	key, err := cache.MetaNamespaceKeyFunc(configMap)
	if err != nil {
		t.Fatal(err)
	}

	item := &AnchorWorkItem{
		ClusterName:           cluster.Name,
		InstallationTargetUID: configMap.Data[InstallationTargetUID],
		Key:                   key,
		Name:                  configMap.GetName(),
		Namespace:             configMap.GetNamespace(),
		ReleaseName:           configMap.GetLabels()[shipper.ReleaseLabel],
	}

	if err := c.syncAnchor(item); err != nil {
		t.Fatal(err)
	}

	expectedActions := []kubetesting.Action{
		kubetesting.NewDeleteAction(
			schema.GroupVersionResource{Resource: string(corev1.ResourceConfigMaps), Version: "v1"},
			configMap.GetNamespace(),
			configMap.GetName(),
		),
	}

	actual := shippertesting.FilterActions(cluster.KubeClient.Actions())
	shippertesting.CheckActions(expectedActions, actual, t)
}

// TestDeleteConfigMapAnchorInstallationTargetDoesNotExist exercises
// syncAnchor() for the case where the installation target present in the
// anchor config map doesn't exist anymore in the management cluster.
func TestDeleteConfigMapAnchorInstallationTargetDoesNotExist(t *testing.T) {
	f := shippertesting.NewControllerTestFixture()
	cluster := f.AddNamedCluster(shippertesting.TestCluster)

	installationTarget := buildInstallationTarget()
	configMap := anchor.CreateConfigMapAnchor(installationTarget)
	cluster.AddOne(configMap)

	c := runController(f)

	key, err := cache.MetaNamespaceKeyFunc(configMap)
	if err != nil {
		t.Fatal(err)
	}

	item := &AnchorWorkItem{
		ClusterName:           cluster.Name,
		InstallationTargetUID: configMap.Data[InstallationTargetUID],
		Key:                   key,
		Name:                  configMap.Name,
		Namespace:             configMap.GetNamespace(),
		ReleaseName:           configMap.GetLabels()[shipper.ReleaseLabel],
	}

	if err := c.syncAnchor(item); err != nil {
		t.Fatal(err)
	}

	expectedActions := []kubetesting.Action{
		kubetesting.NewDeleteAction(
			schema.GroupVersionResource{Resource: string(corev1.ResourceConfigMaps), Version: "v1"},
			configMap.GetNamespace(),
			configMap.GetName(),
		),
	}

	actual := shippertesting.FilterActions(cluster.KubeClient.Actions())
	shippertesting.CheckActions(expectedActions, actual, t)
}

func runController(f *shippertesting.ControllerTestFixture) *Controller {
	c := NewController(
		f.ShipperClient,
		f.ShipperInformerFactory,
		f.ClusterClientStore,
		f.Recorder,
	)

	stopCh := make(chan struct{})
	defer close(stopCh)

	f.Run(stopCh)

	return c
}

func buildInstallationTarget() *shipper.InstallationTarget {
	return &shipper.InstallationTarget{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: shippertesting.TestNamespace,
			Name:      shippertesting.TestApp,
			UID:       "deadbeef",
			Labels: map[string]string{
				shipper.ReleaseLabel: shippertesting.TestApp,
			},
		},
		Spec: shipper.InstallationTargetSpec{
			Clusters: []string{shippertesting.TestCluster},
		},
	}
}
