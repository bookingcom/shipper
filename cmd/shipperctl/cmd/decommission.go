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
	apputil "github.com/bookingcom/shipper/pkg/util/application"
	"github.com/bookingcom/shipper/pkg/util/filters"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

const (
	clustersFlagName = "clusters"
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
	CleanCmd.PersistentFlags().StringSliceVar(&clusters, clustersFlagName, clusters, "List of decommissioned clusters. (Required)")
	if err := CleanCmd.MarkPersistentFlagRequired(clustersFlagName); err != nil {
		CleanCmd.Printf("warning: could not mark %q as required: %s\n", clustersFlagName, err)
	}

	CleanCmd.AddCommand(cleanDeadClustersCmd)
}

func runCleanCommand(cmd *cobra.Command, args []string) error {
	configurator, err := configurator.NewClusterConfiguratorFromKubeConfig(kubeConfigFile, managementClusterContext)
	if err != nil {
		return err
	}

	namespaceList, err := configurator.KubeClient.CoreV1().Namespaces().List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	var errList []string
	for _, ns := range namespaceList.Items {
		releaseList, err := configurator.ShipperClient.ShipperV1alpha1().Releases(ns.Name).List(metav1.ListOptions{})
		if err != nil {
			errList = append(errList, err.Error())
			continue
		}
		for _, rel := range releaseList.Items {
			trueClusters := getFilteredSelectedClusters(&rel)
			if len(trueClusters) > 0 {
				sort.Strings(trueClusters)

				if strings.Join(trueClusters, ",") == rel.Annotations[shipper.ReleaseClustersAnnotation] {
					continue
				}
				rel.Annotations[shipper.ReleaseClustersAnnotation] = strings.Join(trueClusters, ",")
				cmd.Printf("Editing annotations of release %s/%s to %s...", rel.Namespace, rel.Name, rel.Annotations[shipper.ReleaseClustersAnnotation])
				if !dryrun {
					_, err := configurator.ShipperClient.ShipperV1alpha1().Releases(ns.Name).Update(&rel)
					if err != nil {
						errList = append(errList, err.Error())
					}
					cmd.Println("done")
				} else {
					cmd.Println("dryrun")
				}
				continue
			}
			isContender, err := isContender(&rel, configurator)
			if err != nil {
				errList = append(errList, err.Error())
				continue
			}
			if len(trueClusters) == 0 && !isContender {
				cmd.Printf("Deleting release %s/%s...", rel.Namespace, rel.Name)
				if !dryrun {
					err := configurator.ShipperClient.ShipperV1alpha1().Releases(ns.Name).Delete(rel.Name, &metav1.DeleteOptions{})
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
	clusters := releaseutil.GetSelectedClusters(rel)
	var trueClusters []string
	for _, cluster := range clusters {
		if !filters.SliceContainsString(clusters, cluster) {
			trueClusters = append(trueClusters, cluster)
		}
	}
	return trueClusters
}

func isContender(rel *shipper.Release, configurator *configurator.Cluster) (bool, error) {
	appName := rel.Labels[shipper.AppLabel]
	app, err := configurator.ShipperClient.ShipperV1alpha1().Applications(rel.Namespace).Get(appName, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	contender, err := getContender(app, configurator)
	if err != nil {
		return false, err
	}
	return contender.Name == rel.Name && contender.Namespace == rel.Namespace, nil
}

func getContender(app *shipper.Application, configurator *configurator.Cluster) (*shipper.Release, error) {
	appName := app.Name
	selector := labels.Set{shipper.AppLabel: appName}.AsSelector()
	releaseList, err := configurator.ShipperClient.ShipperV1alpha1().Releases(app.Namespace).List(metav1.ListOptions{LabelSelector: selector.String()})
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
