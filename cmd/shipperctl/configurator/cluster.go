package configurator

import (
	"encoding/hex"
	"hash/crc32"

	homedir "github.com/mitchellh/go-homedir"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/bookingcom/shipper/cmd/shipperctl/config"
	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
)

type Cluster struct {
	KubeClient         *kubernetes.Clientset
	ShipperClient      *shipperclientset.Clientset
	ApiExtensionClient *apiextensionclientset.Clientset
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

func (c *Cluster) CreateClusterRole(domain, name string) error {
	clusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				shipper.RBACDomainLabel: domain,
			},
		},
		Rules: []rbacv1.PolicyRule{
			rbacv1.PolicyRule{
				Verbs:     []string{rbacv1.VerbAll},
				APIGroups: []string{shipper.SchemeGroupVersion.Group},
				Resources: []string{rbacv1.ResourceAll},
			},
			rbacv1.PolicyRule{
				Verbs:     []string{"update", "get", "list", "watch"},
				APIGroups: []string{""},
				Resources: []string{"secrets"},
			},
			rbacv1.PolicyRule{
				Verbs:     []string{rbacv1.VerbAll},
				APIGroups: []string{""},
				Resources: []string{"events"},
			},
		},
	}

	_, err := c.KubeClient.RbacV1().ClusterRoles().Create(clusterRole)

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

func (c *Cluster) FetchSecretForServiceAccount(name, namespace string) (*corev1.Secret, error) {
	serviceAccount, err := c.KubeClient.CoreV1().ServiceAccounts(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	if len(serviceAccount.Secrets) == 0 {
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
	hash := crc32.NewIEEE()
	hash.Write(secret.Data["ca.crt"])
	hash.Write(secret.Data["token"])

	newSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: newNamespace,
			Annotations: map[string]string{
				shipper.SecretChecksumAnnotation: hex.EncodeToString(hash.Sum(nil)),
			},
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

func (c *Cluster) CreateOrUpdateClusterWithConfig(configuration *config.ClusterConfiguration, host string) error {
	existingCluster, err := c.ShipperClient.ShipperV1alpha1().Clusters().Get(configuration.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return c.CreateClusterFromConfig(configuration, host)
		} else {
			return err
		}
	}

	existingCluster.Spec = configuration.ClusterSpec
	existingCluster.Spec.APIMaster = host
	_, err = c.ShipperClient.ShipperV1alpha1().Clusters().Update(existingCluster)
	return err
}

func (c *Cluster) CreateClusterFromConfig(configuration *config.ClusterConfiguration, host string) error {
	cluster := &shipper.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: configuration.Name,
		},
		Spec: configuration.ClusterSpec,
	}

	cluster.Spec.APIMaster = host
	_, err := c.ShipperClient.ShipperV1alpha1().Clusters().Create(cluster)
	return err
}

func (c *Cluster) CreateOrUpdateCRD(crd *apiextensionv1beta1.CustomResourceDefinition) error {
	existingCrd, err := c.ApiExtensionClient.ApiextensionsV1beta1().CustomResourceDefinitions().Get(crd.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return c.CreateCrd(crd)
		} else {
			return err
		}
	}

	existingCrd.Spec = crd.Spec
	_, err = c.ApiExtensionClient.ApiextensionsV1beta1().CustomResourceDefinitions().Update(existingCrd)
	return err
}

func (c *Cluster) CreateCrd(crd *apiextensionv1beta1.CustomResourceDefinition) error {
	_, err := c.ApiExtensionClient.ApiextensionsV1beta1().CustomResourceDefinitions().Create(crd)
	return err
}

func NewClusterConfigurator(clusterConfiguration *config.ClusterConfiguration, kubeConfigFile string) (*Cluster, error) {
	var context string
	if clusterConfiguration.Context != "" {
		context = clusterConfiguration.Context
	} else {
		context = clusterConfiguration.Name
	}

	restConfig, err := loadKubeConfig(context, kubeConfigFile)
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}

	shipperClient, err := shipperclientset.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}

	apiExtensionClient, err := apiextensionclientset.NewForConfig(restConfig)
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

func loadKubeConfig(context, kubeConfigFile string) (*rest.Config, error) {
	kubeConfigFilePath, err := homedir.Expand(kubeConfigFile)
	if err != nil {
		return nil, err
	}

	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeConfigFilePath},
		&clientcmd.ConfigOverrides{CurrentContext: context},
	)

	return clientConfig.ClientConfig()
}
