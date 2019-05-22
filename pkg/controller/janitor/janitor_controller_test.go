package janitor

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

// TestSuccessfulDeleteInstallationTarget exercises syncInstallationTarget(),
// which is triggered by deleting an installation target object from the
// management cluster.
func TestSuccessfulDeleteInstallationTarget(t *testing.T) {
	installationTarget := loadInstallationTarget()

	// Setup client without any objects since we'll enqueue a work item directly
	// later on.
	kubeFakeClientset, shipperFakeClientset, shipperInformerFactory, kubeInformerFactory :=
		setup(
			[]runtime.Object{},
			[]runtime.Object{},
		)

	// Setup cluster client store that'll be used by the controller to contact
	// application clusters to perform the anchor removal.
	fakeClusterClientStore := &FakeClusterClientStore{
		fakeClient:            kubeFakeClientset,
		sharedInformerFactory: kubeInformerFactory,
	}

	fakeRecorder := record.NewFakeRecorder(42)

	// Create the controller without waiting until the work queue is populated.
	// This is required since I couldn't find yet a way to trigger the Delete
	// handler from a test; that's the reason c.syncInstallationTarget() is
	// called directly later in this test.
	c := newController(
		false,
		kubeInformerFactory,
		shipperFakeClientset,
		shipperInformerFactory,
		fakeClusterClientStore,
		fakeRecorder)

	anchorName, err := CreateAnchorName(installationTarget)
	if err != nil {
		t.Fatal(err)
	}

	var key string
	if key, err = cache.MetaNamespaceKeyFunc(installationTarget); err != nil {
		t.Fatal(err)
	}

	item := &InstallationTargetWorkItem{
		AnchorName: anchorName,
		Clusters:   installationTarget.Spec.Clusters,
		Key:        key,
		Name:       installationTarget.Name,
		Namespace:  installationTarget.Namespace,
	}

	// Execute c.syncInstallationTarget() here since I couldn't find an API to
	// trigger the Delete event handler.
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

	shippertesting.CheckActions(expectedActions, kubeFakeClientset.Actions(), t)
}

// TestDeleteConfigMapAnchorInstallationTargetMatch should not delete anything,
// since the installation target object's UID matches the anchor config map
// synced from an application cluster.
func TestDeleteConfigMapAnchorInstallationTargetMatch(t *testing.T) {
	cluster := loadCluster("minikube-a")
	installationTarget := loadInstallationTarget()

	// Create a ConfigMap based on the existing installation target object.
	configMap, err := CreateConfigMapAnchor(installationTarget)
	if err != nil {
		t.Fatal(err)
	}

	// Setup clients with only the existing installation target object, that
	// will be retrieved and compared by syncAnchor() later on.
	kubeFakeClientset, shipperFakeClientset, shipperInformerFactory, kubeInformerFactory :=
		setup(
			[]runtime.Object{installationTarget},
			[]runtime.Object{},
		)

	// Setup cluster client store that'll be used by the controller to contact
	// application clusters to perform the anchor removal.
	fakeClusterClientStore := &FakeClusterClientStore{
		fakeClient:            kubeFakeClientset,
		sharedInformerFactory: kubeInformerFactory,
	}

	fakeRecorder := record.NewFakeRecorder(42)

	// Create the controller without waiting until the work queue is populated.
	// This is required since I couldn't find yet a way to trigger the Update
	// handler from a test; that's the reason c.syncAnchor() is called directly
	// later in this test.
	c := newController(
		false,
		kubeInformerFactory,
		shipperFakeClientset,
		shipperInformerFactory,
		fakeClusterClientStore,
		fakeRecorder)

	var key string
	if key, err = cache.MetaNamespaceKeyFunc(configMap); err != nil {
		t.Fatal(err)
	}

	item := &AnchorWorkItem{
		ClusterName:           cluster.Name,
		InstallationTargetUID: configMap.Data[InstallationTargetUID],
		Key:         key,
		Name:        configMap.Name,
		Namespace:   configMap.GetNamespace(),
		ReleaseName: configMap.GetLabels()[shipper.ReleaseLabel],
	}

	// Execute c.syncAnchor() here since I couldn't find an API to trigger the
	// Update event handler.
	if err := c.syncAnchor(item); err != nil {
		t.Fatal(err)
	}

	// Nothing should happen, ConfigMap's InstallationTargetUID key matches
	// existing installation target.
	expectedActions := []kubetesting.Action{}

	shippertesting.CheckActions(expectedActions, kubeFakeClientset.Actions(), t)
}

// TestDeleteConfigMapAnchorInstallationTargetUIDDoNotMatch exercises
// syncAnchor() for the case where an existing installation target object's
// UID differs from the installation target UID present in the anchor config
// map.
func TestDeleteConfigMapAnchorInstallationTargetUIDDoNotMatch(t *testing.T) {
	cluster := loadCluster("minikube-a")
	installationTarget := loadInstallationTarget()

	configMap, err := CreateConfigMapAnchor(installationTarget)
	if err != nil {
		t.Fatal(err)
	}

	// Change the installation target object's UID present in the config map.
	configMap.Data[InstallationTargetUID] = "some-other-installation-target-uid"

	// Setup only with the installation target object and the anchor config map.
	kubeFakeClientset, shipperFakeClientset, shipperInformerFactory, kubeInformerFactory :=
		setup(
			[]runtime.Object{installationTarget},
			[]runtime.Object{},
		)

	// Setup cluster client store that'll be used by the controller to contact
	// application clusters to perform the anchor removal.
	fakeClusterClientStore := &FakeClusterClientStore{
		fakeClient:            kubeFakeClientset,
		sharedInformerFactory: kubeInformerFactory,
	}

	fakeRecorder := record.NewFakeRecorder(42)

	// Create the controller without waiting until the work queue is populated.
	// This is required since I couldn't find a way to trigger the Update
	// handler from a test; that's the reason c.syncAnchor() is called directly
	// later on.
	c := newController(
		false,
		kubeInformerFactory,
		shipperFakeClientset,
		shipperInformerFactory,
		fakeClusterClientStore,
		fakeRecorder)

	var key string
	if key, err = cache.MetaNamespaceKeyFunc(configMap); err != nil {
		t.Fatal(err)
	}

	item := &AnchorWorkItem{
		ClusterName:           cluster.GetName(),
		InstallationTargetUID: configMap.Data[InstallationTargetUID],
		Key:         key,
		Name:        configMap.GetName(),
		Namespace:   configMap.GetNamespace(),
		ReleaseName: configMap.GetLabels()[shipper.ReleaseLabel],
	}

	// Execute c.syncAnchor() here since I couldn't find an API to trigger the
	// Update event handler.
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

	shippertesting.CheckActions(expectedActions, kubeFakeClientset.Actions(), t)
}

// TestDeleteConfigMapAnchorInstallationTargetDoesNotExist exercises
// syncAnchor() for the case where the installation target present in the
// anchor config map doesn't exist anymore in the management cluster.
func TestDeleteConfigMapAnchorInstallationTargetDoesNotExist(t *testing.T) {
	cluster := loadCluster("minikube-a")
	installationTarget := loadInstallationTarget()

	configMap, err := CreateConfigMapAnchor(installationTarget)
	if err != nil {
		t.Fatal(err)
	}

	// Setup without any extra object in the cache.
	kubeFakeClientset, shipperFakeClientset, shipperInformerFactory, kubeInformerFactory :=
		setup(
			[]runtime.Object{},
			[]runtime.Object{},
		)

	// Setup cluster client store that'll be used by the controller to contact
	// application clusters to perform the anchor removal.
	fakeClusterClientStore := &FakeClusterClientStore{
		fakeClient:            kubeFakeClientset,
		sharedInformerFactory: kubeInformerFactory,
	}

	fakeRecorder := record.NewFakeRecorder(42)

	// Create the controller without waiting until the work queue is populated.
	// This is required since I couldn't find a way to trigger the Update
	// handler from a test; that's the reason c.syncAnchor() is called directly
	// later on.
	c := newController(
		false,
		kubeInformerFactory,
		shipperFakeClientset,
		shipperInformerFactory,
		fakeClusterClientStore,
		fakeRecorder)

	var key string
	if key, err = cache.MetaNamespaceKeyFunc(configMap); err != nil {
		t.Fatal(err)
	}

	item := &AnchorWorkItem{
		ClusterName:           cluster.GetName(),
		InstallationTargetUID: configMap.Data[InstallationTargetUID],
		Key:         key,
		Name:        configMap.Name,
		Namespace:   configMap.GetNamespace(),
		ReleaseName: configMap.GetLabels()[shipper.ReleaseLabel],
	}

	// Execute c.syncAnchor() here since I couldn't find an API to trigger the
	// Update event handler.
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
	shippertesting.CheckActions(expectedActions, kubeFakeClientset.Actions(), t)
}
