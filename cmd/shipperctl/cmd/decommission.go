package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/rodaine/table"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/bookingcom/shipper/cmd/shipperctl/configurator"
	"github.com/bookingcom/shipper/cmd/shipperctl/release"
	"github.com/bookingcom/shipper/cmd/shipperctl/ui"
	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
)

const (
	decommissionedClustersFlagName = "decommissionedClusters"
)

var (
	clusters []string
	dryrun   bool

	CleanCmd = &cobra.Command{
		Use:   "clean",
		Short: "clean Shipper objects",
	}

	cleanDeadClustersCmd = &cobra.Command{
		Use:   "decommissioned-clusters",
		Short: "clean Shipper releases from decommissioned clusters",
		Long: "removing decommissioned clusters from annotations of releases that are " +
			"scheduled on decommissioned clusters and are not contenders or incumbents.",
		RunE: runCleanCommand,
	}
)

type ReleaseAndFilteredAnnotations struct {
	OldClusterAnnotation      string
	FilteredClusterAnnotation string
	Namespace                 string
	Name                      string
}

func init() {
	// Flags common to all commands under `shipperctl clean`
	CleanCmd.PersistentFlags().StringVar(&kubeConfigFile, kubeConfigFlagName, "~/.kube/config", "The path to the Kubernetes configuration file")
	if err := CleanCmd.MarkPersistentFlagFilename(kubeConfigFlagName, "yaml"); err != nil {
		CleanCmd.Printf("warning: could not mark %q for filename autocompletion: %s\n", kubeConfigFlagName, err)
	}

	CleanCmd.PersistentFlags().BoolVar(&dryrun, "dryrun", false, "If true, only prints the objects that will be modified/deleted")
	CleanCmd.PersistentFlags().StringVar(&managementClusterContext, "management-cluster-context", "", "The name of the context to use to communicate with the management cluster. defaults to the current one")
	CleanCmd.PersistentFlags().StringSliceVar(&clusters, decommissionedClustersFlagName, clusters, "List of decommissioned clusters. (Required)")
	if err := CleanCmd.MarkPersistentFlagRequired(decommissionedClustersFlagName); err != nil {
		CleanCmd.Printf("warning: could not mark %q as required: %s\n", decommissionedClustersFlagName, err)
	}

	CleanCmd.AddCommand(cleanDeadClustersCmd)
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

	releasesToUpdate, err := collectReleases(kubeClient, shipperClient)
	if err != nil {
		return err
	}

	if err := reviewActions(cmd, releasesToUpdate); err != nil {
		return err
	}

	var errList []string
	if !dryrun {
		if err := updateReleases(cmd, shipperClient, releasesToUpdate); err != nil {
			errList = append(errList, err.Error())
		}
	}

	if len(errList) > 0 {
		return fmt.Errorf(strings.Join(errList, ", "))
	}

	return nil
}

func updateReleases(cmd *cobra.Command, shipperClient shipperclientset.Interface, releasesToUpdate []ReleaseAndFilteredAnnotations) error {
	if len(releasesToUpdate) == 0 {
		return nil
	}

	confirm, err := ui.AskForConfirmation(os.Stdin, fmt.Sprintf("This will update %d releases. Are you sure?", len(releasesToUpdate)))
	if err != nil {
		return err
	}
	if !confirm {
		return nil
	}

	var errList []string
	for _, rel := range releasesToUpdate {
		relObject, err := shipperClient.ShipperV1alpha1().Releases(rel.Namespace).Get(rel.Name, metav1.GetOptions{})
		if err != nil {
			errList = append(errList, fmt.Sprintf("failed to get release: %s", err.Error()))
			continue
		}
		cmd.Printf(
			"Editing annotations of release %s/%s from %s to %s ...",
			rel.Namespace,
			rel.Name,
			relObject.Annotations[shipper.ReleaseClustersAnnotation],
			rel.FilteredClusterAnnotation,
		)
		relObject.Annotations[shipper.ReleaseClustersAnnotation] = rel.FilteredClusterAnnotation

		if _, err = shipperClient.ShipperV1alpha1().Releases(rel.Namespace).Update(relObject); err != nil {
			errList = append(errList, fmt.Sprintf("failed to update release: %s", err.Error()))
			cmd.Printf("errored: %s\n", err.Error())
			continue
		}
		cmd.Println("done")
	}

	if len(errList) > 0 {
		return fmt.Errorf(strings.Join(errList, ", "))
	}

	return nil
}

func collectReleases(kubeClient kubernetes.Interface, shipperClient shipperclientset.Interface) ([]ReleaseAndFilteredAnnotations, error) {
	var releasesToUpdate []ReleaseAndFilteredAnnotations
	namespaceList, err := kubeClient.CoreV1().Namespaces().List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var errList []string
	for _, ns := range namespaceList.Items {
		releaseList, err := shipperClient.ShipperV1alpha1().Releases(ns.Name).List(metav1.ListOptions{})
		if err != nil {
			errList = append(errList, err.Error())
			continue
		}
		for _, rel := range releaseList.Items {
			selectedClusters := strings.Split(rel.Annotations[shipper.ReleaseClustersAnnotation], ",")
			filteredClusters := release.FilterSelectedClusters(selectedClusters, clusters)
			if len(filteredClusters) > 0 {
				sort.Strings(filteredClusters)
				filteredClusterAnnotation := strings.Join(filteredClusters, ",")
				if filteredClusterAnnotation == rel.Annotations[shipper.ReleaseClustersAnnotation] {
					continue
				}
				releasesToUpdate = append(
					releasesToUpdate,
					ReleaseAndFilteredAnnotations{
						OldClusterAnnotation:      strings.Join(selectedClusters, ","),
						FilteredClusterAnnotation: filteredClusterAnnotation,
						Namespace:                 rel.Namespace,
						Name:                      rel.Name,
					})
				continue
			}
			isContender, err := release.IsContender(&rel, shipperClient)
			if err != nil {
				errList = append(errList, err.Error())
				continue
			}
			isIncumbent, err := release.IsIncumbent(&rel, shipperClient)
			if err != nil {
				errList = append(errList, err.Error())
				continue
			}
			// emptying the scheduled clusters annotation will make shipper re-schedule this release.
			// If this release is a history release, Shipper will enforce strategy correctly (no capacity and no traffic)
			// However, if this release is contender, Shipper will possibly give capacity and traffic to a release
			// that did not have running workload. This might cause an un monitored, unexpected and unwanted behaviour
			// from this release. So we are not touching contenders.
			// This applies for incumbents in an incomplete rollout.
			if len(filteredClusters) == 0 && !isContender && !isIncumbent {
				outputRelease := ReleaseAndFilteredAnnotations{
					OldClusterAnnotation:      strings.Join(selectedClusters, ","),
					FilteredClusterAnnotation: "",
					Namespace:                 rel.Namespace,
					Name:                      rel.Name,
				}
				releasesToUpdate = append(releasesToUpdate, outputRelease)
			}
		}
	}
	if len(errList) > 0 {
		return nil, fmt.Errorf(strings.Join(errList, ", "))
	}
	return releasesToUpdate, nil
}

func reviewActions(cmd *cobra.Command, releasesToUpdate []ReleaseAndFilteredAnnotations) error {
	cmd.Printf("About to edit %d releases\n", len(releasesToUpdate))
	confirm, err := ui.AskForConfirmation(os.Stdin, "Would you like to see the releases? (This will not edit anything)")
	if err != nil {
		return err
	}
	if confirm {
		tbl := table.New(
			"NAMESPACE",
			"NAME",
			"OLD CLUSTER ANNOTATIONS",
			"NEW CLUSTER ANNOTATIONS",
		)

		for _, release := range releasesToUpdate {
			tbl.AddRow(
				release.Namespace,
				release.Name,
				release.OldClusterAnnotation,
				release.FilteredClusterAnnotation,
			)
		}
		tbl.Print()
	}
	return nil
}
