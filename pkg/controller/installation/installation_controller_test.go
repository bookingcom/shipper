package installation

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

const (
	repoUrl = "https://charts.example.com"
)

// TestInstallOneCluster tests the installation process using the
// installation.Controller.
func TestInstallOneCluster(t *testing.T) {
	cluster := buildCluster("minikube-a")
	appName := "reviews-api"
	testNs := "test-namespace"
	chart := buildChart(appName, "0.0.1", repoUrl)
	installationTarget := buildInstallationTarget(testNs, appName, []string{cluster.Name}, &chart)

	clientsPerCluster, shipperclientset, fakeDynamicClientBuilder, shipperInformerFactory :=
		initializeClients(apiResourceList, []runtime.Object{cluster, installationTarget}, objectsPerClusterMap{cluster.Name: []runtime.Object{}})

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
	expectedDynamicActions := []kubetesting.Action{
		kubetesting.NewGetAction(schema.GroupVersionResource{Resource: "services", Version: "v1"}, testNs, "0.0.1-reviews-api"),
		kubetesting.NewCreateAction(schema.GroupVersionResource{Resource: "services", Version: "v1"}, testNs, nil),
		kubetesting.NewGetAction(schema.GroupVersionResource{Resource: "deployments", Version: "v1", Group: "apps"}, testNs, "0.0.1-reviews-api"),
		kubetesting.NewCreateAction(schema.GroupVersionResource{Resource: "deployments", Version: "v1", Group: "apps"}, testNs, nil),
	}
	shippertesting.ShallowCheckActions(expectedDynamicActions, clusterPair.fakeDynamicClient.Actions(), t)

	expectedActions := []kubetesting.Action{
		kubetesting.NewGetAction(schema.GroupVersionResource{Resource: "configmaps", Version: "v1"}, testNs, "0.0.1-anchor"),
		kubetesting.NewCreateAction(schema.GroupVersionResource{Resource: "configmaps", Version: "v1"}, testNs, nil),
		shippertesting.NewDiscoveryAction("services"),
		shippertesting.NewDiscoveryAction("deployments"),
	}
	shippertesting.ShallowCheckActions(expectedActions, clusterPair.fakeClient.Actions(), t)

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
	it.Spec.CanOverride = false
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
		}, testNs, it),
	}
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

func TestInstallMultipleClusters(t *testing.T) {
	clusterA := buildCluster("minikube-a")
	clusterB := buildCluster("minikube-b")
	appName := "reviews-api"
	testNs := "reviews-api"
	chart := buildChart(appName, "0.0.1", repoUrl)
	installationTarget := buildInstallationTarget(testNs, appName, []string{clusterA.Name, clusterB.Name}, &chart)

	clientsPerCluster, shipperclientset, fakeDynamicClientBuilder, shipperInformerFactory :=
		initializeClients(apiResourceList, []runtime.Object{clusterA, clusterB, installationTarget}, objectsPerClusterMap{
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
	expectedDynamicActions := []kubetesting.Action{
		kubetesting.NewGetAction(schema.GroupVersionResource{Resource: "services", Version: "v1"}, testNs, "0.0.1-reviews-api"),
		kubetesting.NewCreateAction(schema.GroupVersionResource{Resource: "services", Version: "v1"}, testNs, nil),
		kubetesting.NewGetAction(schema.GroupVersionResource{Resource: "deployments", Version: "v1", Group: "apps"}, testNs, "0.0.1-reviews-api"),
		kubetesting.NewCreateAction(schema.GroupVersionResource{Resource: "deployments", Version: "v1", Group: "apps"}, testNs, nil),
	}

	expectedActions := []kubetesting.Action{
		kubetesting.NewGetAction(schema.GroupVersionResource{Resource: "configmaps", Version: "v1"}, testNs, "0.0.1-anchor"),
		kubetesting.NewCreateAction(schema.GroupVersionResource{Resource: "configmaps", Version: "v1"}, testNs, nil),
		shippertesting.NewDiscoveryAction("services"),
		shippertesting.NewDiscoveryAction("deployments"),
	}

	for _, fakePair := range clientsPerCluster {
		shippertesting.ShallowCheckActions(expectedDynamicActions, fakePair.fakeDynamicClient.Actions(), t)
		shippertesting.ShallowCheckActions(expectedActions, fakePair.fakeClient.Actions(), t)
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
	it.Spec.CanOverride = false
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
		}, testNs, it),
	}
	shippertesting.CheckActions(expectedActions, filteredActions, t)
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
	chart := buildChart(appName, "0.0.1", repoUrl)
	installationTarget := buildInstallationTarget(testNs, appName, []string{cluster.Name}, &chart)

	clientsPerCluster, shipperclientset, fakeDynamicClientBuilder, shipperInformerFactory :=
		initializeClients(apiResourceList, []runtime.Object{cluster, installationTarget}, nil)

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
		}, testNs, it),
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
	chart := buildChart(appName, "0.0.1", repoUrl)
	installationTarget := buildInstallationTarget(testNs, appName, []string{cluster.Name}, &chart)

	clientsPerCluster, shipperclientset, fakeDynamicClientBuilder, shipperInformerFactory :=
		initializeClients([]*v1.APIResourceList{}, []runtime.Object{cluster, installationTarget}, objectsPerClusterMap{cluster.Name: nil})

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
		}, testNs, it),
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
	chart := buildChart(appName, "0.0.1", repoUrl)
	installationTarget := buildInstallationTarget(testNs, appName, []string{"minikube-a"}, &chart)

	clientsPerCluster, shipperclientset, fakeDynamicClientBuilder, shipperInformerFactory :=
		initializeClients(apiResourceList, []runtime.Object{installationTarget}, nil)

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
		}, testNs, it),
	}
	var filteredActions []kubetesting.Action
	for _, a := range shipperclientset.Actions() {
		if a.GetVerb() == "update" {
			filteredActions = append(filteredActions, a)
		}
	}

	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

// TestInstallNoOverride verifies that an InstallationTarget with disabled
// overrides does not get processed.
func TestInstallNoOverride(t *testing.T) {
	cluster := buildCluster("minikube-a")
	appName := "reviews-api"
	testNs := "test-namespace"
	chart := buildChart(appName, "0.0.1", repoUrl)

	it := buildInstallationTarget(testNs, appName, []string{cluster.Name}, &chart)
	it.Spec.CanOverride = false

	clientsPerCluster, shipperclientset, fakeDynamicClientBuilder, shipperInformerFactory :=
		initializeClients(apiResourceList, []runtime.Object{cluster, it},
			objectsPerClusterMap{cluster.Name: []runtime.Object{}})

	clusterPair := clientsPerCluster[cluster.Name]
	fakeClientProvider := &FakeClientProvider{
		clientsPerCluster: clientsPerCluster,
		restConfig:        &rest.Config{},
	}

	fakeRecorder := record.NewFakeRecorder(42)

	c := newController(shipperclientset, shipperInformerFactory,
		fakeClientProvider, fakeDynamicClientBuilder, fakeRecorder)

	if !c.processNextWorkItem() {
		t.Fatal("Could not process work item")
	}

	expectedActions := []kubetesting.Action{}
	shippertesting.ShallowCheckActions(expectedActions, clusterPair.fakeDynamicClient.Actions(), t)
	shippertesting.ShallowCheckActions(expectedActions, clusterPair.fakeClient.Actions(), t)
}

func TestInstallWithoutChart(t *testing.T) {
	cluster := buildCluster("minikube-a")
	appName := "reviews-api"
	testNs := "test-namespace"
	chart := buildChart(appName, "0.0.1", repoUrl)

	rel := buildRelease(testNs, appName, chart)

	it := buildInstallationTarget(testNs, appName, []string{cluster.Name}, nil)
	it.ObjectMeta.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: shipper.SchemeGroupVersion.String(),
			Kind:       "Release",
			Name:       rel.GetName(),
			UID:        rel.GetUID(),
		},
	}

	shipperObjects := []runtime.Object{cluster, rel, it}
	clientsPerCluster, shipperclientset, fakeDynamicClientBuilder, shipperInformerFactory :=
		initializeClients(apiResourceList, shipperObjects,
			objectsPerClusterMap{cluster.Name: []runtime.Object{}})

	fakeClientProvider := &FakeClientProvider{
		clientsPerCluster: clientsPerCluster,
		restConfig:        &rest.Config{},
	}

	fakeRecorder := record.NewFakeRecorder(42)

	c := newController(shipperclientset, shipperInformerFactory,
		fakeClientProvider, fakeDynamicClientBuilder, fakeRecorder)

	if !c.processNextWorkItem() {
		t.Fatal("Could not process work item")
	}

	var filteredActions []kubetesting.Action
	for _, a := range shipperclientset.Actions() {
		if a.GetVerb() == "update" {
			filteredActions = append(filteredActions, a)
		}
	}

	it = it.DeepCopy()
	it.Spec.CanOverride = false
	it.Spec.Chart = &chart
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

	expectedActions := []kubetesting.Action{
		kubetesting.NewUpdateAction(schema.GroupVersionResource{
			Resource: "installationtargets",
			Version:  shipper.SchemeGroupVersion.Version,
			Group:    shipper.SchemeGroupVersion.Group,
		}, testNs, it),
	}
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}
