package configurator

import (
	"fmt"
	"time"

	certificatesv1beta1 "k8s.io/api/certificates/v1beta1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	client "github.com/bookingcom/shipper/pkg/client"
	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	"github.com/mitchellh/go-homedir"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	shipperCSRName                      = "shipper-validating-webhook"
	shipperValidatingWebhookSecretName  = "shipper-validating-webhook"
	shipperValidatingWebhookName        = "shipper.booking.com"
	shipperValidatingWebhookServiceName = "shipper-validating-webhook"
	shipperValidatingWebhookServicePath = "/validate"
	MaximumRetries                      = 20
	AgentName                           = "configurator"
)

type Cluster struct {
	KubeClient         kubernetes.Interface
	ShipperClient      shipperclientset.Interface
	ApiExtensionClient apiextensionclientset.Interface
	Host               string
}

func (c *Cluster) CreateNamespace(namespace string) error {
	namespaceObject := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}

	_, err := c.KubeClient.CoreV1().Namespaces().Create(namespaceObject)

	return err
}

func (c *Cluster) CreateServiceAccount(domain, namespace string, name string) error {
	serviceAccount := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				shipper.RBACDomainLabel: domain,
			},
		},
	}

	_, err := c.KubeClient.CoreV1().ServiceAccounts(namespace).Create(serviceAccount)

	return err
}

func (c *Cluster) CreateApplicationClusterRole(name, domain string) error {
	err := c.createClusterRole(getApplicationClusterRole(name, domain))
	return err
}

func (c *Cluster) CreateManagementClusterRole(name, domain string) error {
	err := c.createClusterRole(getManagementClusterRole(name, domain))
	return err
}

func (c *Cluster) createClusterRole(role *rbacv1.ClusterRole) error {
	_, err := c.KubeClient.RbacV1().ClusterRoles().Create(role)
	return err
}

func (c *Cluster) CreateClusterRoleBinding(domain, clusterRoleBindingName, clusterRoleName, subjectName, subjectNamespace string) error {
	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterRoleBindingName,
			Labels: map[string]string{
				shipper.RBACDomainLabel: domain,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     clusterRoleName,
		},
		Subjects: []rbacv1.Subject{
			rbacv1.Subject{
				Kind:      "ServiceAccount",
				Name:      subjectName,
				Namespace: subjectNamespace,
			},
		},
	}

	_, err := c.KubeClient.RbacV1().ClusterRoleBindings().Create(clusterRoleBinding)

	return err
}

func (c *Cluster) ShouldCopySecret(name, namespace string) (bool, error) {
	_, err := c.KubeClient.CoreV1().Secrets(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return true, nil
		} else {
			return false, err
		}
	}

	return false, nil
}

// FetchSecretForServiceAccount polls the server 10 times with a 1
// second delay between each. If there is still no secret, it returns
// a SecretNotPopulated error.
func (c *Cluster) FetchSecretForServiceAccount(name, namespace string) (*corev1.Secret, error) {
	var serviceAccount *corev1.ServiceAccount
	var err error
	found := false
	for i := 0; i < 10; i++ {
		serviceAccount, err = c.KubeClient.CoreV1().ServiceAccounts(namespace).Get(name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}

		if len(serviceAccount.Secrets) == 0 {
			time.Sleep(1 * time.Second)
			continue
		}

		found = true
		break
	}

	if !found {
		return nil, NewSecretNotPopulatedError(serviceAccount)
	}

	secretName := serviceAccount.Secrets[0].Name
	secret, err := c.KubeClient.CoreV1().Secrets(namespace).Get(secretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return secret, nil
}

func (c *Cluster) FetchCluster(clusterName string) (*shipper.Cluster, error) {
	return c.ShipperClient.ShipperV1alpha1().Clusters().Get(clusterName, metav1.GetOptions{})
}

func (c *Cluster) CopySecret(cluster *shipper.Cluster, newNamespace string, secret *corev1.Secret) error {
	newSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: newNamespace,
			OwnerReferences: []metav1.OwnerReference{
				metav1.OwnerReference{
					APIVersion: "shipper.booking.com/v1",
					Kind:       "Cluster",
					Name:       cluster.Name,
					UID:        cluster.UID,
				},
			},
		},
		Data: secret.Data,
	}

	_, err := c.KubeClient.CoreV1().Secrets(newNamespace).Create(newSecret)

	return err
}

func (c *Cluster) CreateOrUpdateCluster(cluster *shipper.Cluster) error {
	existingCluster, err := c.ShipperClient.ShipperV1alpha1().Clusters().Get(cluster.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			_, err := c.ShipperClient.ShipperV1alpha1().Clusters().Create(cluster)
			return err
		} else {
			return err
		}
	}

	existingCluster.Spec = cluster.Spec
	_, err = c.ShipperClient.ShipperV1alpha1().Clusters().Update(existingCluster)
	return err
}

func (c *Cluster) CreateOrUpdateCRD(crd *apiextensionv1beta1.CustomResourceDefinition) error {
	existingCrd, err := c.ApiExtensionClient.ApiextensionsV1beta1().CustomResourceDefinitions().Get(crd.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			_, err := c.ApiExtensionClient.ApiextensionsV1beta1().CustomResourceDefinitions().Create(crd)
			return err
		} else {
			return err
		}
	}

	existingCrd.Spec = crd.Spec
	_, err = c.ApiExtensionClient.ApiextensionsV1beta1().CustomResourceDefinitions().Update(existingCrd)
	return err
}

func NewKubeClientFromKubeConfig(kubeConfigFile, context string) (kubernetes.Interface, error) {
	config, err := NewClusterConfiguratorFromKubeConfig(kubeConfigFile, context)
	if err != nil {
		return nil, err
	}
	return config.KubeClient, nil
}

func NewShipperClientFromKubeConfig(kubeConfigFile, context string) (shipperclientset.Interface, error) {
	config, err := NewClusterConfiguratorFromKubeConfig(kubeConfigFile, context)
	if err != nil {
		return nil, err
	}
	return config.ShipperClient, nil
}

func NewClusterConfiguratorFromKubeConfig(kubeConfigFile, context string) (*Cluster, error) {
	restConfig, err := loadKubeConfig(kubeConfigFile, context)
	if err != nil {
		return nil, err
	}

	return NewClusterConfigurator(restConfig)
}

func NewClusterConfigurator(restConfig *rest.Config) (*Cluster, error) {
	clientset, err := client.NewKubeClient(restConfig, AgentName, nil)
	if err != nil {
		return nil, err
	}

	shipperClient, err := client.NewShipperClient(restConfig, AgentName, nil)
	if err != nil {
		return nil, err
	}

	apiExtensionClient, err := client.NewApiExtensionClient(restConfig, AgentName, nil)
	if err != nil {
		return nil, err
	}

	configurator := &Cluster{
		KubeClient:         clientset,
		ShipperClient:      shipperClient,
		ApiExtensionClient: apiExtensionClient,
		Host:               restConfig.Host,
	}

	return configurator, nil
}

func (c *Cluster) CreateCertificateSigningRequest(csr []byte) error {
	certificateSigningRequest := &certificatesv1beta1.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: shipperCSRName,
		},
		Spec: certificatesv1beta1.CertificateSigningRequestSpec{
			Request: csr,
			Groups:  []string{"system:authenticated"},
			Usages: []certificatesv1beta1.KeyUsage{
				certificatesv1beta1.UsageServerAuth,
				certificatesv1beta1.UsageDigitalSignature,
				certificatesv1beta1.UsageKeyEncipherment,
			},
		},
	}

	_, err := c.KubeClient.CertificatesV1beta1().CertificateSigningRequests().Create(certificateSigningRequest)
	return err
}

func (c *Cluster) ApproveShipperCSR() error {
	csr, err := c.KubeClient.CertificatesV1beta1().CertificateSigningRequests().Get(shipperCSRName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	approvedCondition := certificatesv1beta1.CertificateSigningRequestCondition{
		Type:    certificatesv1beta1.CertificateApproved,
		Reason:  "ShipperctlApprove",
		Message: "Automatically approved by shipperctl",
	}

	csr.Status.Conditions = append(csr.Status.Conditions, approvedCondition)
	_, err = c.KubeClient.CertificatesV1beta1().CertificateSigningRequests().UpdateApproval(csr)

	return err
}

// FetchCertificateFromCSR continually fetches the Shipper CSR until
// it is populated with a certificate and then returns the PEM-encoded
// certificate from the Status. This is a blocking function.
//
// Note that the returned certificate is already PEM-encoded.
func (c *Cluster) FetchCertificateFromCSR() ([]byte, error) {
	for retries := 0; retries < MaximumRetries; retries++ {
		csr, err := c.KubeClient.CertificatesV1beta1().CertificateSigningRequests().Get(shipperCSRName, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}

		if len(csr.Status.Certificate) == 0 {
			// Pause to give the server some time to sign and populate the certificate
			time.Sleep(1 * time.Second)
			continue
		}

		return csr.Status.Certificate, nil
	}

	// If we reach here, we have failed to get the certificate after all the retries
	return nil, fmt.Errorf("certificate is not populated after %d retries", MaximumRetries)
}

func (c *Cluster) ValidatingWebhookSecretExists(namespace string) (bool, error) {
	_, err := c.KubeClient.CoreV1().Secrets(namespace).Get(shipperValidatingWebhookSecretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}

		return false, err
	}

	return true, nil
}

func (c *Cluster) CreateValidatingWebhookSecret(privateKey, certificate []byte, namespace string) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: shipperValidatingWebhookSecretName,
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSPrivateKeyKey: privateKey,
			corev1.TLSCertKey:       certificate,
		},
	}

	_, err := c.KubeClient.CoreV1().Secrets(namespace).Create(secret)
	if err != nil {
		return err
	}

	return nil
}

func (c *Cluster) FetchKubernetesCABundle() ([]byte, error) {
	configmap, err := c.KubeClient.CoreV1().ConfigMaps("kube-system").Get("extension-apiserver-authentication", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	caBundle, ok := configmap.Data["client-ca-file"]
	if !ok {
		return nil, fmt.Errorf("there is no `client-ca-file` on the `extension-apiserver-authentication` configmap in the `kube-system` namespace")
	}

	return []byte(caBundle), nil
}

func (c *Cluster) CreateOrUpdateValidatingWebhookConfiguration(caBundle []byte, namespace string, setToIgnore bool) error {
	path := shipperValidatingWebhookServicePath
	sideEffectClassNone := admissionregistrationv1beta1.SideEffectClassNone
	failurePolicy := admissionregistrationv1beta1.Fail
	if setToIgnore {
		failurePolicy = admissionregistrationv1beta1.Ignore
	}
	validatingWebhookConfiguration := &admissionregistrationv1beta1.ValidatingWebhookConfiguration{
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
						Operations: []admissionregistrationv1beta1.OperationType{
							admissionregistrationv1beta1.Create,
							admissionregistrationv1beta1.Update,
							admissionregistrationv1beta1.Delete,
						},
						Rule: admissionregistrationv1beta1.Rule{
							APIGroups:   []string{shipper.SchemeGroupVersion.Group},
							APIVersions: []string{"*"},
							Resources:   []string{"*"},
						},
					},
				},
				SideEffects:   &sideEffectClassNone,
				FailurePolicy: &failurePolicy,
			},
		},
	}

	existingConfig, err := c.KubeClient.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Get(shipperValidatingWebhookName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			_, err = c.KubeClient.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Create(validatingWebhookConfiguration)
			return err
		} else {
			return err
		}
	}

	existingConfig.Webhooks = validatingWebhookConfiguration.Webhooks
	_, err = c.KubeClient.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Update(existingConfig)
	return err
}

func (c *Cluster) UpdateValidatingWebhookConfigurationFailurePolicyToFail() error {
	policyTypeFail := admissionregistrationv1beta1.Fail
	existingConfig, err := c.KubeClient.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Get(shipperValidatingWebhookName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	for i := range existingConfig.Webhooks {
		existingConfig.Webhooks[i].FailurePolicy = &policyTypeFail
	}

	_, err = c.KubeClient.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Update(existingConfig)
	return err
}

func (c *Cluster) CreateOrUpdateValidatingWebhookService(namespace string) error {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      shipperValidatingWebhookServiceName,
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": "shipper",
			},
			Ports: []corev1.ServicePort{
				corev1.ServicePort{
					Port:       443,
					TargetPort: intstr.FromInt(9443),
				},
			},
		},
	}

	existingSerivce, err := c.KubeClient.CoreV1().Services(namespace).Get(shipperValidatingWebhookServiceName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			_, err = c.KubeClient.CoreV1().Services(namespace).Create(service)
			return err
		} else {
			return err
		}
	}

	existingSerivce.Spec.Selector = service.Spec.Selector
	existingSerivce.Spec.Ports = service.Spec.Ports
	_, err = c.KubeClient.CoreV1().Services(namespace).Update(existingSerivce)
	return err
}

func loadKubeConfig(kubeConfig, context string) (*rest.Config, error) {
	clientConfig, config, err := ClientConfig(kubeConfig, context)
	if err != nil {
		return config, err
	}
	return clientConfig.ClientConfig()
}

func ClientConfig(kubeConfig string, context string) (clientcmd.ClientConfig, *rest.Config, error) {
	path, err := homedir.Expand(kubeConfig)
	if err != nil {
		return nil, nil, err
	}

	overrides := &clientcmd.ConfigOverrides{CurrentContext: context}
	if context != "" {
		overrides.CurrentContext = context
	}

	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: path},
		overrides,
	)
	return clientConfig, nil, nil
}
