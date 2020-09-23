package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/bookingcom/shipper/cmd/shipperctl/configurator"
	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	apputil "github.com/bookingcom/shipper/pkg/util/application"
	"github.com/bookingcom/shipper/pkg/util/filters"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

const (
	decommissionedClustersFlagName = "decommissioned-clusters"
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

	namespaceList, err := kubeClient.CoreV1().Namespaces().List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	var errList []string
	for _, ns := range namespaceList.Items {
		releaseList, err := shipperClient.ShipperV1alpha1().Releases(ns.Name).List(metav1.ListOptions{})
		if err != nil {
			errList = append(errList, err.Error())
			continue
		}
		for _, rel := range releaseList.Items {
			filteredClusters := getFilteredSelectedClusters(&rel)
			if len(filteredClusters) > 0 {
				sort.Strings(filteredClusters)

				if strings.Join(filteredClusters, ",") == rel.Annotations[shipper.ReleaseClustersAnnotation] {
					continue
				}
				rel.Annotations[shipper.ReleaseClustersAnnotation] = strings.Join(filteredClusters, ",")
				cmd.Printf("Editing annotations of release %s/%s to %s...", rel.Namespace, rel.Name, rel.Annotations[shipper.ReleaseClustersAnnotation])
				if !dryrun {
					_, err := shipperClient.ShipperV1alpha1().Releases(ns.Name).Update(&rel)
					if err != nil {
						errList = append(errList, err.Error())
					}
					cmd.Println("done")
				} else {
					cmd.Println("dryrun")
				}
				continue
			}
			isContender, err := isContender(&rel, shipperClient)
			if err != nil {
				errList = append(errList, err.Error())
				continue
			}
			if len(filteredClusters) == 0 && !isContender {
				cmd.Printf("Deleting release %s/%s...", rel.Namespace, rel.Name)
				if !dryrun {
					err := shipperClient.ShipperV1alpha1().Releases(ns.Name).Delete(rel.Name, &metav1.DeleteOptions{})
					if err != nil {
						errList = append(errList, err.Error())
					}
					cmd.Println("done")
				} else {
					cmd.Println("dryrun")
				}
			}
		}
	}

	if len(errList) > 0 {
		return fmt.Errorf(strings.Join(errList, ","))
	}
	return nil
}

func getFilteredSelectedClusters(rel *shipper.Release) []string {
	selectedClusters := releaseutil.GetSelectedClusters(rel)
	var filteredClusters []string
	for _, selectedCluster := range selectedClusters {
		if !filters.SliceContainsString(clusters, selectedCluster) {
			filteredClusters = append(filteredClusters, selectedCluster)
		}
	}
	return filteredClusters
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
