package backup

import (
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/bookingcom/shipper/cmd/shipperctl/configurator"
	"github.com/bookingcom/shipper/cmd/shipperctl/release"
	"github.com/bookingcom/shipper/cmd/shipperctl/ui"
)

var (
	prepareBackupCmd = &cobra.Command{
		Use:   "prepare",
		Short: "Get yaml of application and release objects prepared for backup",
		Long: "Get yaml of application and release objects prepared for backup" +
			"where every application will be bundled with it's releases",
		RunE: runPrepareCommand,
	}
)

func init() {
	BackupCmd.AddCommand(prepareBackupCmd)
}

func runPrepareCommand(cmd *cobra.Command, args []string) error {
	kubeClient, err := configurator.NewKubeClientFromKubeConfig(kubeConfigFile, managementClusterContext)
	if err != nil {
		return err
	}

	shipperClient, err := configurator.NewShipperClientFromKubeConfig(kubeConfigFile, managementClusterContext)
	if err != nil {
		return err
	}

	namespaceList, err := kubeClient.CoreV1().Namespaces().List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	var errList []string

	releasesPerApplications := []shipperBackupObject{}
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
			releasesPerApplications = append(
				releasesPerApplications,
				shipperBackupObject{
					Application: app,
					Releases:    releaseList.Items,
				},
			)
		}
	}
	if len(errList) > 0 {
		confirm, err := ui.AskForConfirmation(
			cmd.InOrStdin(),
			fmt.Sprintf(
				"Found errors while retreiving objects: %s. Continue anyway?",
				strings.Join(errList, ", "),
			),
		)
		if err != nil {
			return err
		}
		if !confirm {
			return fmt.Errorf(strings.Join(errList, ", "))
		}
	}

	if verboseFlag {
		printReleasesPerApplications(releasesPerApplications)
	}
	data, err := marshalReleasesPerApplications(releasesPerApplications)
	if err != nil {
		return err
	} else {
		err := ioutil.WriteFile(backupFile, data, 0644)
		if err != nil {
			return err
		}
	}

	fmt.Printf("Backup objects stored in %q\n", backupFile)
	return nil
}
