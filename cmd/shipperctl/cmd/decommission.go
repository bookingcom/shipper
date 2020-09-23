package cmd

import (
	"bufio"
	"fmt"
	"io"
	"k8s.io/cli-runtime/pkg/printers"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tatsushid/go-prettytable"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	//"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/client-go/kubernetes"

	"github.com/bookingcom/shipper/cmd/shipperctl/configurator"
	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	apputil "github.com/bookingcom/shipper/pkg/util/application"
	"github.com/bookingcom/shipper/pkg/util/filters"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

const (
	clustersFlagName = "decommissionedClusters"
	retries          = 10
)

var (
	decommissionedClusters []string
	dryrun                 bool

	CleanCmd = &cobra.Command{
		Use:   "clean",
		Short: "clean Shipper objects",
	}

	cleanDeadClustersCmd = &cobra.Command{
		Use:   "decommissioned-clusters",
		Short: "clean Shipper releases from decommissioned clusters",
		Long: "deleting releases that are scheduled *only* on decommissioned clusters and are not contenders, " +
			"removing decommissioned clusters from annotations of releases that are scheduled partially on decommissioned clusters.",
		RunE: runCleanCommand,
	}
)

func init() {
	// Flags common to all commands under `shipperctl clean`
	CleanCmd.PersistentFlags().StringVar(&kubeConfigFile, kubeConfigFlagName, "~/.kube/config", "The path to the Kubernetes configuration file")
	if err := CleanCmd.MarkPersistentFlagFilename(kubeConfigFlagName, "yaml"); err != nil {
		CleanCmd.Printf("warning: could not mark %q for filename autocompletion: %s\n", kubeConfigFlagName, err)
	}

	CleanCmd.PersistentFlags().BoolVar(&dryrun, "dryrun", false, "If true, only prints the objects that will be modified/deleted")
	CleanCmd.PersistentFlags().StringVar(&managementClusterContext, "management-cluster-context", "", "The name of the context to use to communicate with the management cluster. defaults to the current one")
	CleanCmd.PersistentFlags().StringSliceVar(&decommissionedClusters, clustersFlagName, decommissionedClusters, "List of decommissioned clusters. (Required)")
	if err := CleanCmd.MarkPersistentFlagRequired(clustersFlagName); err != nil {
		CleanCmd.Printf("warning: could not mark %q as required: %s\n", clustersFlagName, err)
	}

	CleanCmd.AddCommand(cleanDeadClustersCmd)
}

type ReleaseAndFilteredAnnotations struct {
	OldClusterAnnotation      string
	FilteredClusterAnnotation string
	Namespace                 string
	Name                      string
}

func runCleanCommand(cmd *cobra.Command, args []string) error {
	kubeClient, err := configurator.NewKubeClientFromKubeConfig(kubeConfigFile, managementClusterContext)
	if err != nil {
		return err
	}

	shipperClient, err := configurator.NewShipperClientFromKubeConfig(kubeConfigFile, managementClusterContext)
	if err != nil {
		return err
	}

	deleteReleases, updateAnnotations, err := collectReleases(kubeClient, shipperClient)
	if err != nil {
		return err
	}

	err = reviewActions(cmd, deleteReleases, updateAnnotations)
	if err != nil {
		return err
	}

	var errList []string
	if !dryrun {
		// do it
	}
	if len(errList) > 0 {
		return fmt.Errorf(strings.Join(errList, ","))
	}
	return nil
}

func reviewActions(cmd *cobra.Command, deleteReleases []ReleaseAndFilteredAnnotations, updateAnnotations []ReleaseAndFilteredAnnotations) error {
	cmd.Printf("About to edit %d releases and and delete %d releases\n", len(updateAnnotations), len(deleteReleases))
	confirm, err := askForConfirmation(os.Stdin, "Would you like to see the releases? (This will not start the process)", retries)
	if err != nil {
		return err
	}
	if confirm {
		tt := &metav1.Table{
			TypeMeta: metav1.TypeMeta{},
			ListMeta: metav1.ListMeta{},
			ColumnDefinitions: []metav1.TableColumnDefinition{
				{
					Name: "Namespace",
					Type: "string",
				},
				{
					Name: "Name",
					Type: "string",
				},

				{
					Name: "Action Taken",
					Type: "string",
				},

				{
					Name: "Old Clusters Annotations",
					Type: "string",
				},

				{
					Name: "New Clusters Annotations",
					Type: "string",
				},
			},
			Rows: nil,
		}
		//tbl, err := prettytable.NewTable([]prettytable.Column{
		//	{Header: "NAMESPACE"},
		//	{Header: "NAME"},
		//	{Header: "ACTION TAKEN"},
		//	{Header: "OLD CLUSTER ANNOTATIONS"},
		//	{Header: "NEW CLUSTER ANNOTATIONS"},
		//}...)
		//if err != nil {
		//	return err
		//}
		for _, release := range deleteReleases {
			tt.Rows = append(tt.Rows, metav1.TableRow{
				Cells: []interface{}{
					release.Namespace,
					release.Name,
					" DELETE ",
					release.OldClusterAnnotation,
					release.FilteredClusterAnnotation,
				},
			})
			//tbl.AddRow(
			//	release.Namespace,
			//	release.Name,
			//	" DELETE ",
			//	release.OldClusterAnnotation,
			//	release.FilteredClusterAnnotation,
			//)
		}
		for _, release := range updateAnnotations {
			tt.Rows = append(tt.Rows, metav1.TableRow{
				Cells: []interface{}{
					release.Namespace,
					release.Name,
					"UPDATE",
					release.OldClusterAnnotation,
					release.FilteredClusterAnnotation,
				},
			})
			//tbl.AddRow(
			//	release.Namespace,
			//	release.Name,
			//	"UPDATE",
			//	release.OldClusterAnnotation,
			//	release.FilteredClusterAnnotation,
			//)
		}
		//tbl.Print()
		//printer := printers.NewTablePrinter(printers.PrintOptions{})
		//printer.PrintObj(tt, cmd.OutOrStdout())
		//cmd.Println()
	}
	return nil
}

func reviewModifiedAnnotations(cmd *cobra.Command, updateAnnotations []ReleaseAndFilteredAnnotations) error {
	if len(updateAnnotations) == 0 {
		return nil
	}
	cmd.Printf("About to edit these %d releases:\n", len(updateAnnotations))
	confirm, err := askForConfirmation(os.Stdin, "Would you like to see the releases? (This will not start the edition process)", retries)
	if err != nil {
		return err
	}
	if confirm {
		tbl, err := prettytable.NewTable([]prettytable.Column{
			{Header: "NAMESPACE"},
			{Header: "NAME"},
			{Header: "OLD CLUSTER ANNOTATIONS"},
			{Header: "NEW CLUSTER ANNOTATIONS"},
			{Header: "WILL BE DELETED"},
		}...)
		if err != nil {
			return err
		}
		for _, release := range updateAnnotations {
			tbl.AddRow(release.Namespace, release.Name, release.OldClusterAnnotation, release.FilteredClusterAnnotation)
		}
		tbl.Print()
		cmd.Println()
	}
	return nil
}

func reviewDeletedReleases(cmd *cobra.Command, deleteReleases []OutputRelease) error {
	if len(deleteReleases) == 0 {
		return nil
	}
	cmd.Printf("About to delete these %d releases:\n", len(deleteReleases))
	confirm, err := askForConfirmation(os.Stdin, "Would you like to see the releases? (This will not start the deletion process)", retries)
	if err != nil {
		return err
	}
	if confirm {
		tbl, err := prettytable.NewTable([]prettytable.Column{
			{Header: "NAMESPACE"},
			{Header: "NAME"},
		}...)
		if err != nil {
			return err
		}
		for _, release := range deleteReleases {
			tbl.AddRow(release.Namespace, release.Name)
		}
		tbl.Print()
		cmd.Println()
	}
	return nil
}

func collectReleases(kubeClient kubernetes.Interface, shipperClient shipperclientset.Interface) ([]ReleaseAndFilteredAnnotations, []ReleaseAndFilteredAnnotations, error) {
	var deleteReleases []ReleaseAndFilteredAnnotations
	var updateAnnotations []ReleaseAndFilteredAnnotations
	namespaceList, err := kubeClient.CoreV1().Namespaces().List(metav1.ListOptions{})
	if err != nil {
		return nil, nil, err
	}
	var errList []string
	for _, ns := range namespaceList.Items {
		releaseList, err := shipperClient.ShipperV1alpha1().Releases(ns.Name).List(metav1.ListOptions{})
		if err != nil {
			errList = append(errList, err.Error())
			continue
		}
		for _, rel := range releaseList.Items {
			selectedClusters := releaseutil.GetSelectedClusters(&rel)
			trueClusters := getFilteredScheduledClusters(selectedClusters, decommissionedClusters)
			if len(trueClusters) > 0 {
				sort.Strings(trueClusters)

				filteredClusterAnnotation := strings.Join(trueClusters, ",")
				if filteredClusterAnnotation == rel.Annotations[shipper.ReleaseClustersAnnotation] {
					continue
				}
				rel.Annotations[shipper.ReleaseClustersAnnotation] = filteredClusterAnnotation
				updateAnnotations = append(
					updateAnnotations,
					ReleaseAndFilteredAnnotations{
						OldClusterAnnotation:      strings.Join(selectedClusters, ","),
						FilteredClusterAnnotation: filteredClusterAnnotation,
						Namespace:                 rel.Namespace,
						Name:                      rel.Name,
					})
				continue
			}
			isContender, err := isContender(&rel, shipperClient)
			if err != nil {
				errList = append(errList, err.Error())
				continue
			}
			if len(trueClusters) == 0 && !isContender {
				outputRelease := ReleaseAndFilteredAnnotations{
					OldClusterAnnotation:      strings.Join(selectedClusters, ","),
					FilteredClusterAnnotation: "",
					Namespace:                 rel.Namespace,
					Name:                      rel.Name,
				}
				deleteReleases = append(deleteReleases, outputRelease)
			}
		}
	}
	return deleteReleases, updateAnnotations, nil
}

func getFilteredScheduledClusters(scheduledClusters, decommissionedClusters []string) []string {
	var trueClusters []string
	for _, cluster := range scheduledClusters {
		if !filters.SliceContainsString(decommissionedClusters, cluster) {
			trueClusters = append(trueClusters, cluster)
		}
	}
	return trueClusters
}

func isContender(rel *shipper.Release, shipperClient shipperclientset.Interface) (bool, error) {
	appName := rel.Labels[shipper.AppLabel]
	app, err := shipperClient.ShipperV1alpha1().Applications(rel.Namespace).Get(appName, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	contender, err := getContender(app, shipperClient)
	if err != nil {
		return false, err
	}
	return contender.Name == rel.Name && contender.Namespace == rel.Namespace, nil
}

func getContender(app *shipper.Application, shipperClient shipperclientset.Interface) (*shipper.Release, error) {
	appName := app.Name
	selector := labels.Set{shipper.AppLabel: appName}.AsSelector()
	releaseList, err := shipperClient.ShipperV1alpha1().Releases(app.Namespace).List(metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, err
	}
	rels := make([]*shipper.Release, len(releaseList.Items))
	for i, _ := range releaseList.Items {
		rels[i] = &releaseList.Items[i]
	}
	rels = releaseutil.SortByGenerationDescending(rels)
	contender, err := apputil.GetContender(appName, rels)
	if err != nil {
		return nil, err
	}
	return contender, nil
}

// askForConfirmation asks the user for confirmation. A user must type in "y" or "n" and
// then press enter. It has fuzzy matching, so "y", "Y" both count as
// confirmations. If the input is not recognized, it will ask again.
// It accepts an int `tries` representing the number of attempts before returning false
// taken from https://gist.github.com/r0l1/3dcbb0c8f6cfe9c66ab8008f55f8f28b
func askForConfirmation(stdin io.Reader, message string, tries int) (bool, error) {
	reader := bufio.NewReader(stdin)

	for ; tries > 0; tries-- {
		fmt.Printf("%s [y/n]: ", message)

		response, err := reader.ReadString('\n')
		if err != nil {
			return false, err
		}

		response = strings.ToLower(strings.TrimSpace(response))

		if response == "y" {
			return true, nil
		} else if response == "n" {
			return false, nil
		}
	}
	return false, fmt.Errorf("accepts only [y/n]")
}
