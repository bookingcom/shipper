package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/rodaine/table"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/bookingcom/shipper/cmd/shipperctl/configurator"
	"github.com/bookingcom/shipper/cmd/shipperctl/release"
	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
)

const (
	clustersFlagName = "clusters"
)

var (
	printOption string

	ListCmd = &cobra.Command{
		Use:   "list",
		Short: "lists Shipper releases that are scheduled *only* on given clusters",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			switch printOption {
			case "", "json", "yaml":
				return
			default:
				cmd.Printf("error: output format %q not supported, allowed formats are: json, yaml\n", printOption)
				os.Exit(1)
			}
		},
	}

	countContendersCmd = &cobra.Command{
		Use:   "contender",
		Short: "list Shipper *contenders* that are scheduled *only* on given clusters",
		RunE:  runCountContenderCommand,
	}

	countReleasesCmd = &cobra.Command{
		Use:   "release",
		Short: "list Shipper *releases* that are scheduled *only* on given clusters",
		RunE:  runCountReleasesCommand,
	}
)

type outputRelease struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Clusters  string `json:"clusters"`
}

func init() {
	// Flags common to all commands under `shipperctl count`
	ListCmd.PersistentFlags().StringVar(&kubeConfigFile, kubeConfigFlagName, "~/.kube/config", "The path to the Kubernetes configuration file")
	if err := ListCmd.MarkPersistentFlagFilename(kubeConfigFlagName, "yaml"); err != nil {
		ListCmd.Printf("warning: could not mark %q for filename autocompletion: %s\n", kubeConfigFlagName, err)
	}

	ListCmd.PersistentFlags().StringVar(&managementClusterContext, "management-cluster-context", "", "The name of the context to use to communicate with the management cluster. defaults to the current one")
	ListCmd.PersistentFlags().StringSliceVar(&clusters, clustersFlagName, clusters, "List of comma separated clusters to list releases that are scheduled *only* on those clusters. If empty, will list without filtering")
	ListCmd.PersistentFlags().StringVarP(&printOption, "output", "o", "", "Output format. One of: json|yaml. (Optional) defaults to verbose")

	ListCmd.AddCommand(countContendersCmd)
	ListCmd.AddCommand(countReleasesCmd)
	for _, command := range []*cobra.Command{countContendersCmd, countReleasesCmd} {
		command.SetOutput(os.Stdout)
	}
}

func runCountContenderCommand(cmd *cobra.Command, args []string) error {
	counter := 0
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
	countedReleases := []outputRelease{}
	for _, ns := range namespaceList.Items {
		applicationList, err := shipperClient.ShipperV1alpha1().Applications(ns.Name).List(metav1.ListOptions{})
		if err != nil {
			errList = append(errList, err.Error())
			continue
		}
		for _, app := range applicationList.Items {
			contender, err := release.GetContender(&app, shipperClient)
			if err != nil {
				errList = append(errList, err.Error())
				continue
			}
			clustersAnnotation := contender.Annotations[shipper.ReleaseClustersAnnotation]
			trueClusters := release.FilterSelectedClusters(strings.Split(clustersAnnotation, ","), clusters)
			if len(trueClusters) == 0 {
				counter++
				countedReleases = append(
					countedReleases,
					outputRelease{
						Namespace: contender.Namespace,
						Name:      contender.Name,
						Clusters:  clustersAnnotation,
					})
			}
		}
	}

	printCountedRelease(cmd.OutOrStdout(), countedReleases)

	if len(errList) > 0 {
		return fmt.Errorf(strings.Join(errList, ","))
	}
	return nil
}

func runCountReleasesCommand(cmd *cobra.Command, args []string) error {
	counter := 0

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
	countedReleases := []outputRelease{}
	for _, ns := range namespaceList.Items {
		releaseList, err := shipperClient.ShipperV1alpha1().Releases(ns.Name).List(metav1.ListOptions{})
		if err != nil {
			errList = append(errList, err.Error())
			continue
		}
		for _, rel := range releaseList.Items {
			clustersAnnotation := rel.Annotations[shipper.ReleaseClustersAnnotation]
			trueClusters := release.FilterSelectedClusters(strings.Split(clustersAnnotation, ","), clusters)
			if len(trueClusters) == 0 {
				counter++
				countedReleases = append(
					countedReleases,
					outputRelease{
						Namespace: rel.Namespace,
						Name:      rel.Name,
						Clusters:  clustersAnnotation,
					})
			}
		}
	}

	printCountedRelease(cmd.OutOrStdout(), countedReleases)

	if len(errList) > 0 {
		return fmt.Errorf(strings.Join(errList, ","))
	}
	return nil
}

func printCountedRelease(stdout io.Writer, outputReleases []outputRelease) {
	var err error
	var data []byte

	switch printOption {
	case "yaml":
		data, err = yaml.Marshal(outputReleases)
	case "json":
		data, err = json.MarshalIndent(outputReleases, "", "    ")
	case "":
		tbl := table.New(
			"NAMESPACE",
			"NAME",
			"CLUSTERS ANNOTATION",
		)
		for _, release := range outputReleases {
			tbl.AddRow(
				release.Namespace,
				release.Name,
				release.Clusters,
			)
		}

		tbl.Print()

		return
	}
	if err != nil {
		stdout.Write(bytes.NewBufferString(err.Error()).Bytes())
		return
	}

	if _, err := stdout.Write(data); err != nil {
		panic(err)
	}
}
