package installation

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"path"
	"reflect"
	"regexp"
	"testing"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/proto/hapi/chart"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/controller/janitor"
	shippererrors "github.com/bookingcom/shipper/pkg/errors"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

var localFetchChart = func(chartspec *shipper.Chart) (*chart.Chart, error) {
	re := regexp.MustCompile(`[^a-zA-Z0-9]+`)
	pathurl := re.ReplaceAllString(chartspec.RepoURL, "_")
	data, err := ioutil.ReadFile(
		path.Join(
			"testdata",
			"chart-cache",
			pathurl,
			fmt.Sprintf("%s-%s.tgz", chartspec.Name, chartspec.Version),
		))
	if err != nil {
		return nil, err
	}
	buf := bytes.NewBuffer(data)
	return chartutil.LoadArchive(buf)
}

// apiResourceList contains a list of APIResources containing some of v1 and
// extensions/v1beta1 resources from Kubernetes, since fake clients by default
// don't contain any reference which resources it can handle.
var apiResourceList = []*metav1.APIResourceList{
	{
		GroupVersion: "v1",
		APIResources: []metav1.APIResource{
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
		APIResources: []metav1.APIResource{

			{
				Kind:       "Deployment",
				Namespaced: true,
				Name:       "deployments",
			},
		},
	},
	{
		GroupVersion: "apps/v1",
		APIResources: []metav1.APIResource{

			{
				Kind:       "Deployment",
				Namespaced: true,
				Name:       "deployments",
			},
		},
	},
}

// TestInstaller tests the installation process using a Installer directly.
func TestInstaller(t *testing.T) {
	// First install.
	ImplTestInstaller(t, nil, nil)

	// With existing remote service.
	notOwnedService := loadService("no-owners")
	ImplTestInstaller(t, nil, []runtime.Object{notOwnedService})

	// With existing remote service.
	ownedService := loadService("existing-owners")
	ImplTestInstaller(t, nil, []runtime.Object{ownedService})
}

func ImplTestInstaller(t *testing.T, shipperObjects []runtime.Object, kubeObjects []runtime.Object) {
	cluster := buildCluster("minikube-a")
	appName := "reviews-api"
	testNs := "test-namespace"
	chart := buildChart(appName, "0.0.1", repoUrl)
	it := buildInstallationTarget(testNs, appName, []string{cluster.Name}, &chart)
	configMapAnchor, err := janitor.CreateConfigMapAnchor(it)
	if err != nil {
		panic(err)
	}
	installer := newInstaller(it)
	svc := loadService("baseline")
	svc.SetOwnerReferences(append(svc.GetOwnerReferences(), janitor.ConfigMapAnchorToOwnerReference(configMapAnchor)))

	clientsPerCluster, _, fakeDynamicClientBuilder, _ := initializeClients(apiResourceList, shipperObjects, objectsPerClusterMap{cluster.Name: kubeObjects})

	fakePair := clientsPerCluster[cluster.Name]

	restConfig := &rest.Config{}

	expectedActions := []kubetesting.Action{
		kubetesting.NewGetAction(schema.GroupVersionResource{Resource: "configmaps", Version: "v1"}, testNs, "reviews-api-anchor"),
		kubetesting.NewCreateAction(schema.GroupVersionResource{Resource: "configmaps", Version: "v1"}, testNs, nil),
		shippertesting.NewDiscoveryAction("services"),
		shippertesting.NewDiscoveryAction("deployments"),
	}

	expectedDynamicActions := []kubetesting.Action{
		kubetesting.NewGetAction(schema.GroupVersionResource{Resource: "services", Version: "v1"}, testNs, "0.0.1-reviews-api"),
		kubetesting.NewCreateAction(schema.GroupVersionResource{Resource: "services", Version: "v1"}, testNs, nil),
		kubetesting.NewGetAction(schema.GroupVersionResource{Resource: "deployments", Version: "v1", Group: "apps"}, testNs, "test-namespace-reviews-api"),
		kubetesting.NewCreateAction(schema.GroupVersionResource{Resource: "deployments", Version: "v1", Group: "apps"}, testNs, nil),
	}

	if err := installer.install(cluster, fakePair.fakeClient, restConfig, fakeDynamicClientBuilder); err != nil {
		t.Fatal(err)
	}

	shippertesting.ShallowCheckActions(expectedActions, fakePair.fakeClient.Actions(), t)
	shippertesting.ShallowCheckActions(expectedDynamicActions, fakePair.fakeDynamicClient.Actions(), t)

	filteredActions := filterActions(fakePair.fakeClient.Actions(), "create")
	filteredActions = append(filteredActions, filterActions(fakePair.fakeDynamicClient.Actions(), "create")...)

	validateAction(t, filteredActions[0], "ConfigMap")
	validateServiceCreateAction(t, svc, validateAction(t, filteredActions[1], "Service"))
	validateDeploymentCreateAction(t, validateAction(t, filteredActions[2], "Deployment"), map[string]string{"app": "reviews-api"})
}

func extractUnstructuredContent(scheme *runtime.Scheme, obj runtime.Object) (*unstructured.Unstructured, map[string]interface{}) {
	u := &unstructured.Unstructured{}
	err := scheme.Convert(obj, u, nil)
	if err != nil {
		panic(err)
	}
	uContent := u.UnstructuredContent()
	contentCopy := map[string]interface{}{}
	for k, v := range uContent {
		contentCopy[k] = v
	}
	return u, contentCopy
}

func validateAction(t *testing.T, a kubetesting.Action, k string) runtime.Object {
	ca := a.(kubetesting.CreateAction)
	caObj := ca.GetObject()
	if caObj.GetObjectKind().GroupVersionKind().Kind == k {
		return caObj
	}
	t.Logf("%+v", caObj)
	t.Fatalf("object is not a %q", k)
	return nil
}

func validateServiceCreateAction(t *testing.T, existingService *corev1.Service, obj runtime.Object) {
	scheme := kubescheme.Scheme

	unstructuredObj, unstructuredContent := extractUnstructuredContent(scheme, obj)

	// First we test the data that is expected to be in the created service
	// object, since we delete keys on the underlying unstructured object
	// later on, when comparing spec and metadata.
	if _, ok := unstructuredObj.GetLabels()[shipper.InstallationTargetOwnerLabel]; !ok {
		t.Fatalf("could not find %q in Service .metadata.labels", shipper.InstallationTargetOwnerLabel)
	}

	_, expectedUnstructuredServiceContent := extractUnstructuredContent(scheme, existingService)

	uMetadata := unstructuredContent["metadata"].(map[string]interface{})
	sMetadata := expectedUnstructuredServiceContent["metadata"].(map[string]interface{})

	if !reflect.DeepEqual(uMetadata, sMetadata) {
		t.Fatalf("metadata mismatch in Service (-want +got): %s", cmp.Diff(sMetadata, uMetadata))
	}

	uSpec := unstructuredContent["spec"].(map[string]interface{})
	sSpec := expectedUnstructuredServiceContent["spec"].(map[string]interface{})
	if !reflect.DeepEqual(uSpec, sSpec) {
		t.Fatalf("spec mismatch in Service (-want +got): %s", cmp.Diff(sSpec, uSpec))
	}
}

func validateDeploymentCreateAction(t *testing.T, obj runtime.Object, validateLabels map[string]string) {
	scheme := kubescheme.Scheme

	u := &unstructured.Unstructured{}
	err := scheme.Convert(obj, u, nil)
	if err != nil {
		panic(err)
	}
	if _, ok := u.GetLabels()[shipper.InstallationTargetOwnerLabel]; !ok {
		t.Fatalf("could not find %q in Deployment .metadata.labels", shipper.InstallationTargetOwnerLabel)
	}

	deployment := &appsv1.Deployment{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, deployment); err != nil {
		t.Fatalf("could not decode deployment from unstructured: %s", err)
	}

	// If provided, we ensure the labels from the list present in the
	// produced deployment object.
	for labelName, labelValue := range validateLabels {
		actualChartLabelValue, ok := deployment.Spec.Template.Labels[labelName]
		if !ok {
			t.Fatalf("deployment .spec.template.labels doesn't contain a label (%q) which was present in the chart", labelName)
		}

		if actualChartLabelValue != labelValue {
			t.Fatalf(
				"deployment .spec.template.labels has the right previously-existing label (%q) but wrong value. Expected %q but got %q",
				labelName, labelValue, actualChartLabelValue,
			)
		}
	}

}

func filterActions(actions []kubetesting.Action, verb string) []kubetesting.Action {
	var filteredActions []kubetesting.Action
	for _, a := range actions {
		if a.GetVerb() == verb {
			filteredActions = append(filteredActions, a)
		}
	}
	return filteredActions
}

// TestInstallerBrokenChartTarball tests if the installation process fails when the
// release contains an invalid serialized chart.
func TestInstallerBrokenChartTarball(t *testing.T) {
	cluster := buildCluster("minikube-a")
	appName := "reviews-api"
	testNs := "reviews-api"

	// there is a reviews-api-invalid-tarball.tgz in testdata which
	// contains a broken tarball
	chart := buildChart(appName, "invalid-tarball", repoUrl)

	it := buildInstallationTarget(testNs, appName, []string{cluster.Name}, &chart)
	installer := newInstaller(it)

	clientsPerCluster, _, fakeDynamicClientBuilder, _ := initializeClients(apiResourceList, nil, objectsPerClusterMap{cluster.Name: []runtime.Object{}})

	fakePair := clientsPerCluster[cluster.Name]

	restConfig := &rest.Config{}
	if err := installer.install(cluster, fakePair.fakeClient, restConfig, fakeDynamicClientBuilder); err == nil {
		t.Fatal("installRelease should fail, invalid tarball")
	}
}

// TestInstallerBrokenChartTarball tests if the installation process fails when the
// release contains an invalid serialized chart.
func TestInstallerChartTarballBrokenService(t *testing.T) {
	cluster := buildCluster("minikube-a")
	appName := "reviews-api"
	testNs := "reviews-api"

	// there is a reviews-api-0.0.1-broken-service.tgz in testdata which
	// contains invalid deployment and service templates
	chart := buildChart(appName, "0.0.1-broken-service", repoUrl)

	it := buildInstallationTarget(testNs, appName, []string{cluster.Name}, &chart)
	installer := newInstaller(it)

	clientsPerCluster, _, fakeDynamicClientBuilder, _ := initializeClients(apiResourceList, nil, objectsPerClusterMap{cluster.Name: []runtime.Object{}})

	fakePair := clientsPerCluster[cluster.Name]

	restConfig := &rest.Config{}
	if err := installer.install(cluster, fakePair.fakeClient, restConfig, fakeDynamicClientBuilder); err == nil {
		t.Fatal("installRelease should fail, invalid tarball")
	}
}

// TestInstallerChartTarballInvalidDeploymentName tests if the installation
// process fails when the release contains a deployment that doesn't have a
// name templated with the release's name.
func TestInstallerChartTarballInvalidDeploymentName(t *testing.T) {
	cluster := buildCluster("minikube-a")
	appName := "reviews-api"
	testNs := "reviews-api"

	// there is a reviews-api-invalid-deployment-name.tgz in testdata which
	// contains invalid deployment and service templates
	chart := buildChart(appName, "invalid-deployment-name", repoUrl)

	it := buildInstallationTarget(testNs, appName, []string{cluster.Name}, &chart)
	installer := newInstaller(it)

	clientsPerCluster, _, fakeDynamicClientBuilder, _ := initializeClients(apiResourceList, nil, objectsPerClusterMap{cluster.Name: []runtime.Object{}})

	fakePair := clientsPerCluster[cluster.Name]

	restConfig := &rest.Config{}

	err := installer.install(cluster, fakePair.fakeClient, restConfig, fakeDynamicClientBuilder)
	if err == nil {
		t.Fatal("installRelease should fail, invalid deployment name")
	}

	if _, ok := err.(shippererrors.InvalidChartError); !ok {
		t.Fatalf("installRelease should fail with InvalidChartError, got %v instead", err)
	}
}

// TestInstallerBrokenChartContents tests if the installation process fails when the
// release contains a valid chart tarball with invalid K8s object templates.
func TestInstallerBrokenChartContents(t *testing.T) {
	cluster := buildCluster("minikube-a")
	appName := "reviews-api"
	testNs := "reviews-api"

	// There is a reviews-api-invalid-k8s-objects.tgz in testdata which contains
	// invalid deployment and service templates.
	chart := buildChart(appName, "invalid-k8s-objects", repoUrl)

	it := buildInstallationTarget(testNs, appName, []string{cluster.Name}, &chart)
	installer := newInstaller(it)

	clientsPerCluster, _, fakeDynamicClientBuilder, _ := initializeClients(apiResourceList, nil, objectsPerClusterMap{cluster.Name: nil})

	fakePair := clientsPerCluster[cluster.Name]

	restConfig := &rest.Config{}
	if err := installer.install(cluster, fakePair.fakeClient, restConfig, fakeDynamicClientBuilder); err == nil {
		t.Fatal("installRelease should fail, invalid k8s objects")
	}
}

func TestInstallerSingleServiceNoLB(t *testing.T) {
	cluster := buildCluster("minikube-a")
	appName := "reviews-api"
	testNs := "reviews-api"

	chart := buildChart(appName, "single-service-no-lb", repoUrl)

	it := buildInstallationTarget(testNs, appName, []string{cluster.Name}, &chart)
	configMapAnchor, err := janitor.CreateConfigMapAnchor(it)
	if err != nil {
		panic(err)
	}
	installer := newInstaller(it)
	svc := loadService("baseline")
	svc.SetOwnerReferences(append(svc.GetOwnerReferences(), janitor.ConfigMapAnchorToOwnerReference(configMapAnchor)))

	clientsPerCluster, _, fakeDynamicClientBuilder, _ := initializeClients(apiResourceList, nil, objectsPerClusterMap{cluster.Name: nil})

	fakePair := clientsPerCluster[cluster.Name]

	restConfig := &rest.Config{}

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

	if err := installer.install(cluster, fakePair.fakeClient, restConfig, fakeDynamicClientBuilder); err != nil {
		t.Fatal(err)
	}

	shippertesting.ShallowCheckActions(expectedDynamicActions, fakePair.fakeDynamicClient.Actions(), t)
	shippertesting.ShallowCheckActions(expectedActions, fakePair.fakeClient.Actions(), t)

	filteredActions := filterActions(fakePair.fakeClient.Actions(), "create")
	filteredActions = append(filteredActions, filterActions(fakePair.fakeDynamicClient.Actions(), "create")...)

	validateAction(t, filteredActions[0], "ConfigMap")
	validateServiceCreateAction(t, svc, validateAction(t, filteredActions[1], "Service"))
	validateDeploymentCreateAction(t, validateAction(t, filteredActions[2], "Deployment"), map[string]string{"app": "reviews-api"})
}

func TestInstallerSingleServiceWithLB(t *testing.T) {
	cluster := buildCluster("minikube-a")
	appName := "reviews-api"
	testNs := "reviews-api"

	chart := buildChart(appName, "single-service-with-lb", repoUrl)

	it := buildInstallationTarget(testNs, appName, []string{cluster.Name}, &chart)
	configMapAnchor, err := janitor.CreateConfigMapAnchor(it)
	if err != nil {
		panic(err)
	}
	installer := newInstaller(it)
	svc := loadService("baseline")
	svc.SetOwnerReferences(append(svc.GetOwnerReferences(), janitor.ConfigMapAnchorToOwnerReference(configMapAnchor)))

	clientsPerCluster, _, fakeDynamicClientBuilder, _ := initializeClients(apiResourceList, nil, objectsPerClusterMap{cluster.Name: nil})

	fakePair := clientsPerCluster[cluster.Name]

	restConfig := &rest.Config{}

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

	if err := installer.install(cluster, fakePair.fakeClient, restConfig, fakeDynamicClientBuilder); err != nil {
		t.Fatal(err)
	}

	shippertesting.ShallowCheckActions(expectedDynamicActions, fakePair.fakeDynamicClient.Actions(), t)
	shippertesting.ShallowCheckActions(expectedActions, fakePair.fakeClient.Actions(), t)

	filteredActions := filterActions(fakePair.fakeClient.Actions(), "create")
	filteredActions = append(filteredActions, filterActions(fakePair.fakeDynamicClient.Actions(), "create")...)

	validateAction(t, filteredActions[0], "ConfigMap")
	validateServiceCreateAction(t, svc, validateAction(t, filteredActions[1], "Service"))
	validateDeploymentCreateAction(t, validateAction(t, filteredActions[2], "Deployment"), map[string]string{"app": "reviews-api"})
}

func TestInstallerMultiServiceNoLB(t *testing.T) {
	cluster := buildCluster("minikube-a")
	appName := "reviews-api"
	testNs := "reviews-api"

	chart := buildChart(appName, "multi-service-no-lb", repoUrl)

	it := buildInstallationTarget(testNs, appName, []string{cluster.Name}, &chart)
	configMapAnchor, err := janitor.CreateConfigMapAnchor(it)
	if err != nil {
		panic(err)
	}
	installer := newInstaller(it)
	svc := loadService("baseline")
	svc.SetOwnerReferences(append(svc.GetOwnerReferences(), janitor.ConfigMapAnchorToOwnerReference(configMapAnchor)))

	clientsPerCluster, _, fakeDynamicClientBuilder, _ := initializeClients(apiResourceList, nil, objectsPerClusterMap{cluster.Name: nil})

	fakePair := clientsPerCluster[cluster.Name]

	restConfig := &rest.Config{}

	err = installer.install(cluster, fakePair.fakeClient, restConfig, fakeDynamicClientBuilder)
	if err == nil {
		t.Fatal("Expected an error, none raised")
	}
	if matched, err := regexp.MatchString("one and only one .* object .* is required", err.Error()); err != nil {
		t.Fatalf("Failed to test error against the regex: %s", err)
	} else if !matched {
		t.Fatalf("Unexpected error raised: %s", err)
	}
}

func TestInstallerMultiServiceWithLB(t *testing.T) {
	cluster := buildCluster("minikube-a")
	appName := "reviews-api"
	testNs := "reviews-api"

	chart := buildChart(appName, "multi-service-with-lb", repoUrl)

	it := buildInstallationTarget(testNs, appName, []string{cluster.Name}, &chart)
	configMapAnchor, err := janitor.CreateConfigMapAnchor(it)
	if err != nil {
		panic(err)
	}
	installer := newInstaller(it)
	svc := loadService("baseline")
	svc.SetOwnerReferences(append(svc.GetOwnerReferences(), janitor.ConfigMapAnchorToOwnerReference(configMapAnchor)))

	clientsPerCluster, _, fakeDynamicClientBuilder, _ := initializeClients(apiResourceList, nil, objectsPerClusterMap{cluster.Name: nil})

	fakePair := clientsPerCluster[cluster.Name]

	restConfig := &rest.Config{}

	expectedDynamicActions := []kubetesting.Action{
		kubetesting.NewGetAction(schema.GroupVersionResource{Resource: "services", Version: "v1"}, testNs, "0.0.1-reviews-api"),
		kubetesting.NewCreateAction(schema.GroupVersionResource{Resource: "services", Version: "v1"}, testNs, nil),
		kubetesting.NewGetAction(schema.GroupVersionResource{Resource: "services", Version: "v1"}, testNs, "0.0.1-reviews-api-staging"),
		kubetesting.NewCreateAction(schema.GroupVersionResource{Resource: "services", Version: "v1"}, testNs, nil),
		kubetesting.NewGetAction(schema.GroupVersionResource{Resource: "deployments", Version: "v1", Group: "apps"}, testNs, "0.0.1-reviews-api"),
		kubetesting.NewCreateAction(schema.GroupVersionResource{Resource: "deployments", Version: "v1", Group: "apps"}, testNs, nil),
	}

	expectedActions := []kubetesting.Action{
		kubetesting.NewGetAction(schema.GroupVersionResource{Resource: "configmaps", Version: "v1"}, testNs, "0.0.1-anchor"),
		kubetesting.NewCreateAction(schema.GroupVersionResource{Resource: "configmaps", Version: "v1"}, testNs, nil),
		shippertesting.NewDiscoveryAction("services"),
		shippertesting.NewDiscoveryAction("services"),
		shippertesting.NewDiscoveryAction("deployments"),
	}

	if err := installer.install(cluster, fakePair.fakeClient, restConfig, fakeDynamicClientBuilder); err != nil {
		t.Fatal(err)
	}

	shippertesting.ShallowCheckActions(expectedDynamicActions, fakePair.fakeDynamicClient.Actions(), t)
	shippertesting.ShallowCheckActions(expectedActions, fakePair.fakeClient.Actions(), t)

	filteredActions := filterActions(fakePair.fakeClient.Actions(), "create")
	filteredActions = append(filteredActions, filterActions(fakePair.fakeDynamicClient.Actions(), "create")...)

	validateAction(t, filteredActions[0], "ConfigMap")
	validateServiceCreateAction(t, svc, validateAction(t, filteredActions[1], "Service"))
	validateDeploymentCreateAction(t, validateAction(t, filteredActions[3], "Deployment"), map[string]string{"app": "reviews-api"})
}

func TestInstallerMultiServiceWithLBOffTheShelf(t *testing.T) {
	cluster := buildCluster("minikube-a")
	appName := "nginx"
	testNs := "nginx"

	chart := buildChart(appName, "0.1.0", repoUrl)

	it := buildInstallationTarget("nginx", "nginx", []string{cluster.Name}, &chart)

	configMapAnchor, err := janitor.CreateConfigMapAnchor(it)
	if err != nil {
		panic(err)
	}
	installer := newInstaller(it)
	primarySvc := loadService("nginx-primary")
	secondarySvc := loadService("nginx-secondary")
	primarySvc.SetOwnerReferences(append(primarySvc.GetOwnerReferences(), janitor.ConfigMapAnchorToOwnerReference(configMapAnchor)))
	secondarySvc.SetOwnerReferences(append(secondarySvc.GetOwnerReferences(), janitor.ConfigMapAnchorToOwnerReference(configMapAnchor)))

	clientsPerCluster, _, fakeDynamicClientBuilder, _ := initializeClients(apiResourceList, nil, objectsPerClusterMap{cluster.Name: nil})

	fakePair := clientsPerCluster[cluster.Name]

	restConfig := &rest.Config{}

	expectedDynamicActions := []kubetesting.Action{
		kubetesting.NewGetAction(schema.GroupVersionResource{Resource: "services", Version: "v1"}, testNs, "0.1.0-nginx"),
		kubetesting.NewCreateAction(schema.GroupVersionResource{Resource: "services", Version: "v1"}, testNs, nil),
		kubetesting.NewGetAction(schema.GroupVersionResource{Resource: "services", Version: "v1"}, testNs, "0.1.0-nginx-staging"),
		kubetesting.NewCreateAction(schema.GroupVersionResource{Resource: "services", Version: "v1"}, testNs, nil),
		kubetesting.NewGetAction(schema.GroupVersionResource{Resource: "deployments", Version: "v1", Group: "apps"}, testNs, "0.1.0-nginx"),
		kubetesting.NewCreateAction(schema.GroupVersionResource{Resource: "deployments", Version: "v1", Group: "apps"}, testNs, nil),
	}

	expectedActions := []kubetesting.Action{
		kubetesting.NewGetAction(schema.GroupVersionResource{Resource: "configmaps", Version: "v1"}, testNs, "0.1.0-anchor"),
		kubetesting.NewCreateAction(schema.GroupVersionResource{Resource: "configmaps", Version: "v1"}, testNs, nil),
		shippertesting.NewDiscoveryAction("services"),
		shippertesting.NewDiscoveryAction("services"),
		shippertesting.NewDiscoveryAction("deployments"),
	}

	if err := installer.install(cluster, fakePair.fakeClient, restConfig, fakeDynamicClientBuilder); err != nil {
		t.Fatal(err)
	}

	shippertesting.ShallowCheckActions(expectedActions, fakePair.fakeClient.Actions(), t)
	shippertesting.ShallowCheckActions(expectedDynamicActions, fakePair.fakeDynamicClient.Actions(), t)

	filteredActions := filterActions(fakePair.fakeClient.Actions(), "create")
	filteredActions = append(filteredActions, filterActions(fakePair.fakeDynamicClient.Actions(), "create")...)
	validateAction(t, filteredActions[0], "ConfigMap")
	validateServiceCreateAction(t, primarySvc, validateAction(t, filteredActions[1], "Service"))
	validateServiceCreateAction(t, secondarySvc, validateAction(t, filteredActions[2], "Service"))
	validateDeploymentCreateAction(t, validateAction(t, filteredActions[3], "Deployment"), map[string]string{})
}

func TestInstallerServiceWithReleaseNoWorkaround(t *testing.T) {
	cluster := buildCluster("minikube-a")
	appName := "reviews-api"
	testNs := "reviews-api"

	chart := buildChart(appName, "0.0.1", repoUrl)
	it := buildInstallationTarget(testNs, appName, []string{cluster.Name}, &chart)

	// Disabling the helm workaround
	delete(it.ObjectMeta.Labels, shipper.HelmWorkaroundLabel)

	configMapAnchor, err := janitor.CreateConfigMapAnchor(it)
	if err != nil {
		panic(err)
	}

	installer := newInstaller(it)
	svc := loadService("baseline")
	svc.SetOwnerReferences(append(svc.GetOwnerReferences(), janitor.ConfigMapAnchorToOwnerReference(configMapAnchor)))

	clientsPerCluster, _, fakeDynamicClientBuilder, _ := initializeClients(apiResourceList, nil, objectsPerClusterMap{cluster.Name: nil})

	fakePair := clientsPerCluster[cluster.Name]

	restConfig := &rest.Config{}

	err = installer.install(cluster, fakePair.fakeClient, restConfig, fakeDynamicClientBuilder)
	if err == nil {
		t.Fatal("Expected error, none raised")
	}
	if matched, regexErr := regexp.MatchString("This will break shipper traffic shifting logic", err.Error()); regexErr != nil {
		t.Fatalf("Failed to match the error message against the regex: %s", regexErr)
	} else if !matched {
		t.Fatalf("Unexpected error: %s", err)
	}
}

// TestInstallerNoOverride verifies that an InstallationTarget with disabled
// overrides does not try to update existing resources that it does not own.
func TestInstallerNoOverride(t *testing.T) {
	cluster := buildCluster("minikube-a")
	appName := "reviews-api"
	testNs := "reviews-api"
	ownerLabel := "some-other-installation-target"

	chart := buildChart(appName, "0.0.1", repoUrl)
	it := buildInstallationTarget(testNs, appName, []string{cluster.Name}, &chart)
	it.Spec.CanOverride = false

	svc := loadService("baseline")
	svc.ObjectMeta.Namespace = testNs
	svc.ObjectMeta.SetLabels(map[string]string{
		shipper.InstallationTargetOwnerLabel: ownerLabel})

	deployment := loadDeployment("baseline")
	deployment.ObjectMeta.Namespace = testNs
	deployment.ObjectMeta.SetLabels(map[string]string{
		shipper.InstallationTargetOwnerLabel: ownerLabel})

	kubeObjects := []runtime.Object{svc, deployment}

	clientsPerCluster, _, fakeDynamicClientBuilder, _ := initializeClients(
		apiResourceList, nil,
		objectsPerClusterMap{cluster.Name: kubeObjects})

	expectedActions := []kubetesting.Action{
		kubetesting.NewGetAction(schema.GroupVersionResource{Resource: "configmaps", Version: "v1"}, testNs, "reviews-api-anchor"),
		kubetesting.NewCreateAction(schema.GroupVersionResource{Resource: "configmaps", Version: "v1"}, testNs, nil),
		shippertesting.NewDiscoveryAction("services"),
		shippertesting.NewDiscoveryAction("deployments"),
	}

	expectedDynamicActions := []kubetesting.Action{
		kubetesting.NewGetAction(schema.GroupVersionResource{Resource: "services", Version: "v1"}, testNs, "0.0.1-reviews-api"),
		kubetesting.NewGetAction(schema.GroupVersionResource{Resource: "deployments", Version: "v1", Group: "apps"}, testNs, "test-namespace-reviews-api"),
	}

	installer := newInstaller(it)
	fakePair := clientsPerCluster[cluster.Name]
	restConfig := &rest.Config{}
	if err := installer.install(cluster, fakePair.fakeClient, restConfig, fakeDynamicClientBuilder); err != nil {
		t.Fatal(err)
	}

	shippertesting.ShallowCheckActions(expectedActions, fakePair.fakeClient.Actions(), t)
	shippertesting.ShallowCheckActions(expectedDynamicActions, fakePair.fakeDynamicClient.Actions(), t)
}
