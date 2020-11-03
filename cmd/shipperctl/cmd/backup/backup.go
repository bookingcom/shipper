package backup

import (
	"encoding/json"
	"os"

	"github.com/rodaine/table"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

var (
	outputFormat             = "yaml"
	kubeConfigFile           string
	managementClusterContext string
	verboseFlag              bool

	BackupCmd = &cobra.Command{
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

type shipperBackupObject struct {
	Application shipper.Application `json:"application"`
	Releases    []shipper.Release   `json:"releases"`
}

func init() {
	kubeConfigFlagName := "kubeconfig"
	fileFlagName := "file"

	BackupCmd.PersistentFlags().StringVar(&kubeConfigFile, kubeConfigFlagName, "~/.kube/config", "The path to the Kubernetes configuration file")
	if err := BackupCmd.MarkPersistentFlagFilename(kubeConfigFlagName, "yaml"); err != nil {
		BackupCmd.Printf("warning: could not mark %q for filename autocompletion: %s\n", kubeConfigFlagName, err)
	}

	BackupCmd.PersistentFlags().StringVar(&managementClusterContext, "management-cluster-context", "", "The name of the context to use to communicate with the management cluster. defaults to the current one")
	BackupCmd.PersistentFlags().BoolVarP(&verboseFlag, "verbose", "v", false, "Prints the list of backup items")
	BackupCmd.PersistentFlags().StringVar(&outputFormat, "format", "yaml", "Output format. One of: json|yaml")

	BackupCmd.PersistentFlags().StringVarP(&backupFile, fileFlagName, "f", "backup.yaml", "The path to a backup file")
	err := BackupCmd.MarkPersistentFlagFilename(fileFlagName, "yaml")
	if err != nil {
		BackupCmd.Printf("warning: could not mark %q for filename yaml autocompletion: %s\n", fileFlagName, err)
	}
	err = BackupCmd.MarkPersistentFlagFilename(fileFlagName, "json")
	if err != nil {
		BackupCmd.Printf("warning: could not mark %q for filename json autocompletion: %s\n", fileFlagName, err)
	}
}

func marshalReleasesPerApplications(releasesPerApplications []shipperBackupObject) ([]byte, error) {
	if outputFormat == "json" {
		return json.MarshalIndent(releasesPerApplications, "", "    ")
	}
	return yaml.Marshal(releasesPerApplications)
}

func printReleasesPerApplications(releasesPerApplications []shipperBackupObject) {
	tbl := table.New(
		"NAMESPACE",
		"RELEASE NAME",
		"OWNING APPLICATION",
	)
	for _, obj := range releasesPerApplications {
		for _, rel := range obj.Releases {

			tbl.AddRow(
				rel.Namespace,
				rel.Name,
				obj.Application.Name,
			)
		}
	}

	tbl.Print()
}
