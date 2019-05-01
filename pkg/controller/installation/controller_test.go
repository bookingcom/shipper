package installation

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	runtimeutil "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	"github.com/bookingcom/shipper/pkg/conditions"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

func init() {
	conditions.InstallationConditionsShouldDiscardTimestamps = true
}

func TestInstallIncumbent(t *testing.T) {
	cluster := buildCluster("minikube-a")
	appName := "reviews-api"
	testNs := "reviews-api"
	incumbentRel := buildRelease("0.0.1", testNs, "0", "deadbeef", appName)
	contenderRel := buildRelease("0.0.2", testNs, "1", "beefdead", appName)
	incumbentIt := buildInstallationTarget(incumbentRel, testNs, appName, []string{cluster.Name})

	shipperObjects := []runtime.Object{cluster, contenderRel, incumbentRel, incumbentIt}
	clientsPerCluster, shipperclientset, fakeDynamicClientBuilder, shipperInformerFactory := initializeClients(apiResourceList, shipperObjects, objectsPerClusterMap{cluster.Name: nil})
	clusterPair := clientsPerCluster[cluster.Name]

	fakeClientProvider := &FakeClientProvider{
		clientsPerCluster: clientsPerCluster,
		restConfig:        &rest.Config{},
	}
	fakeRecorder := record.NewFakeRecorder(42)

	c := newController(
		shipperclientset, shipperInformerFactory, fakeClientProvider, fakeDynamicClientBuilder, fakeRecorder)

	if !c.processNextWorkItem() {
		t.Fatal("Could not process work item")
	}

	expectedActions := []kubetesting.Action{}
	shippertesting.CheckActions(expectedActions, clusterPair.fakeDynamicClient.Actions(), t)
}

// TestInstallOneCluster tests the installation process using the
// installation.Controller.
func TestInstallOneCluster(t *testing.T) {
	cluster := buildCluster("minikube-a")
	appName := "reviews-api"
	testNs := "reviews-api"
	app := buildApplication(appName, appName)
	release := buildRelease("0.0.1", testNs, "0", "deadbeef", app.Name)
	installationTarget := buildInstallationTarget(release, testNs, appName, []string{cluster.Name})

	clientsPerCluster, shipperclientset, fakeDynamicClientBuilder, shipperInformerFactory :=
		initializeClients(apiResourceList, []runtime.Object{app, cluster, release, installationTarget}, objectsPerClusterMap{cluster.Name: []runtime.Object{}})

	clusterPair := clientsPerCluster[cluster.Name]
	fakeClientProvider := &FakeClientProvider{
		clientsPerCluster: clientsPerCluster,
		restConfig:        &rest.Config{},
	}

	fakeRecorder := record.NewFakeRecorder(42)

	c := newController(
		shipperclientset, shipperInformerFactory, fakeClientProvider, fakeDynamicClientBuilder, fakeRecorder)

	if !c.processNextWorkItem() {
		t.Fatal("Could not process work item")
	}

	// The chart contained in the test release produces a service and a deployment
	// manifest. The events order should be always the same, since we changed the
	// renderer behavior to always return a consistently ordered list of manifests,
	// according to Kind and Name. The actions described below are expected to be
	// executed against the dynamic client that is returned by the
	// fakeDynamicClientBuilder function passed to the controller, which mimics a
	// connection to a Target Cluster.
	expectedActions := []kubetesting.Action{
		kubetesting.NewGetAction(schema.GroupVersionResource{Resource: "configmaps", Version: "v1"}, release.GetNamespace(), "0.0.1-anchor"),
		kubetesting.NewCreateAction(schema.GroupVersionResource{Resource: "configmaps", Version: "v1"}, release.GetNamespace(), nil),
		kubetesting.NewGetAction(schema.GroupVersionResource{Resource: "services", Version: "v1"}, release.GetNamespace(), "0.0.1-reviews-api"),
		kubetesting.NewCreateAction(schema.GroupVersionResource{Resource: "services", Version: "v1"}, release.GetNamespace(), nil),
		kubetesting.NewGetAction(schema.GroupVersionResource{Resource: "deployments", Version: "v1", Group: "apps"}, release.GetNamespace(), "0.0.1-reviews-api"),
		kubetesting.NewCreateAction(schema.GroupVersionResource{Resource: "deployments", Version: "v1", Group: "apps"}, release.GetNamespace(), nil),
	}
	shippertesting.ShallowCheckActions(expectedActions, clusterPair.fakeDynamicClient.Actions(), t)

	// We are interested only in "update" actions here.
	var filteredActions []kubetesting.Action
	for _, a := range shipperclientset.Actions() {
		if a.GetVerb() == "update" {
			filteredActions = append(filteredActions, a)
		}
	}

	// Now we need to check if the installation target process was properly
	// patched.
	it := installationTarget.DeepCopy()
	it.Status.Clusters = []*shipper.ClusterInstallationStatus{
		{
			Name: "minikube-a", Status: shipper.InstallationStatusInstalled,
			Conditions: []shipper.ClusterInstallationCondition{
				{
					Type:   shipper.ClusterConditionTypeOperational,
					Status: corev1.ConditionTrue,
				},
				{
					Type:   shipper.ClusterConditionTypeReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
	expectedActions = []kubetesting.Action{
		kubetesting.NewUpdateAction(schema.GroupVersionResource{
			Resource: "installationtargets",
			Version:  shipper.SchemeGroupVersion.Version,
			Group:    shipper.SchemeGroupVersion.Group,
		}, release.GetNamespace(), it),
	}
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

func TestInstallMultipleClusters(t *testing.T) {
	clusterA := buildCluster("minikube-a")
	clusterB := buildCluster("minikube-b")
	appName := "reviews-api"
	testNs := "reviews-api"
	app := buildApplication(appName, testNs)
	release := buildRelease("0.0.1", testNs, "0", "deadbeef", appName)
	installationTarget := buildInstallationTarget(release, testNs, appName, []string{clusterA.Name, clusterB.Name})

	clientsPerCluster, shipperclientset, fakeDynamicClientBuilder, shipperInformerFactory :=
		initializeClients(apiResourceList, []runtime.Object{app, clusterA, clusterB, release, installationTarget}, objectsPerClusterMap{
			clusterA.Name: []runtime.Object{},
			clusterB.Name: []runtime.Object{},
		})

	fakeRecorder := record.NewFakeRecorder(42)

	fakeClientProvider := &FakeClientProvider{
		clientsPerCluster: clientsPerCluster,
		restConfig:        &rest.Config{},
	}

	c := newController(
		shipperclientset, shipperInformerFactory, fakeClientProvider, fakeDynamicClientBuilder, fakeRecorder)

	if !c.processNextWorkItem() {
		t.Fatal("Could not process work item")
	}

	// The chart contained in the test release produces a service and a deployment
	// manifest. The events order should be always the same, since we changed the
	// renderer behavior to always return a consistently ordered list of manifests,
	// according to Kind and Name. The actions described below are expected to be
	// executed against the dynamic client that is returned by the
	// fakeDynamicClientBuilder function passed to the controller, which mimics a
	// connection to a Target Cluster.
	expectedActions := []kubetesting.Action{
		kubetesting.NewGetAction(schema.GroupVersionResource{Resource: "configmaps", Version: "v1"}, release.GetNamespace(), "0.0.1-anchor"),
		kubetesting.NewCreateAction(schema.GroupVersionResource{Resource: "configmaps", Version: "v1"}, release.GetNamespace(), nil),
		kubetesting.NewGetAction(schema.GroupVersionResource{Resource: "services", Version: "v1"}, release.GetNamespace(), "0.0.1-reviews-api"),
		kubetesting.NewCreateAction(schema.GroupVersionResource{Resource: "services", Version: "v1"}, release.GetNamespace(), nil),
		kubetesting.NewGetAction(schema.GroupVersionResource{Resource: "deployments", Version: "v1", Group: "apps"}, release.GetNamespace(), "0.0.1-reviews-api"),
		kubetesting.NewCreateAction(schema.GroupVersionResource{Resource: "deployments", Version: "v1", Group: "apps"}, release.GetNamespace(), nil),
	}

	for _, fakePair := range clientsPerCluster {
		shippertesting.ShallowCheckActions(expectedActions, fakePair.fakeDynamicClient.Actions(), t)
	}

	// We are interested only in "update" actions here.
	var filteredActions []kubetesting.Action
	for _, a := range shipperclientset.Actions() {
		if a.GetVerb() == "update" {
			filteredActions = append(filteredActions, a)
		}
	}

	// Now we need to check if the installation target process was properly patched
	// and the clusters are listed in alphabetical order.
	it := installationTarget.DeepCopy()
	it.Status.Clusters = []*shipper.ClusterInstallationStatus{
		{
			Name:   "minikube-a",
			Status: shipper.InstallationStatusInstalled,
			Conditions: []shipper.ClusterInstallationCondition{
				{
					Type:   shipper.ClusterConditionTypeOperational,
					Status: corev1.ConditionTrue,
				},
				{
					Type:   shipper.ClusterConditionTypeReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
		{
			Name:   "minikube-b",
			Status: shipper.InstallationStatusInstalled,
			Conditions: []shipper.ClusterInstallationCondition{
				{
					Type:   shipper.ClusterConditionTypeOperational,
					Status: corev1.ConditionTrue,
				},
				{
					Type:   shipper.ClusterConditionTypeReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
	expectedActions = []kubetesting.Action{
		kubetesting.NewUpdateAction(schema.GroupVersionResource{
			Resource: "installationtargets",
			Version:  shipper.SchemeGroupVersion.Version,
			Group:    shipper.SchemeGroupVersion.Group,
		}, release.GetNamespace(), it),
	}
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

// TestMissingRelease tests a case that the installation target object is being
// processed but the release it refers to doesn't exist in the management
// cluster anymore.
//
// This doesn't raise an error, but handles it to Kubernetes runtime.HandleError
// instead, so this test checks whether or not HandleError has been called and
// if it has been called with the message we expect.
func TestMissingRelease(t *testing.T) {
	var shipperclientset *shipperfake.Clientset

	cluster := buildCluster("minikube-a")
	appName := "reviews-api"
	testNs := "reviews-api"
	installationTarget := buildInstallationTargetWithOwner("0.0.1", "deadbeef", testNs, appName, []string{cluster.Name})

	clientsPerCluster, shipperclientset, fakeDynamicClientBuilder, shipperInformerFactory :=
		initializeClients(apiResourceList, []runtime.Object{cluster, installationTarget}, nil)

	fakeClientProvider := &FakeClientProvider{
		clientsPerCluster: clientsPerCluster,
		restConfig:        &rest.Config{},
	}
	fakeRecorder := record.NewFakeRecorder(42)

	c := newController(
		shipperclientset, shipperInformerFactory, fakeClientProvider, fakeDynamicClientBuilder, fakeRecorder)

	handleErrors := 0
	runtimeutil.ErrorHandlers = []func(error){
		func(err error) {
			handleErrors++
		},
	}

	if !c.processNextWorkItem() {
		t.Fatal("Could not process work item")
	}

	const expectedHandleErrors = 1
	if handleErrors != expectedHandleErrors {
		t.Fatalf("expected %d handle errors, got %d instead", expectedHandleErrors, handleErrors)
	}
}

// TestClientError tests a case where an error has been returned by the
// clusterclientstore when requesting a client.
//
// This raises an error so the operation can be retried, but it also updates
// the installation target status, so this test checks whether the manifest has
// been updated with the desired status.
func TestClientError(t *testing.T) {
	var shipperclientset *shipperfake.Clientset

	cluster := buildCluster("minikube-a")
	appName := "reviews-api"
	testNs := "reviews-api"
	app := buildApplication(appName, testNs)
	release := buildRelease("0.0.1", testNs, "0", "deadbeef", appName)
	installationTarget := buildInstallationTarget(release, testNs, appName, []string{cluster.Name})

	clientsPerCluster, shipperclientset, fakeDynamicClientBuilder, shipperInformerFactory :=
		initializeClients(apiResourceList, []runtime.Object{app, release, cluster, installationTarget}, nil)

	fakeClientProvider := &FakeClientProvider{
		clientsPerCluster:   clientsPerCluster,
		restConfig:          &rest.Config{},
		getClientShouldFail: true,
	}
	fakeRecorder := record.NewFakeRecorder(42)

	c := newController(
		shipperclientset, shipperInformerFactory, fakeClientProvider, fakeDynamicClientBuilder, fakeRecorder)

	handleErrors := 0
	runtimeutil.ErrorHandlers = []func(error){
		func(err error) {
			handleErrors = handleErrors + 1
		},
	}

	if !c.processNextWorkItem() {
		t.Fatal("Could not process work item")
	}

	expectedHandleErrors := 1
	if handleErrors != expectedHandleErrors {
		t.Fatalf("expected %d handle errors, got %d instead", expectedHandleErrors, handleErrors)
	}

	it := installationTarget.DeepCopy()
	it.Status.Clusters = []*shipper.ClusterInstallationStatus{
		{
			Name:    "minikube-a",
			Status:  shipper.InstallationStatusFailed,
			Message: `cluster "minikube-a" not ready for use yet; cluster client is being initialized`,
			Conditions: []shipper.ClusterInstallationCondition{
				{
					Type:    shipper.ClusterConditionTypeOperational,
					Status:  corev1.ConditionFalse,
					Reason:  conditions.TargetClusterClientError,
					Message: `cluster "minikube-a" not ready for use yet; cluster client is being initialized`,
				},
				{
					Type:    shipper.ClusterConditionTypeReady,
					Status:  corev1.ConditionUnknown,
					Reason:  conditions.TargetClusterClientError,
					Message: `cluster "minikube-a" not ready for use yet; cluster client is being initialized`,
				},
			},
		},
	}
	expectedActions := []kubetesting.Action{
		kubetesting.NewUpdateAction(schema.GroupVersionResource{
			Resource: "installationtargets",
			Version:  shipper.SchemeGroupVersion.Version,
			Group:    shipper.SchemeGroupVersion.Group,
		}, release.GetNamespace(), it),
	}
	var filteredActions []kubetesting.Action
	for _, a := range shipperclientset.Actions() {
		if a.GetVerb() == "update" {
			filteredActions = append(filteredActions, a)
		}
	}

	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

// TestTargetClusterMissesGVK tests a case where the rendered manifest refers to
// a GroupVersionKind that the Target Cluster doesn't understand.
//
// This raises an error, but it also updates the installation target status, so
// this test checks whether the manifest has been updated with the desired
// status.
func TestTargetClusterMissesGVK(t *testing.T) {
	var shipperclientset *shipperfake.Clientset

	cluster := buildCluster("minikube-a")
	appName := "reviews-api"
	testNs := "reviews-api"
	app := buildApplication(appName, testNs)
	release := buildRelease("0.0.1", testNs, "0", "deadbeef", appName)
	installationTarget := buildInstallationTarget(release, testNs, appName, []string{cluster.Name})

	clientsPerCluster, shipperclientset, fakeDynamicClientBuilder, shipperInformerFactory :=
		initializeClients([]*v1.APIResourceList{}, []runtime.Object{app, release, cluster, installationTarget}, objectsPerClusterMap{cluster.Name: nil})

	fakeClientProvider := &FakeClientProvider{
		clientsPerCluster: clientsPerCluster,
		restConfig:        &rest.Config{},
	}
	fakeRecorder := record.NewFakeRecorder(42)

	c := newController(
		shipperclientset, shipperInformerFactory, fakeClientProvider, fakeDynamicClientBuilder, fakeRecorder)

	handleErrors := 0
	runtimeutil.ErrorHandlers = []func(error){
		func(err error) {
			handleErrors = handleErrors + 1
		},
	}

	if !c.processNextWorkItem() {
		t.Fatal("Could not process work item")
	}

	expectedHandleErrors := 1
	if handleErrors != expectedHandleErrors {
		t.Fatalf("expected %d handle errors, got %d instead", expectedHandleErrors, handleErrors)
	}

	it := installationTarget.DeepCopy()
	it.Status.Clusters = []*shipper.ClusterInstallationStatus{
		{
			Name:    "minikube-a",
			Status:  shipper.InstallationStatusFailed,
			Message: `failed to discover server resources for GroupVersion "v1": GroupVersion "v1" not found`,
			Conditions: []shipper.ClusterInstallationCondition{
				{
					Type:   shipper.ClusterConditionTypeOperational,
					Status: corev1.ConditionTrue,
				},
				{
					Type:    shipper.ClusterConditionTypeReady,
					Status:  corev1.ConditionFalse,
					Reason:  conditions.ServerError,
					Message: `failed to discover server resources for GroupVersion "v1": GroupVersion "v1" not found`,
				},
			},
		},
	}
	expectedActions := []kubetesting.Action{
		kubetesting.NewUpdateAction(schema.GroupVersionResource{
			Resource: "installationtargets",
			Version:  shipper.SchemeGroupVersion.Version,
			Group:    shipper.SchemeGroupVersion.Group,
		}, release.GetNamespace(), it),
	}
	var filteredActions []kubetesting.Action
	for _, a := range shipperclientset.Actions() {
		if a.GetVerb() == "update" {
			filteredActions = append(filteredActions, a)
		}
	}

	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

// TestManagementServerMissesCluster tests a case where the installation target
// refers to a cluster the management cluster doesn't know.
//
// This raises an error so the operation can be retried, but it also updates
// the installation target status, so this test checks whether the manifest has
// been updated with the desired status.
func TestManagementServerMissesCluster(t *testing.T) {
	var shipperclientset *shipperfake.Clientset

	appName := "reviews-api"
	testNs := "reviews-api"
	app := buildApplication(appName, testNs)
	release := buildRelease("0.0.1", testNs, "0", "deadbeef", appName)
	installationTarget := buildInstallationTarget(release, testNs, appName, []string{"minikube-a"})

	clientsPerCluster, shipperclientset, fakeDynamicClientBuilder, shipperInformerFactory :=
		initializeClients(apiResourceList, []runtime.Object{app, release, installationTarget}, nil)

	fakeClientProvider := &FakeClientProvider{
		clientsPerCluster: clientsPerCluster,
		restConfig:        &rest.Config{},
	}
	fakeRecorder := record.NewFakeRecorder(42)

	c := newController(
		shipperclientset, shipperInformerFactory, fakeClientProvider, fakeDynamicClientBuilder, fakeRecorder)

	handleErrors := 0
	runtimeutil.ErrorHandlers = []func(error){
		func(err error) {
			handleErrors = handleErrors + 1
		},
	}

	if !c.processNextWorkItem() {
		t.Fatal("Could not process work item")
	}

	expectedHandleErrors := 1
	if handleErrors != expectedHandleErrors {
		t.Fatalf("expected %d handle errors, got %d instead", expectedHandleErrors, handleErrors)
	}

	it := installationTarget.DeepCopy()
	it.Status.Clusters = []*shipper.ClusterInstallationStatus{
		{
			Name:    "minikube-a",
			Status:  shipper.InstallationStatusFailed,
			Message: `failed to GET Cluster "minikube-a": cluster.shipper.booking.com "minikube-a" not found`,
			Conditions: []shipper.ClusterInstallationCondition{
				{
					Type:    shipper.ClusterConditionTypeOperational,
					Status:  corev1.ConditionFalse,
					Reason:  conditions.ServerError,
					Message: `failed to GET Cluster "minikube-a": cluster.shipper.booking.com "minikube-a" not found`,
				},
				{
					Type:    shipper.ClusterConditionTypeReady,
					Status:  corev1.ConditionUnknown,
					Reason:  conditions.ServerError,
					Message: `failed to GET Cluster "minikube-a": cluster.shipper.booking.com "minikube-a" not found`,
				},
			},
		},
	}
	expectedActions := []kubetesting.Action{
		kubetesting.NewUpdateAction(schema.GroupVersionResource{
			Resource: "installationtargets",
			Version:  shipper.SchemeGroupVersion.Version,
			Group:    shipper.SchemeGroupVersion.Group,
		}, release.GetNamespace(), it),
	}
	var filteredActions []kubetesting.Action
	for _, a := range shipperclientset.Actions() {
		if a.GetVerb() == "update" {
			filteredActions = append(filteredActions, a)
		}
	}

	shippertesting.CheckActions(expectedActions, filteredActions, t)
}
