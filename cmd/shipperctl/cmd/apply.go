package cmd

import (
	"fmt"
	"io/ioutil"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/validation"

	"crypto/rand"
	"crypto/rsa"

	"crypto/x509"

	"github.com/bookingcom/shipper/cmd/shipperctl/config"
	"github.com/bookingcom/shipper/cmd/shipperctl/configurator"
	"github.com/bookingcom/shipper/cmd/shipperctl/tls"
	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/crds"
)

var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Set up management and application clusters to work with Shipper",
	RunE:  runApplyClustersCommand,
}

// Parameters
var (
	configFile                  string
	kubeConfigFile              string
	shipperSystemNamespace      string
	globalRolloutBlockNamespace string
)

// Name constants
const (
	level1Padding                              = "    "
	shipperManagementServiceName               = "shipper-validating-webhook"
	shipperManagementClusterServiceAccountName = "shipper-management-cluster"
	shipperManagementClusterRoleName           = "shipper:management-cluster"
	shipperManagementClusterRoleBindingName    = "shipper:management-cluster"

	shipperApplicationClusterServiceAccountName = "shipper-application-cluster"
	shipperApplicationClusterRoleName           = "cluster-admin" // needs to be able to install any kind of Helm chart
	shipperApplicationClusterRoleBindingName    = "shipper:application-cluster"
)

func init() {
	fileFlagName := "file"
	kubeConfigFlagName := "kube-config"
	applyCmd.Flags().StringVarP(&configFile, fileFlagName, "f", "clusters.yaml", "config file")
	applyCmd.Flags().StringVar(&kubeConfigFile, kubeConfigFlagName, "~/.kube/config", "the path to the Kubernetes configuration file")
	applyCmd.Flags().StringVarP(&shipperSystemNamespace, "shipper-system-namespace", "n", shipper.ShipperNamespace, "the namespace where Shipper is running")
	applyCmd.Flags().StringVarP(&globalRolloutBlockNamespace, "rollout-blocks-global-namespace", "g", shipper.GlobalRolloutBlockNamespace, "the namespace where Global RolloutBlocks are running")

	err := applyCmd.MarkFlagFilename(fileFlagName, "yaml")
	if err != nil {
		applyCmd.Printf("warning: could not mark %q for filename autocompletion: %s\n", fileFlagName, err)
	}
	err = applyCmd.MarkFlagFilename(kubeConfigFlagName, "yaml")
	if err != nil {
		applyCmd.Printf("warning: could not mark %q for filename autocompletion: %s\n", kubeConfigFlagName, err)
	}
}

func runApplyClustersCommand(cmd *cobra.Command, args []string) error {
	clustersConfiguration, err := loadClustersConfiguration()
	if err != nil {
		return err
	}

	if err := validateApplicationClusters(clustersConfiguration); err != nil {
		return err
	}

	for _, managementCluster := range clustersConfiguration.ManagementClusters {
		cmd.Printf("Setting up management cluster %s:\n", managementCluster.Name)
		if err := setupManagementCluster(managementCluster, cmd); err != nil {
			return err
		}
		cmd.Printf("Finished setting up cluster %s\n\n", managementCluster.Name)
	}

	for _, applicationCluster := range clustersConfiguration.ApplicationClusters {
		cmd.Printf("Setting up application cluster %s:\n", applicationCluster.Name)
		if err := setupApplicationCluster(applicationCluster, cmd); err != nil {
			return err
		}
		cmd.Printf("Finished setting up cluster %s\n\n", applicationCluster.Name)
	}

	// Go through both the management and application clusters and join them together
	for _, managementCluster := range clustersConfiguration.ManagementClusters {
		for _, applicationCluster := range clustersConfiguration.ApplicationClusters {
			cmd.Printf("Joining management cluster %s to application cluster %s:\n", managementCluster.Name, applicationCluster.Name)
			if err := joinClusters(managementCluster, applicationCluster, cmd); err != nil {
				return err
			}
			cmd.Printf("Finished joining cluster %s and %s together\n\n", managementCluster.Name, applicationCluster.Name)
		}
	}

	cmd.Println("Cluster configuration applied successfully!")
	return nil
}

func validateApplicationClusters(configuration *config.ClustersConfiguration) error {
	for _, cluster := range configuration.ApplicationClusters {
		if msgs := validation.IsDNS1123Subdomain(cluster.Name); len(msgs) > 0 {
			return fmt.Errorf("%q is not a valid cluster name in Kubernetes. If this is the name of a context in your Kubernetes configuration, check out the relevant example at https://docs.shipper-k8s.io/en/latest/operations/shipperctl.html#using-google-kubernetes-engine-gke-context-names", cluster.Name)
		}

		if cluster.Region == "" {
			return fmt.Errorf("you must specify region for cluster %s", cluster.Name)
		}
	}

	return nil
}

func setupManagementCluster(managementCluster *config.ClusterConfiguration, cmd *cobra.Command) error {
	configurator, err := configurator.NewClusterConfigurator(managementCluster, kubeConfigFile)
	if err != nil {
		return err
	}

	if err := createOrUpdateCrds(cmd, configurator); err != nil {
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

func setupApplicationCluster(applicationCluster *config.ClusterConfiguration, cmd *cobra.Command) error {
	configurator, err := configurator.NewClusterConfigurator(applicationCluster, kubeConfigFile)
	if err != nil {
		return err
	}

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

func joinClusters(managementCluster, applicationCluster *config.ClusterConfiguration, cmd *cobra.Command) error {
	managementClusterConfigurator, err := configurator.NewClusterConfigurator(managementCluster, kubeConfigFile)
	if err != nil {
		return err
	}

	applicationClusterConfigurator, err := configurator.NewClusterConfigurator(applicationCluster, kubeConfigFile)
	if err != nil {
		return err
	}

	if applicationCluster.APIMaster == "" {
		applicationCluster.APIMaster = applicationClusterConfigurator.Host
	}

	if err := createApplicationClusterObjectOnManagementCluster(cmd, managementClusterConfigurator, applicationCluster); err != nil {
		return err
	}

	if err := copySecretFromApplicationToManagementCluster(cmd, applicationClusterConfigurator, managementClusterConfigurator, applicationCluster.Name); err != nil {
		return err
	}

	return nil
}

func createOrUpdateCrds(cmd *cobra.Command, configurator *configurator.Cluster) error {
	cmd.Print("Registering or updating custom resource definitions... ")
	if err := configurator.CreateOrUpdateCRD(crds.Application); err != nil {
		return err
	}

	if err := configurator.CreateOrUpdateCRD(crds.Release); err != nil {
		return err
	}

	if err := configurator.CreateOrUpdateCRD(crds.InstallationTarget); err != nil {
		return err
	}

	if err := configurator.CreateOrUpdateCRD(crds.CapacityTarget); err != nil {
		return err
	}

	if err := configurator.CreateOrUpdateCRD(crds.TrafficTarget); err != nil {
		return err
	}

	if err := configurator.CreateOrUpdateCRD(crds.Cluster); err != nil {
		return err
	}

	if err := configurator.CreateOrUpdateCRD(crds.RolloutBlock); err != nil {
		return err
	}

	cmd.Println("done")

	return nil
}

func createNamespace(cmd *cobra.Command, configurator *configurator.Cluster) error {
	cmd.Printf("Creating a namespace called %s... ", shipperSystemNamespace)
	if err := configurator.CreateNamespace(shipperSystemNamespace); err != nil {
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
	cmd.Printf("Creating a service account called %s... ", shipperManagementClusterServiceAccountName)

	err := configurator.CreateServiceAccount(
		shipper.RBACManagementDomain,
		shipperSystemNamespace,
		shipperManagementClusterServiceAccountName,
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
	cmd.Printf("Checking if a secret already exists for the validating webhook in the %s namespace... ", shipperSystemNamespace)

	exists, err := configurator.ValidatingWebhookSecretExists(shipperSystemNamespace)
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
	csr, err := tls.GenerateCSRForServiceInNamespace(privatekey, shipperManagementServiceName, shipperSystemNamespace)
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

	cmd.Printf("%sCreating the Secret using the private key and certificate in the %s namespace... ", level1Padding, shipperSystemNamespace)
	if err := configurator.CreateValidatingWebhookSecret(privatekeyPEM, certificate, shipperSystemNamespace); err != nil {
		return err
	}
	cmd.Println("done")

	return nil
}

func createValidatingWebhookConfiguration(cmd *cobra.Command, configurator *configurator.Cluster) error {
	cmd.Printf("Creating the ValidatingWebhookConfiguration in %s namespace... ", shipperSystemNamespace)
	caBundle, err := configurator.FetchKubernetesCABundle()
	if err != nil {
		return err
	}

	if err := configurator.CreateValidatingWebhookConfiguration(caBundle, shipperSystemNamespace); err != nil {
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
	if err := configurator.CreateValidatingWebhookService(shipperSystemNamespace); err != nil {
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
	cmd.Printf("Creating a service account called %s... ", shipperApplicationClusterServiceAccountName)

	err := configurator.CreateServiceAccount(
		shipper.RBACApplicationDomain,
		shipperSystemNamespace,
		shipperApplicationClusterServiceAccountName,
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
	cmd.Printf("Creating a ClusterRole called %s... ", shipperManagementClusterRoleName)
	if err := configurator.CreateClusterRole(shipper.RBACManagementDomain, shipperManagementClusterRoleName); err != nil {
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
	cmd.Printf("Creating a ClusterRoleBinding called %s... ", shipperManagementClusterRoleBindingName)
	err := configurator.CreateClusterRoleBinding(
		shipper.RBACManagementDomain,
		shipperManagementClusterRoleBindingName,
		shipperManagementClusterRoleName,
		shipperManagementClusterServiceAccountName,
		shipperSystemNamespace,
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
	cmd.Printf("Creating a ClusterRoleBinding called %s... ", shipperApplicationClusterRoleBindingName)
	err := configurator.CreateClusterRoleBinding(
		shipper.RBACApplicationDomain,
		shipperApplicationClusterRoleBindingName,
		shipperApplicationClusterRoleName,
		shipperApplicationClusterServiceAccountName,
		shipperSystemNamespace,
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

func copySecretFromApplicationToManagementCluster(cmd *cobra.Command, applicationClusterConfigurator, managementClusterConfigurator *configurator.Cluster, applicationClusterName string) error {
	// The workflow for this function is different to generate
	// more readable output
	cmd.Printf("Checking whether a secret for the %s cluster exists in the %s namespace... ", applicationClusterName, shipperSystemNamespace)
	shouldCopySecret, err := managementClusterConfigurator.ShouldCopySecret(applicationClusterName, shipperSystemNamespace)
	if err != nil {
		return err
	}

	if shouldCopySecret {
		cmd.Printf("no. Fetching secret for service account %s from the %s cluster... ", shipperApplicationClusterServiceAccountName, applicationClusterName)

		// Poll the server until the service account's
		// `secrets` field is populated.
		var secret *corev1.Secret
		for {
			secret, err = applicationClusterConfigurator.FetchSecretForServiceAccount(shipperApplicationClusterServiceAccountName, shipperSystemNamespace)
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

		applicationClusterObject, err := managementClusterConfigurator.FetchCluster(applicationClusterName)
		if err != nil {
			return err
		}

		cmd.Print("Copying the secret to the management cluster... ")
		if err := managementClusterConfigurator.CopySecret(applicationClusterObject, shipperSystemNamespace, secret); err != nil {
			return err
		}
		cmd.Println("done")
	} else {
		cmd.Println("yes. Skipping")
	}

	return nil
}

func createApplicationClusterObjectOnManagementCluster(cmd *cobra.Command, managementClusterConfigurator *configurator.Cluster, applicationCluster *config.ClusterConfiguration) error {
	cmd.Printf("Creating or updating the cluster object for cluster %s on the management cluster... ", applicationCluster.Name)

	// Initialize the map of capabilities if it's null so that we
	// don't fire an error when creating it
	if applicationCluster.Capabilities == nil {
		applicationCluster.Capabilities = []string{}
	}
	if err := managementClusterConfigurator.CreateOrUpdateClusterWithConfig(applicationCluster); err != nil {
		return err
	}
	cmd.Println("done")

	return nil
}

func loadClustersConfiguration() (*config.ClustersConfiguration, error) {
	configBytes, err := ioutil.ReadFile(configFile)
	if err != nil {
		return nil, err
	}

	configuration := &config.ClustersConfiguration{}
	err = yaml.Unmarshal(configBytes, configuration)
	if err != nil {
		return nil, err
	}

	return configuration, nil
}
