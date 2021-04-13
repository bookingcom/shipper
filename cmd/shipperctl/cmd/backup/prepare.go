package backup

import (
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/bookingcom/shipper/cmd/shipperctl/config"
	"github.com/bookingcom/shipper/cmd/shipperctl/configurator"
	"github.com/bookingcom/shipper/cmd/shipperctl/release"
	"github.com/bookingcom/shipper/cmd/shipperctl/ui"
	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
)

var (
	useInClusterConfigFlag bool

	prepareBackupCmd = &cobra.Command{
		Use:   "prepare",
		Short: "Get yaml of application and release objects prepared for backup",
		Long: "Get yaml of application and release objects prepared for backup" +
			"where every application will be bundled with it's releases",
		RunE: runPrepareCommand,
	}
)

func init() {
	prepareBackupCmd.Flags().BoolVar(&useInClusterConfigFlag, "use-in-cluster-config",false, "Use this when running inside a pod running on kubernetes. It will use config object which uses the service account kubernetes gives to pods.")
	Cmd.AddCommand(prepareBackupCmd)
}

func runPrepareCommand(cmd *cobra.Command, args []string) error {
	var (
		kubeClient kubernetes.Interface
		shipperClient shipperclientset.Interface
		err error
	)

	if useInClusterConfigFlag {
		clusterConfig, err := rest.InClusterConfig()
		if err != nil {
			return err
		}
		clusterConfigurator, err := configurator.NewClusterConfigurator(clusterConfig)
		if err != nil {
			return err
		}
		kubeClient = clusterConfigurator.KubeClient
		shipperClient = clusterConfigurator.ShipperClient
	} else {
		kubeClient, shipperClient, err = config.Load(kubeConfigFile, managementClusterContext)
		if err != nil {
			return err
		}
	}

	shipperBackupApplications, err := buildShipperBackupApplication(kubeClient, shipperClient)
	if err != nil {
		confirm, err := ui.AskForConfirmation(
			cmd.InOrStdin(),
			fmt.Sprintf(
				"%s\nContinue anyway?",
				err.Error(),
			),
		)
		if err != nil {
			return err
		}
		if !confirm {
			return fmt.Errorf(err.Error())
		}
	}

	if verboseFlag {
		printShipperBackupApplication(shipperBackupApplications)
	}
	data, err := marshalShipperBackupApplication(shipperBackupApplications)
	if err != nil {
		return err
	}
	if err := ioutil.WriteFile(backupFile, data, 0644); err != nil {
		return err
	}

	cmd.Printf("Backup objects stored in %q\n", backupFile)
	return nil
}

func buildShipperBackupApplication(kubeClient kubernetes.Interface, shipperClient shipperclientset.Interface) ([]shipperBackupApplication, error) {
	namespaceList, err := kubeClient.CoreV1().Namespaces().List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var errList []string

	shipperBackupApplications := []shipperBackupApplication{}
	for _, ns := range namespaceList.Items {
		applicationList, err := shipperClient.ShipperV1alpha1().Applications(ns.Name).List(metav1.ListOptions{})
		if err != nil {
			errList = append(errList, err.Error())
			continue
		}

		for _, app := range applicationList.Items {
			releaseList, err := release.ReleasesForApplication(app.Name, app.Namespace, shipperClient)
			if err != nil {
				errList = append(errList, err.Error())
				continue
			}
			backupReleases := []shipperBackupRelease{}
			for _, rel := range releaseList.Items {
				it, tt, ct, err := release.TargetObjectsForRelease(rel.Name, rel.Namespace, shipperClient)
				if err != nil {
					errList = append(errList, err.Error())
					continue
				}
				backupReleases = append(backupReleases, shipperBackupRelease{
					Release:            rel,
					InstallationTarget: *it,
					TrafficTarget:      *tt,
					CapacityTarget:     *ct,
				})
			}
			shipperBackupApplications = append(
				shipperBackupApplications,
				shipperBackupApplication{
					Application:    app,
					BackupReleases: backupReleases,
				},
			)
		}
	}
	if len(errList) > 0 {
		return nil, fmt.Errorf("Failed to retrieve some objects:\n - %s\n", strings.Join(errList, "\n - "))
	}
	return shipperBackupApplications, nil
}
