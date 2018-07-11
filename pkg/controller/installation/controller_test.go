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

	shipperV1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	"github.com/bookingcom/shipper/pkg/conditions"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

func init() {
	conditions.InstallationConditionsShouldDiscardTimestamps = true
}

func TestInstallIncumbent(t *testing.T) {
	app := loadApplication()
	cluster := loadCluster("minikube-a")
	releaseA := loadRelease()
	releaseB := loadRelease()
	releaseB.Name = "0.0.2"
	installationTarget := loadInstallationTarget()
	app.Status.History = []string{"0.0.1", "0.0.2"}

	fakeClient, shipperclientset, fakeDynamicClient, fakeDynamicClientBuilder, shipperInformerFactory :=
		initializeClients(apiResourceList, []runtime.Object{app, cluster, installationTarget, releaseA, releaseB}, nil)

	fakeClientProvider := &FakeClientProvider{
		fakeClient: fakeClient,
		restConfig: &rest.Config{},
	}
	fakeRecorder := record.NewFakeRecorder(42)

	c := newController(
		shipperclientset, shipperInformerFactory, fakeClientProvider, fakeDynamicClientBuilder, fakeRecorder)

	if !c.processNextWorkItem() {
		t.Fatal("Could not process work item")
	}

	expectedActions := []kubetesting.Action{}
	shippertesting.CheckActions(expectedActions, fakeDynamicClient.Actions(), t)
}

// TestInstallOneCluster tests the installation process using the installation.Controller.
func TestInstallOneCluster(t *testing.T) {
	app := loadApplication()
	cluster := loadCluster("minikube-a")
	release := loadRelease()
	installationTarget := loadInstallationTarget()

	fakeClient, shipperclientset, fakeDynamicClient, fakeDynamicClientBuilder, shipperInformerFactory :=
		initializeClients(apiResourceList, []runtime.Object{app, cluster, release, installationTarget}, nil)

	fakeClientProvider := &FakeClientProvider{
		fakeClient: fakeClient,
		restConfig: &rest.Config{},
	}
	fakeRecorder := record.NewFakeRecorder(42)

	c := newController(
		shipperclientset, shipperInformerFactory, fakeClientProvider, fakeDynamicClientBuilder, fakeRecorder)

	if !c.processNextWorkItem() {
		t.Fatal("Could not process work item")
	}

	// The chart contained in the test release produces a service and a
	// deployment manifest. The events order should be always the same,
	// since we changed the renderer behavior to always return a
	// consistently ordered list of manifests, according to Kind and
	// Name. The actions described below are expected to be executed
	// against the dynamic client that is returned by the
	// fakeDynamicClientBuilder function passed to the controller, which
	// mimics a connection to a Target Cluster.
	expectedActions := []kubetesting.Action{
		kubetesting.NewCreateAction(
			schema.GroupVersionResource{Resource: "namespaces", Version: "v1"},
			release.GetNamespace(),
			nil),
		kubetesting.NewCreateAction(
			schema.GroupVersionResource{Resource: "services", Version: "v1"},
			release.GetNamespace(),
			nil),
		kubetesting.NewCreateAction(
			schema.GroupVersionResource{Resource: "deployments", Version: "v1", Group: "apps"},
			release.GetNamespace(),
			nil),
	}
	shippertesting.CheckActions(expectedActions, fakeDynamicClient.Actions(), t)

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
	it.Status.Clusters = []*shipperV1.ClusterInstallationStatus{
		{
			Name: "minikube-a", Status: shipperV1.InstallationStatusInstalled,
			Conditions: []shipperV1.ClusterInstallationCondition{
				{
					Type:   shipperV1.ClusterConditionTypeOperational,
					Status: corev1.ConditionTrue,
				},
				{
					Type:   shipperV1.ClusterConditionTypeReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
	expectedActions = []kubetesting.Action{
		kubetesting.NewUpdateAction(
			schema.GroupVersionResource{Resource: "installationtargets", Version: "v1", Group: "shipper.booking.com"},
			release.GetNamespace(),
			it),
	}
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

func TestInstallMultipleClusters(t *testing.T) {
	app := loadApplication()
	clusterA := loadCluster("minikube-a")
	clusterB := loadCluster("minikube-b")
	release := loadRelease()
	installationTarget := loadInstallationTarget()
	installationTarget.Spec.Clusters = []string{"minikube-a", "minikube-b"}

	fakeClient, shipperclientset, fakeDynamicClient, fakeDynamicClientBuilder, shipperInformerFactory :=
		initializeClients(apiResourceList, []runtime.Object{app, clusterA, clusterB, release, installationTarget}, nil)

	fakeClientProvider := &FakeClientProvider{
		fakeClient: fakeClient,
		restConfig: &rest.Config{},
	}
	fakeRecorder := record.NewFakeRecorder(42)

	c := newController(
		shipperclientset, shipperInformerFactory, fakeClientProvider, fakeDynamicClientBuilder, fakeRecorder)

	if !c.processNextWorkItem() {
		t.Fatal("Could not process work item")
	}

	// The chart contained in the test release produces a service and a
	// deployment manifest. The events order should be always the same,
	// since we changed the renderer behavior to always return a
	// consistently ordered list of manifests, according to Kind and
	// Name. The actions described below are expected to be executed
	// against the dynamic client that is returned by the
	// fakeDynamicClientBuilder function passed to the controller, which
	// mimics a connection to a Target Cluster.
	expectedActions := []kubetesting.Action{
		kubetesting.NewCreateAction(
			schema.GroupVersionResource{Resource: "namespaces", Version: "v1"},
			release.GetNamespace(),
			nil),
		kubetesting.NewCreateAction(
			schema.GroupVersionResource{Resource: "services", Version: "v1"},
			release.GetNamespace(),
			nil),
		kubetesting.NewCreateAction(
			schema.GroupVersionResource{Resource: "deployments", Version: "v1", Group: "apps"},
			release.GetNamespace(),
			nil),
		kubetesting.NewCreateAction(
			schema.GroupVersionResource{Resource: "namespaces", Version: "v1"},
			release.GetNamespace(),
			nil),
		kubetesting.NewCreateAction(
			schema.GroupVersionResource{Resource: "services", Version: "v1"},
			release.GetNamespace(),
			nil),
		kubetesting.NewCreateAction(
			schema.GroupVersionResource{Resource: "deployments", Version: "v1", Group: "apps"},
			release.GetNamespace(),
			nil),
	}
	shippertesting.CheckActions(expectedActions, fakeDynamicClient.Actions(), t)

	// We are interested only in "update" actions here.
	var filteredActions []kubetesting.Action
	for _, a := range shipperclientset.Actions() {
		if a.GetVerb() == "update" {
			filteredActions = append(filteredActions, a)
		}
	}

	// Now we need to check if the installation target process was properly
	// patched and the clusters are listed in alphabetical order.
	it := installationTarget.DeepCopy()
	it.Status.Clusters = []*shipperV1.ClusterInstallationStatus{
		{
			Name:   "minikube-a",
			Status: shipperV1.InstallationStatusInstalled,
			Conditions: []shipperV1.ClusterInstallationCondition{
				{
					Type:   shipperV1.ClusterConditionTypeOperational,
					Status: corev1.ConditionTrue,
				},
				{
					Type:   shipperV1.ClusterConditionTypeReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
		{
			Name:   "minikube-b",
			Status: shipperV1.InstallationStatusInstalled,
			Conditions: []shipperV1.ClusterInstallationCondition{
				{
					Type:   shipperV1.ClusterConditionTypeOperational,
					Status: corev1.ConditionTrue,
				},
				{
					Type:   shipperV1.ClusterConditionTypeReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
	expectedActions = []kubetesting.Action{
		kubetesting.NewUpdateAction(
			schema.GroupVersionResource{Resource: "installationtargets", Version: "v1", Group: "shipper.booking.com"},
			release.GetNamespace(),
			it),
	}
	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

// TestMissingRelease tests a case that the installation target object is
// being processed but the release it refers to doesn't exist in the
// management cluster anymore.
//
// This doesn't raise an error, but handles it to Kubernetes
// runtime.HandleError instead, so this test checks whether or not
// HandleError has been called and if it has been called with the
// message we expect.
func TestMissingRelease(t *testing.T) {
	var shipperclientset *shipperfake.Clientset

	cluster := loadCluster("minikube-a")
	installationTarget := loadInstallationTarget()

	fakeClient, shipperclientset, _, fakeDynamicClientBuilder, shipperInformerFactory :=
		initializeClients(apiResourceList, []runtime.Object{cluster, installationTarget}, nil)

	fakeClientProvider := &FakeClientProvider{
		fakeClient: fakeClient,
		restConfig: &rest.Config{},
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
// This doesn't raise an error, but it updates the installation target
// status, so this test checks whether the manifest has been updated
// with the desired status.
func TestClientError(t *testing.T) {
	var shipperclientset *shipperfake.Clientset

	app := loadApplication()
	cluster := loadCluster("minikube-a")
	installationTarget := loadInstallationTarget()
	release := loadRelease()

	fakeClient, shipperclientset, _, fakeDynamicClientBuilder, shipperInformerFactory :=
		initializeClients(apiResourceList, []runtime.Object{app, release, cluster, installationTarget}, nil)

	fakeClientProvider := &FakeClientProvider{
		fakeClient:          fakeClient,
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

	expectedHandleErrors := 0
	if handleErrors != expectedHandleErrors {
		t.Fatalf("expected %d handle errors, got %d instead", expectedHandleErrors, handleErrors)
	}

	it := installationTarget.DeepCopy()
	it.Status.Clusters = []*shipperV1.ClusterInstallationStatus{
		{
			Name:    "minikube-a",
			Status:  shipperV1.InstallationStatusFailed,
			Message: "client error",
			Conditions: []shipperV1.ClusterInstallationCondition{
				{
					Type:    shipperV1.ClusterConditionTypeOperational,
					Status:  corev1.ConditionFalse,
					Reason:  conditions.ServerError,
					Message: "client error",
				},
				{
					Type:    shipperV1.ClusterConditionTypeReady,
					Status:  corev1.ConditionUnknown,
					Reason:  conditions.ServerError,
					Message: "client error",
				},
			},
		},
	}
	expectedActions := []kubetesting.Action{
		kubetesting.NewUpdateAction(
			schema.GroupVersionResource{Resource: "installationtargets", Version: "v1", Group: "shipper.booking.com"},
			release.GetNamespace(),
			it),
	}
	var filteredActions []kubetesting.Action
	for _, a := range shipperclientset.Actions() {
		if a.GetVerb() == "update" {
			filteredActions = append(filteredActions, a)
		}
	}

	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

// TestTargetClusterMissesGVK tests a case where the rendered manifest refers
// to a GroupVersionKind that the Target Cluster doesn't understand.
//
// This doesn't raise an error, but it updates the installation target
// status, so this test checks whether the manifest has been updated
// with the desired status.
func TestTargetClusterMissesGVK(t *testing.T) {
	var shipperclientset *shipperfake.Clientset

	app := loadApplication()
	cluster := loadCluster("minikube-a")
	installationTarget := loadInstallationTarget()
	release := loadRelease()

	fakeClient, shipperclientset, _, fakeDynamicClientBuilder, shipperInformerFactory :=
		initializeClients([]*v1.APIResourceList{}, []runtime.Object{app, release, cluster, installationTarget}, nil)

	fakeClientProvider := &FakeClientProvider{
		fakeClient: fakeClient,
		restConfig: &rest.Config{},
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

	expectedHandleErrors := 0
	if handleErrors != expectedHandleErrors {
		t.Fatalf("expected %d handle errors, got %d instead", expectedHandleErrors, handleErrors)
	}

	it := installationTarget.DeepCopy()
	it.Status.Clusters = []*shipperV1.ClusterInstallationStatus{
		{
			Name:    "minikube-a",
			Status:  shipperV1.InstallationStatusFailed,
			Message: `error building resource client: GroupVersion "v1" not found`,
			Conditions: []shipperV1.ClusterInstallationCondition{
				{
					Type:   shipperV1.ClusterConditionTypeOperational,
					Status: corev1.ConditionTrue,
				},
				{
					Type:    shipperV1.ClusterConditionTypeReady,
					Status:  corev1.ConditionFalse,
					Reason:  conditions.ServerError,
					Message: `error building resource client: GroupVersion "v1" not found`,
				},
			},
		},
	}
	expectedActions := []kubetesting.Action{
		kubetesting.NewUpdateAction(
			schema.GroupVersionResource{Resource: "installationtargets", Version: "v1", Group: "shipper.booking.com"},
			release.GetNamespace(),
			it),
	}
	var filteredActions []kubetesting.Action
	for _, a := range shipperclientset.Actions() {
		if a.GetVerb() == "update" {
			filteredActions = append(filteredActions, a)
		}
	}

	shippertesting.CheckActions(expectedActions, filteredActions, t)
}

// TestManagementServerMissesCluster tests a case where the installation
// target refers to a cluster the management cluster doesn't know.
//
// This doesn't raise an error, but it updates the installation target
// status, so this test checks whether the manifest has been updated
// with the desired status.
func TestManagementServerMissesCluster(t *testing.T) {
	var shipperclientset *shipperfake.Clientset

	app := loadApplication()
	installationTarget := loadInstallationTarget()
	release := loadRelease()

	fakeClient, shipperclientset, _, fakeDynamicClientBuilder, shipperInformerFactory :=
		initializeClients(apiResourceList, []runtime.Object{app, release, installationTarget}, nil)

	fakeClientProvider := &FakeClientProvider{
		fakeClient: fakeClient,
		restConfig: &rest.Config{},
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

	expectedHandleErrors := 0
	if handleErrors != expectedHandleErrors {
		t.Fatalf("expected %d handle errors, got %d instead", expectedHandleErrors, handleErrors)
	}

	it := installationTarget.DeepCopy()
	it.Status.Clusters = []*shipperV1.ClusterInstallationStatus{
		{
			Name:    "minikube-a",
			Status:  shipperV1.InstallationStatusFailed,
			Message: `cluster.shipper.booking.com "minikube-a" not found`,
			Conditions: []shipperV1.ClusterInstallationCondition{
				{
					Type:    shipperV1.ClusterConditionTypeOperational,
					Status:  corev1.ConditionFalse,
					Reason:  conditions.ServerError,
					Message: `cluster.shipper.booking.com "minikube-a" not found`,
				},
				{
					Type:    shipperV1.ClusterConditionTypeReady,
					Status:  corev1.ConditionUnknown,
					Reason:  conditions.ServerError,
					Message: `cluster.shipper.booking.com "minikube-a" not found`,
				},
			},
		},
	}
	expectedActions := []kubetesting.Action{
		kubetesting.NewUpdateAction(
			schema.GroupVersionResource{Resource: "installationtargets", Version: "v1", Group: "shipper.booking.com"},
			release.GetNamespace(),
			it),
	}
	var filteredActions []kubetesting.Action
	for _, a := range shipperclientset.Actions() {
		if a.GetVerb() == "update" {
			filteredActions = append(filteredActions, a)
		}
	}

	shippertesting.CheckActions(expectedActions, filteredActions, t)
}
