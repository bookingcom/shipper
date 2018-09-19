package installation

import (
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/diff"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	kubetesting "k8s.io/client-go/testing"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/controller/janitor"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
)

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
	release := buildRelease("0.0.1", "reviews-api", "0", "deadbeef", "reviews-api")
	it := buildInstallationTarget(release, "reviews-api", "reviews-api", []string{cluster.Name})
	configMapAnchor, err := janitor.CreateConfigMapAnchor(it)
	if err != nil {
		panic(err)
	}
	installer := newInstaller(release, it)
	svc := loadService("baseline")
	svc.SetOwnerReferences(append(svc.GetOwnerReferences(), janitor.ConfigMapAnchorToOwnerReference(configMapAnchor)))

	clientsPerCluster, _, fakeDynamicClientBuilder, _ := initializeClients(apiResourceList, shipperObjects, objectsPerClusterMap{cluster.Name: kubeObjects})

	fakePair := clientsPerCluster[cluster.Name]

	restConfig := &rest.Config{}

	expectedActions := []kubetesting.Action{
		kubetesting.NewGetAction(schema.GroupVersionResource{Resource: "configmaps", Version: "v1"}, release.GetNamespace(), "0.0.1-anchor"),
		kubetesting.NewCreateAction(schema.GroupVersionResource{Resource: "configmaps", Version: "v1"}, release.GetNamespace(), nil),
		kubetesting.NewGetAction(schema.GroupVersionResource{Resource: "services", Version: "v1"}, release.GetNamespace(), "0.0.1-reviews-api"),
		kubetesting.NewCreateAction(schema.GroupVersionResource{Resource: "services", Version: "v1"}, release.GetNamespace(), nil),
		kubetesting.NewGetAction(schema.GroupVersionResource{Resource: "deployments", Version: "v1", Group: "apps"}, release.GetNamespace(), "0.0.1-reviews-api"),
		kubetesting.NewCreateAction(schema.GroupVersionResource{Resource: "deployments", Version: "v1", Group: "apps"}, release.GetNamespace(), nil),
	}

	if err := installer.installRelease(cluster, fakePair.fakeClient, restConfig, fakeDynamicClientBuilder); err != nil {
		t.Fatal(err)
	}

	shippertesting.ShallowCheckActions(expectedActions, fakePair.fakeDynamicClient.Actions(), t)

	filteredActions := filterActions(fakePair.fakeDynamicClient.Actions(), "create")
	validateAction(t, filteredActions[0], "ConfigMap")
	validateServiceCreateAction(t, svc, validateAction(t, filteredActions[1], "Service"))
	validateDeploymentCreateAction(t, validateAction(t, filteredActions[2], "Deployment"))
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
	if _, ok := unstructuredObj.GetLabels()[shipper.ReleaseLabel]; !ok {
		t.Fatalf("could not find %q in Deployment .metadata.labels", shipper.ReleaseLabel)
	}

	_, expectedUnstructuredServiceContent := extractUnstructuredContent(scheme, existingService)

	uMetadata := unstructuredContent["metadata"].(map[string]interface{})
	sMetadata := expectedUnstructuredServiceContent["metadata"].(map[string]interface{})

	delete(uMetadata, "labels")
	delete(sMetadata, "labels")
	if !reflect.DeepEqual(uMetadata, sMetadata) {
		t.Fatalf("%s",
			diff.ObjectGoPrintDiff(uMetadata, sMetadata),
		)
	}

	uSpec := unstructuredContent["spec"].(map[string]interface{})
	sSpec := expectedUnstructuredServiceContent["spec"].(map[string]interface{})
	if !reflect.DeepEqual(uSpec, sSpec) {
		t.Fatalf("%s",
			diff.ObjectGoPrintDiff(uSpec, sSpec),
		)
	}
}

func validateDeploymentCreateAction(t *testing.T, obj runtime.Object) {
	scheme := kubescheme.Scheme

	u := &unstructured.Unstructured{}
	err := scheme.Convert(obj, u, nil)
	if err != nil {
		panic(err)
	}
	if _, ok := u.GetLabels()[shipper.ReleaseLabel]; !ok {
		t.Fatalf("could not find %q in Deployment .metadata.labels", shipper.ReleaseLabel)
	}

	deployment := &appsv1.Deployment{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, deployment); err != nil {
		t.Fatalf("could not decode deployment from unstructured: %s", err)
	}

	if _, ok := deployment.Spec.Selector.MatchLabels[shipper.ReleaseLabel]; !ok {
		t.Fatal("deployment .spec.selector.matchLabels doesn't contain shipperV1.ReleaseLabel")
	}

	if _, ok := deployment.Spec.Template.Labels[shipper.ReleaseLabel]; !ok {
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

	// there is a reviews-api-invalid-tarball.tgz in testdata which contains invalid deployment and service templates
	release := buildRelease("0.0.1", "reviews-api", "0", "deadbeef", "reviews-api")
	release.Environment.Chart.Version = "invalid-tarball"

	it := buildInstallationTarget(release, "reviews-api", "reviews-api", []string{cluster.Name})
	installer := newInstaller(release, it)

	clientsPerCluster, _, fakeDynamicClientBuilder, _ := initializeClients(apiResourceList, nil, objectsPerClusterMap{cluster.Name: []runtime.Object{}})

	fakePair := clientsPerCluster[cluster.Name]

	restConfig := &rest.Config{}
	if err := installer.installRelease(cluster, fakePair.fakeClient, restConfig, fakeDynamicClientBuilder); err == nil {
		t.Fatal("installRelease should fail, invalid tarball")
	}
}

// TestInstallerBrokenChartContents tests if the installation process fails when the
// release contains a valid chart tarball with invalid K8s object templates.
func TestInstallerBrokenChartContents(t *testing.T) {
	cluster := buildCluster("minikube-a")

	// There is a reviews-api-invalid-k8s-objects.tgz in testdata which contains
	// invalid deployment and service templates.
	release := buildRelease("0.0.1", "reviews-api", "0", "deadbeef", "reviews-api")
	release.Environment.Chart.Version = "invalid-k8s-objects"

	it := buildInstallationTarget(release, "reviews-api", "reviews-api", []string{cluster.Name})
	installer := newInstaller(release, it)

	clientsPerCluster, _, fakeDynamicClientBuilder, _ := initializeClients(apiResourceList, nil, objectsPerClusterMap{cluster.Name: nil})

	fakePair := clientsPerCluster[cluster.Name]

	restConfig := &rest.Config{}
	if err := installer.installRelease(cluster, fakePair.fakeClient, restConfig, fakeDynamicClientBuilder); err == nil {
		t.Fatal("installRelease should fail, invalid k8s objects")
	}
}
