package backup

import (
	"encoding/json"
	"os"

	"github.com/rodaine/table"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/bookingcom/shipper/cmd/shipperctl/config"
	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

var (
	outputFormat             = "yaml"
	kubeConfigFile           string
	managementClusterContext string
	verboseFlag              bool

	Cmd = &cobra.Command{
		Use:   "backup",
		Short: "Backup and restore Shipper applications and releases",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			switch outputFormat {
			case "json", "yaml":
				return
			default:
				cmd.Printf("error: output format %q not supported, allowed formats are: json, yaml\n", outputFormat)
				os.Exit(1)
			}
		},
	}
)

type shipperBackupApplication struct {
	Application    shipper.Application    `json:"application"`
	BackupReleases []shipperBackupRelease `json:"backup_releases"`
}

type shipperBackupRelease struct {
	Release            shipper.Release            `json:"release"`
	InstallationTarget shipper.InstallationTarget `json:"installation_target"`
	TrafficTarget      shipper.TrafficTarget      `json:"traffic_target"`
	CapacityTarget     shipper.CapacityTarget     `json:"capacity_target"`
}

func init() {
	kubeConfigFlagName := "kubeconfig"
	fileFlagName := "file"

	config.RegisterFlag(Cmd.PersistentFlags(), &kubeConfigFile)
	if err := Cmd.MarkPersistentFlagFilename(kubeConfigFlagName, "yaml"); err != nil {
		Cmd.Printf("warning: could not mark %q for filename autocompletion: %s\n", kubeConfigFlagName, err)
	}

	Cmd.PersistentFlags().StringVar(&managementClusterContext, "management-cluster-context", "", "The name of the context to use to communicate with the management cluster. defaults to the current one")
	Cmd.PersistentFlags().BoolVarP(&verboseFlag, "verbose", "v", false, "Prints the list of backup items")
	Cmd.PersistentFlags().StringVar(&outputFormat, "format", "yaml", "Output format. One of: json|yaml")

	Cmd.PersistentFlags().StringVarP(&backupFile, fileFlagName, "f", "backup.yaml", "The path to a backup file")
	err := Cmd.MarkPersistentFlagFilename(fileFlagName, "yaml")
	if err != nil {
		Cmd.Printf("warning: could not mark %q for filename yaml autocompletion: %s\n", fileFlagName, err)
	}
	err = Cmd.MarkPersistentFlagFilename(fileFlagName, "json")
	if err != nil {
		Cmd.Printf("warning: could not mark %q for filename json autocompletion: %s\n", fileFlagName, err)
	}
}

func marshalShipperBackupApplication(shipperBackupApplication []shipperBackupApplication) ([]byte, error) {
	if outputFormat == "json" {
		return json.MarshalIndent(shipperBackupApplication, "", "    ")
	}
	return yaml.Marshal(shipperBackupApplication)
}

func printShipperBackupApplication(shipperBackupApplication []shipperBackupApplication) {
	tbl := table.New(
		"NAMESPACE",
		"RELEASE NAME",
		"OWNING APPLICATION",
	)
	for _, obj := range shipperBackupApplication {
		for _, backupRelease := range obj.BackupReleases {
			rel := backupRelease.Release
			tbl.AddRow(
				rel.Namespace,
				rel.Name,
				obj.Application.Name,
			)
		}
	}

	tbl.Print()
}
