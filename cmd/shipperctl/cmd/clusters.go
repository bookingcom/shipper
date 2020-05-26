package cmd

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	apiextensionv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/yaml"

	"github.com/bookingcom/shipper/cmd/shipperctl/config"
	"github.com/bookingcom/shipper/cmd/shipperctl/configurator"
	"github.com/bookingcom/shipper/cmd/shipperctl/tls"
	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperclient "github.com/bookingcom/shipper/pkg/client"
	"github.com/bookingcom/shipper/pkg/crds"
)

var (
	clustersYaml   string
	kubeConfigFile string

	shipperNamespace            string
	globalRolloutBlockNamespace string

	managementClusterContext         string
	managementClusterServiceAccount  string
	applicationClusterServiceAccount string

	setupCmd = &cobra.Command{
		Use:   "setup",
		Short: "setup Shipper in clusters",
	}

	setupMgmtCmd = &cobra.Command{
		Use:   "management",
		Short: "setup a Shipper management cluster",
		RunE:  runSetupMgmtClusterCommand,
	}

	setupAppCmd = &cobra.Command{
		Use:   "application",
		Short: "setup Shipper application clusters",
		RunE:  runSetupAppClusterCommand,
	}

	joinCmd = &cobra.Command{
		Use:   "join",
		Short: "join application clusters",
		RunE:  runJoinClustersCommand,
	}

	ClustersCmd = &cobra.Command{
		Use:   "clusters",
		Short: "manage Shipper clusters",
	}
)

const (
	fileFlagName       = "file"
	kubeConfigFlagName = "kubeconfig"
	level1Padding      = "    "

	validatingWebhookName = "shipper-validating-webhook"

	managementClusterRoleName         = "shipper:mgmt-cluster"
	managementClusterRoleBindingName  = "shipper:mgmt-cluster"
	applicationClusterRoleName        = "cluster-admin" // needs to be able to install any kind of Helm chart
	applicationClusterRoleBindingName = "shipper:app-cluster"
)

func init() {
	// Flags common to all commands under `shipperctl clusters`
	for _, cmd := range []*cobra.Command{joinCmd, setupMgmtCmd, setupAppCmd} {
		cmd.Flags().StringVar(&kubeConfigFile, kubeConfigFlagName, "~/.kube/config", "the path to the Kubernetes configuration file")
		if err := cmd.MarkFlagFilename(kubeConfigFlagName, "yaml"); err != nil {
			cmd.Printf("warning: could not mark %q for filename autocompletion: %s\n", kubeConfigFlagName, err)
		}

		cmd.Flags().StringVarP(&shipperNamespace, "namespace", "n", shipper.ShipperNamespace, "the namespace where Shipper is running")
		cmd.Flags().StringVar(&managementClusterContext, "management-cluster-context", "", "the name of the context to use to communicate with the management cluster. defaults to the current one")
	}

	setupMgmtCmd.Flags().StringVarP(&globalRolloutBlockNamespace, "rollout-blocks-global-namespace", "g", shipper.GlobalRolloutBlockNamespace, "the namespace where global RolloutBlocks should be created")
	setupMgmtCmd.Flags().StringVar(&managementClusterServiceAccount, "management-cluster-service-account", shipper.ShipperManagementServiceAccount, "the name of the service account Shipper will use in the management cluster")

	setupAppCmd.Flags().StringVar(&applicationClusterServiceAccount, "application-cluster-service-account", shipper.ShipperApplicationServiceAccount, "the name of the service account Shipper will use for the application cluster")

	joinCmd.Flags().StringVar(&applicationClusterServiceAccount, "application-cluster-service-account", shipper.ShipperApplicationServiceAccount, "the name of the service account Shipper will use for the application cluster")
	joinCmd.Flags().StringVarP(&clustersYaml, fileFlagName, "f", "clusters.yaml", "the path to a YAML file containing application cluster configuration")
	err := joinCmd.MarkFlagFilename(fileFlagName, "yaml")
	if err != nil {
		joinCmd.Printf("warning: could not mark %q for filename autocompletion: %s\n", fileFlagName, err)
	}

	setupCmd.AddCommand(setupMgmtCmd)
	setupCmd.AddCommand(setupAppCmd)

	ClustersCmd.AddCommand(setupCmd)
	ClustersCmd.AddCommand(joinCmd)
}

func runSetupMgmtClusterCommand(cmd *cobra.Command, args []string) error {
	configurator, err := configurator.NewClusterConfiguratorFromKubeConfig(kubeConfigFile, managementClusterContext)
	if err != nil {
		return err
	}

	cmd.Println("Setting up management cluster:")
	err = setupManagementCluster(cmd, configurator)
	if err != nil {
		return err
	}
	cmd.Println("Finished setting up management cluster")

	return nil
}

func runSetupAppClusterCommand(cmd *cobra.Command, args []string) error {
	mgmtConfigurator, err := configurator.NewClusterConfiguratorFromKubeConfig(kubeConfigFile, managementClusterContext)
	if err != nil {
		return err
	}

	clusterList, err := mgmtConfigurator.ListClusters()
	if err != nil {
		return err
	}

	cmd.Printf("Setting up %d application clusters:\n", len(clusterList.Items))

	for _, cluster := range clusterList.Items {
		cmd.Printf("Setting up application clusters %s:\n", cluster.Name)

		secret, err := mgmtConfigurator.FetchSecret(cluster.Name, shipperNamespace)
		if err != nil {
			return err
		}

		restConfig := shipperclient.BuildConfigFromClusterAndSecret(&cluster, secret)
		appConfigurator, err := configurator.NewClusterConfigurator(restConfig)
		if err != nil {
			return err
		}

		err = createOrUpdateApplicationCrds(cmd, appConfigurator)
		if err != nil {
			return err
		}
	}

	cmd.Println("Finished setting up application clusters")

	return nil
}

func runJoinClustersCommand(cmd *cobra.Command, args []string) error {
	clustersConfiguration, err := loadClustersConfiguration()
	if err != nil {
		return err
	}

	mgmtConfigurator, err := configurator.NewClusterConfiguratorFromKubeConfig(kubeConfigFile, managementClusterContext)
	if err != nil {
		return err
	}

	for _, appClusterConfig := range clustersConfiguration.ApplicationClusters {
		context := appClusterConfig.Context
		if context == "" {
			context = appClusterConfig.Name
		}

		appConfigurator, err := configurator.NewClusterConfiguratorFromKubeConfig(kubeConfigFile, context)
		if err != nil {
			return err
		}

		cmd.Printf("Creating application cluster accounts in cluster %s:\n", appClusterConfig.Name)
		err = setupApplicationClusterAccounts(cmd, appConfigurator)
		if err != nil {
			return err
		}
		cmd.Printf("Finished creating application cluster accounts in cluster %s\n\n", appClusterConfig.Name)

		cmd.Printf("Joining management cluster to application cluster %s:\n", appClusterConfig.Name)
		err = joinClusters(cmd, mgmtConfigurator, appConfigurator, appClusterConfig)
		if err != nil {
			return err
		}
		cmd.Printf("Finished joining management cluster to application cluster %s\n\n", appClusterConfig.Name)
	}

	return nil
}

func setupManagementCluster(cmd *cobra.Command, configurator *configurator.Cluster) error {
	if err := createOrUpdateManagementCrds(cmd, configurator); err != nil {
		return err
	}

	if err := createNamespace(cmd, configurator); err != nil {
		return err
	}

	if err := createGlobalRolloutBlockNamespace(cmd, configurator); err != nil {
		return err
	}

	if err := createManagementServiceAccount(cmd, configurator); err != nil {
		return err
	}

	if err := createManagementClusterRole(cmd, configurator); err != nil {
		return err
	}

	if err := createManagementClusterRoleBinding(cmd, configurator); err != nil {
		return err
	}

	if err := createValidatingWebhookSecret(cmd, configurator); err != nil {
		return err
	}

	if err := createValidatingWebhookConfiguration(cmd, configurator); err != nil {
		return err
	}

	if err := createValidatingWebhookService(cmd, configurator); err != nil {
		return err
	}

	return nil
}

func setupApplicationClusterAccounts(cmd *cobra.Command, configurator *configurator.Cluster) error {
	if err := createNamespace(cmd, configurator); err != nil {
		return err
	}

	if err := createApplicationServiceAccount(cmd, configurator); err != nil {
		return err
	}

	if err := createApplicationClusterRoleBinding(cmd, configurator); err != nil {
		return err
	}

	return nil
}

func joinClusters(
	cmd *cobra.Command,
	mgmtConfigurator, appConfigurator *configurator.Cluster,
	clusterConfig *config.ClusterConfiguration,
) error {
	cluster := &shipper.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterConfig.Name,
		},
		Spec: clusterConfig.ClusterSpec,
	}

	if cluster.Spec.APIMaster == "" {
		cluster.Spec.APIMaster = appConfigurator.Host
	}

	err := createClusterObject(cmd, mgmtConfigurator, cluster)
	if err != nil {
		return err
	}

	err = installAppClusterSecrets(cmd, appConfigurator, mgmtConfigurator, cluster.Name)
	if err != nil {
		return err
	}

	return nil
}

func createOrUpdateApplicationCrds(cmd *cobra.Command, configurator *configurator.Cluster) error {
	cmd.Print("Registering or updating custom resource definitions... ")

	crds := []*apiextensionv1beta1.CustomResourceDefinition{
		crds.InstallationTarget,
		crds.CapacityTarget,
		crds.TrafficTarget,
	}

	for _, crd := range crds {
		err := configurator.CreateOrUpdateCRD(crd)
		if err != nil {
			return err
		}
	}

	cmd.Println("done")

	return nil
}

func createOrUpdateManagementCrds(cmd *cobra.Command, configurator *configurator.Cluster) error {
	cmd.Print("Registering or updating custom resource definitions... ")

	crds := []*apiextensionv1beta1.CustomResourceDefinition{
		crds.Cluster,
		crds.RolloutBlock,
		crds.Application,
		crds.Release,
	}

	for _, crd := range crds {
		err := configurator.CreateOrUpdateCRD(crd)
		if err != nil {
			return err
		}
	}

	cmd.Println("done")

	return nil
}

func createNamespace(cmd *cobra.Command, configurator *configurator.Cluster) error {
	cmd.Printf("Creating a namespace called %s... ", shipperNamespace)
	if err := configurator.CreateNamespace(shipperNamespace); err != nil {
		if errors.IsAlreadyExists(err) {
			cmd.Println("already exists. Skipping")
			return nil
		}

		return err
	}

	cmd.Println("done")
	return nil
}

func createGlobalRolloutBlockNamespace(cmd *cobra.Command, configurator *configurator.Cluster) error {
	cmd.Printf("Creating a namespace called %s... ", globalRolloutBlockNamespace)
	if err := configurator.CreateNamespace(globalRolloutBlockNamespace); err != nil {
		if errors.IsAlreadyExists(err) {
			cmd.Println("already exists. Skipping")
			return nil
		} else {
			return err
		}
	}

	cmd.Println("done")
	return nil
}

func createManagementServiceAccount(cmd *cobra.Command, configurator *configurator.Cluster) error {
	cmd.Printf("Creating a service account called %s... ", managementClusterServiceAccount)

	err := configurator.CreateServiceAccount(
		shipper.RBACManagementDomain,
		shipperNamespace,
		managementClusterServiceAccount,
	)

	if err != nil {
		if errors.IsAlreadyExists(err) {
			cmd.Println("already exists. Skipping")
			return nil
		} else {
			return err
		}
	}

	cmd.Println("done")
	return nil
}

func createValidatingWebhookSecret(cmd *cobra.Command, configurator *configurator.Cluster) error {
	cmd.Printf("Checking if a secret already exists for the validating webhook in the %s namespace... ", shipperNamespace)

	exists, err := configurator.ValidatingWebhookSecretExists(shipperNamespace)
	if err != nil {
		return err
	}

	if exists {
		cmd.Println("yes. Skipping")
		return nil
	}
	cmd.Println("no.")

	cmd.Println("Creating a secret for the validating webhook:")

	cmd.Printf("%sGenerating a private key... ", level1Padding)
	privatekey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	cmd.Println("done")

	cmd.Printf("%sCreating a TLS certificate signing request... ", level1Padding)
	csr, err := tls.GenerateCSRForServiceInNamespace(privatekey, validatingWebhookName, shipperNamespace)
	if err != nil {
		return err
	}
	cmd.Println("done")

	cmd.Printf("%sCreating a Kubernetes CertificateSigningRequest... ", level1Padding)
	if err := configurator.CreateCertificateSigningRequest(csr); err != nil {
		return err
	}
	cmd.Println("done")

	cmd.Printf("%sApproving the CertificateSigningRequest... ", level1Padding)
	if err := configurator.ApproveShipperCSR(); err != nil {
		return err
	}
	cmd.Println("done")

	cmd.Printf("%sFetching the certificate from the CertificateSigningRequest object... ", level1Padding)
	certificate, err := configurator.FetchCertificateFromCSR()
	if err != nil {
		return err
	}
	cmd.Println("done")

	// The private key we generated is not encoded as PEM, so we
	// have to convert it. The certificate, however, is already
	// PEM-encoded when we get it from Kubernetes above.
	privatekeyPEM := tls.EncodePrivateKeyAsPEM(x509.MarshalPKCS1PrivateKey(privatekey))

	cmd.Printf("%sCreating the Secret using the private key and certificate in the %s namespace... ", level1Padding, shipperNamespace)
	if err := configurator.CreateValidatingWebhookSecret(privatekeyPEM, certificate, shipperNamespace); err != nil {
		return err
	}
	cmd.Println("done")

	return nil
}

func createValidatingWebhookConfiguration(cmd *cobra.Command, configurator *configurator.Cluster) error {
	cmd.Printf("Creating the ValidatingWebhookConfiguration in %s namespace... ", shipperNamespace)
	caBundle, err := configurator.FetchKubernetesCABundle()
	if err != nil {
		return err
	}

	if err := configurator.CreateOrUpdateValidatingWebhookConfiguration(caBundle, shipperNamespace); err != nil {
		if errors.IsAlreadyExists(err) {
			cmd.Println("already exists. Skipping")
			return nil
		}

		return err
	}
	cmd.Println("done")

	return nil
}

func createValidatingWebhookService(cmd *cobra.Command, configurator *configurator.Cluster) error {
	cmd.Print("Creating a Service object for the validating webhook... ")
	if err := configurator.CreateOrUpdateValidatingWebhookService(shipperNamespace); err != nil {
		if errors.IsAlreadyExists(err) {
			cmd.Println("already exists. Skipping")
			return nil
		}

		return err
	}
	cmd.Println("done")

	return nil
}

func createApplicationServiceAccount(cmd *cobra.Command, configurator *configurator.Cluster) error {
	cmd.Printf("Creating a service account called %s... ", applicationClusterServiceAccount)

	err := configurator.CreateServiceAccount(
		shipper.RBACApplicationDomain,
		shipperNamespace,
		applicationClusterServiceAccount,
	)

	if err != nil {
		if errors.IsAlreadyExists(err) {
			cmd.Println("already exists. Skipping")
			return nil
		} else {
			return err
		}
	}

	cmd.Println("done")
	return nil
}

func createManagementClusterRole(cmd *cobra.Command, configurator *configurator.Cluster) error {
	cmd.Printf("Creating a ClusterRole called %s... ", managementClusterRoleName)
	if err := configurator.CreateClusterRole(shipper.RBACManagementDomain, managementClusterRoleName); err != nil {
		if errors.IsAlreadyExists(err) {
			cmd.Println("already exists. Skipping")
			return nil
		} else {
			return err
		}
	}

	cmd.Println("done")
	return nil
}

func createManagementClusterRoleBinding(cmd *cobra.Command, configurator *configurator.Cluster) error {
	cmd.Printf("Creating a ClusterRoleBinding called %s... ", managementClusterRoleBindingName)
	err := configurator.CreateClusterRoleBinding(
		shipper.RBACManagementDomain,
		managementClusterRoleBindingName,
		managementClusterRoleName,
		managementClusterServiceAccount,
		shipperNamespace,
	)

	if err != nil {
		if errors.IsAlreadyExists(err) {
			cmd.Println("already exists. Skipping")
			return nil
		} else {
			return err
		}
	}

	cmd.Println("done")
	return nil
}

func createApplicationClusterRoleBinding(cmd *cobra.Command, configurator *configurator.Cluster) error {
	cmd.Printf("Creating a ClusterRoleBinding called %s... ", applicationClusterRoleBindingName)
	err := configurator.CreateClusterRoleBinding(
		shipper.RBACApplicationDomain,
		applicationClusterRoleBindingName,
		applicationClusterRoleName,
		applicationClusterServiceAccount,
		shipperNamespace,
	)

	if err != nil {
		if errors.IsAlreadyExists(err) {
			cmd.Println("already exists. Skipping")
			return nil
		} else {
			return err
		}
	}

	cmd.Println("done")
	return nil
}

func installAppClusterSecrets(
	cmd *cobra.Command,
	appConfigurator, mgmtConfigurator *configurator.Cluster,
	appClusterName string,
) error {
	// The workflow for this function is different to generate more
	// readable output
	cmd.Printf("Checking whether a secret for the %s cluster exists in the %s namespace... ", appClusterName, shipperNamespace)
	shouldCopySecret, err := mgmtConfigurator.ShouldCopySecret(appClusterName, shipperNamespace)
	if err != nil {
		return err
	}

	if !shouldCopySecret {
		cmd.Println("yes. Skipping")
		return nil
	}

	cmd.Printf("no. Fetching secret for service account %s from the %s cluster... ", applicationClusterServiceAccount, appClusterName)

	// Poll the server until the service account's `secrets` field is
	// populated.
	var secret *corev1.Secret
	for {
		secret, err = appConfigurator.FetchSecretForServiceAccount(applicationClusterServiceAccount, shipperNamespace)
		if err != nil {
			if !configurator.IsSecretNotPopulatedError(err) {
				return err
			} else {
				time.Sleep(1 * time.Second)
				continue
			}
		}

		break
	}

	cmd.Println("done")

	appCluster, err := mgmtConfigurator.FetchCluster(appClusterName)
	if err != nil {
		return err
	}

	cmd.Print("Copying the secret to the management cluster... ")
	err = mgmtConfigurator.CopySecret(appCluster, shipperNamespace, secret)
	if err != nil {
		return err
	}
	cmd.Println("done")

	return nil
}

func createClusterObject(
	cmd *cobra.Command,
	mgmtConfigurator *configurator.Cluster,
	appCluster *shipper.Cluster,
) error {
	cmd.Printf("Creating or updating the cluster object for cluster %s on the management cluster... ", appCluster.Name)

	// Initialize the map of capabilities if it's null so that we don't
	// fire an error when creating it
	if appCluster.Spec.Capabilities == nil {
		appCluster.Spec.Capabilities = []string{}
	}

	err := mgmtConfigurator.CreateOrUpdateCluster(appCluster)
	if err != nil {
		return err
	}

	cmd.Println("done")

	return nil
}

func loadClustersConfiguration() (*config.ClustersConfiguration, error) {
	configBytes, err := ioutil.ReadFile(clustersYaml)
	if err != nil {
		return nil, err
	}

	configuration := &config.ClustersConfiguration{}
	err = yaml.Unmarshal(configBytes, configuration)
	if err != nil {
		return nil, err
	}

	for _, cluster := range configuration.ApplicationClusters {
		if msgs := validation.IsDNS1123Subdomain(cluster.Name); len(msgs) > 0 {
			return nil, fmt.Errorf("%q is not a valid cluster name in Kubernetes. If this is the name of a context in your Kubernetes configuration, check out the relevant example at https://docs.shipper-k8s.io/en/latest/operations/shipperctl.html#using-google-kubernetes-engine-gke-context-names", cluster.Name)
		}

		if cluster.Region == "" {
			return nil, fmt.Errorf("you must specify region for cluster %s", cluster.Name)
		}
	}

	return configuration, nil
}
