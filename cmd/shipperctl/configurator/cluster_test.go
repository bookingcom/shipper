package configurator

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	fakeapiextensionclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	kubefake "k8s.io/client-go/kubernetes/fake"
	kubetesting "k8s.io/client-go/testing"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
)

const shipperSystemNamespace = "shipper-system"

func TestCreateValidatingWebhookConfiguration(t *testing.T) {
	f := newFixture(t)
	caBundle := []byte{}
	if err := f.configurator.CreateOrUpdateValidatingWebhookConfiguration(caBundle, shipperSystemNamespace); err != nil {
		t.Fatal(err)
	}

	operations := []admissionregistrationv1beta1.OperationType{
		admissionregistrationv1beta1.Create,
		admissionregistrationv1beta1.Update,
	}
	expectedConfiguration := f.newValidatingWebhookConfiguration(caBundle, shipperSystemNamespace, operations)
	gvr := admissionregistrationv1beta1.SchemeGroupVersion.WithResource("validatingwebhookconfigurations")
	createAction := kubetesting.NewCreateAction(gvr, "", expectedConfiguration)
	f.actions = append(f.actions, createAction)

	clientSet, ok := f.configurator.KubeClient.(*kubefake.Clientset)
	if !ok {
		t.Fatalf("not a *kubefake.Clientset: %#v", f.configurator.KubeClient)
	}
	actualActions := shippertesting.FilterActions(clientSet.Actions())
	shippertesting.CheckActions(f.actions, actualActions, f.t)
}

func TestUpdateValidatingWebhookConfiguration(t *testing.T) {
	f := newFixture(t)
	caBundle := []byte{}

	operations := []admissionregistrationv1beta1.OperationType{
		admissionregistrationv1beta1.Create,
	}
	configuration := f.newValidatingWebhookConfiguration(caBundle, shipperSystemNamespace, operations)
	if _, err := f.configurator.KubeClient.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Create(configuration); err != nil {
		t.Fatal(err)
	}
	gvr := admissionregistrationv1beta1.SchemeGroupVersion.WithResource("validatingwebhookconfigurations")
	createAction := kubetesting.NewCreateAction(gvr, "", configuration)
	f.actions = append(f.actions, createAction)

	if err := f.configurator.CreateOrUpdateValidatingWebhookConfiguration(caBundle, shipperSystemNamespace); err != nil {
		t.Fatal(err)
	}

	operations = []admissionregistrationv1beta1.OperationType{
		admissionregistrationv1beta1.Create,
		admissionregistrationv1beta1.Update,
	}
	expectedConfiguration := f.newValidatingWebhookConfiguration(caBundle, shipperSystemNamespace, operations)
	updateAction := kubetesting.NewUpdateAction(gvr, "", expectedConfiguration)
	f.actions = append(f.actions, updateAction)

	clientSet, ok := f.configurator.KubeClient.(*kubefake.Clientset)
	if !ok {
		t.Fatalf("not a *kubefake.Clientset: %#v", f.configurator.KubeClient)
	}
	actualActions := shippertesting.FilterActions(clientSet.Actions())
	shippertesting.CheckActions(f.actions, actualActions, f.t)
}

func TestCreateValidatingWebhookService(t *testing.T) {
	f := newFixture(t)
	if err := f.configurator.CreateOrUpdateValidatingWebhookService(shipperSystemNamespace); err != nil {
		t.Fatal(err)
	}

	expectedService := f.newValidatingWebhookService("shipper", shipperSystemNamespace)
	gvr := corev1.SchemeGroupVersion.WithResource("services")
	createAction := kubetesting.NewCreateAction(gvr, shipperSystemNamespace, expectedService)
	f.actions = append(f.actions, createAction)

	clientSet, ok := f.configurator.KubeClient.(*kubefake.Clientset)
	if !ok {
		t.Fatalf("not a *kubefake.Clientset: %#v", f.configurator.KubeClient)
	}
	actualActions := shippertesting.FilterActions(clientSet.Actions())
	shippertesting.CheckActions(f.actions, actualActions, f.t)
}

func TestUpdateValidatingWebhookService(t *testing.T) {
	f := newFixture(t)
	service := f.newValidatingWebhookService("Hello", shipperSystemNamespace)
	if _, err := f.configurator.KubeClient.CoreV1().Services(shipperSystemNamespace).Create(service); err != nil {
		t.Fatal(err)
	}
	gvr := corev1.SchemeGroupVersion.WithResource("services")
	createAction := kubetesting.NewCreateAction(gvr, shipperSystemNamespace, service)
	f.actions = append(f.actions, createAction)

	if err := f.configurator.CreateOrUpdateValidatingWebhookService(shipperSystemNamespace); err != nil {
		t.Fatal(err)
	}

	expectedService := f.newValidatingWebhookService("shipper", shipperSystemNamespace)
	updateAction := kubetesting.NewUpdateAction(gvr, shipperSystemNamespace, expectedService)
	f.actions = append(f.actions, updateAction)

	clientSet, ok := f.configurator.KubeClient.(*kubefake.Clientset)
	if !ok {
		t.Fatalf("not a *kubefake.Clientset: %#v", f.configurator.KubeClient)
	}
	actualActions := shippertesting.FilterActions(clientSet.Actions())
	shippertesting.CheckActions(f.actions, actualActions, f.t)
}

type fixture struct {
	t            *testing.T
	configurator *Cluster

	actions []kubetesting.Action
}

func newFixture(t *testing.T) *fixture {
	return &fixture{
		t:            t,
		configurator: newCluster(),
	}
}

func newCluster() *Cluster {
	return &Cluster{
		KubeClient:         kubefake.NewSimpleClientset(),
		ShipperClient:      shipperfake.NewSimpleClientset(),
		ApiExtensionClient: fakeapiextensionclientset.NewSimpleClientset(),
		Host:               "localhost:8080",
	}
}

func (f *fixture) newValidatingWebhookConfiguration(caBundle []byte, namespace string, operations []admissionregistrationv1beta1.OperationType) *admissionregistrationv1beta1.ValidatingWebhookConfiguration {
	path := shipperValidatingWebhookServicePath
	return &admissionregistrationv1beta1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: shipperValidatingWebhookName,
		},
		Webhooks: []admissionregistrationv1beta1.ValidatingWebhook{
			admissionregistrationv1beta1.ValidatingWebhook{
				Name: shipperValidatingWebhookName,
				ClientConfig: admissionregistrationv1beta1.WebhookClientConfig{
					CABundle: caBundle,
					Service: &admissionregistrationv1beta1.ServiceReference{
						Name:      shipperValidatingWebhookServiceName,
						Namespace: namespace,
						Path:      &path,
					},
				},
				Rules: []admissionregistrationv1beta1.RuleWithOperations{
					admissionregistrationv1beta1.RuleWithOperations{
						Operations: operations,
						Rule: admissionregistrationv1beta1.Rule{
							APIGroups:   []string{shipper.SchemeGroupVersion.Group},
							APIVersions: []string{shipper.SchemeGroupVersion.Version},
							Resources:   []string{"*"},
						},
					},
				},
			},
		},
	}
}

func (f *fixture) newValidatingWebhookService(appName, namespace string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      shipperValidatingWebhookServiceName,
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": appName,
			},
			Ports: []corev1.ServicePort{
				corev1.ServicePort{
					Port:       443,
					TargetPort: intstr.FromInt(9443),
				},
			},
		},
	}
}
