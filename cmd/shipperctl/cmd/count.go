package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/bookingcom/shipper/cmd/shipperctl/configurator"
	cmdutil "github.com/bookingcom/shipper/cmd/shipperctl/util"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

const (
	clustersFlagName = "clusters"
)

var (
	printOption string

	CountCmd = &cobra.Command{
		Use:   "count",
		Short: "count Shipper releases that are scheduled *only* on given clusters",
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
		Short: "count Shipper *contenders* that are scheduled *only* on given clusters",
		RunE:  runCountContenderCommand,
	}

	countReleasesCmd = &cobra.Command{
		Use:   "release",
		Short: "count Shipper *releases* that are scheduled *only* on given clusters",
		RunE:  runCountReleasesCommand,
	}
)

type OutputRelease struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

func init() {
	// Flags common to all commands under `shipperctl count`
	CountCmd.PersistentFlags().StringVar(&kubeConfigFile, kubeConfigFlagName, "~/.kube/config", "The path to the Kubernetes configuration file")
	if err := CountCmd.MarkPersistentFlagFilename(kubeConfigFlagName, "yaml"); err != nil {
		CountCmd.Printf("warning: could not mark %q for filename autocompletion: %s\n", kubeConfigFlagName, err)
	}

	CountCmd.PersistentFlags().StringVar(&managementClusterContext, "management-cluster-context", "", "The name of the context to use to communicate with the management cluster. defaults to the current one")
	CountCmd.PersistentFlags().StringSliceVar(&clusters, clustersFlagName, clusters, "List of comma separated clusters to count releases that are scheduled on. If empty, will count without filtering")
	CountCmd.PersistentFlags().StringVarP(&printOption, "output", "o", "", "Output format. One of: json|yaml. (Optional)")

	CountCmd.AddCommand(countContendersCmd)
	CountCmd.AddCommand(countReleasesCmd)
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
	countedReleases := []OutputRelease{}
	for _, ns := range namespaceList.Items {
		applicationList, err := shipperClient.ShipperV1alpha1().Applications(ns.Name).List(metav1.ListOptions{})
		if err != nil {
			errList = append(errList, err.Error())
			continue
		}
		for _, app := range applicationList.Items {
			contender, err := cmdutil.GetContender(&app, shipperClient)
			if err != nil {
				errList = append(errList, err.Error())
				continue
			}
			trueClusters := cmdutil.FilterSelectedClusters(releaseutil.GetSelectedClusters(contender), clusters)
			if len(trueClusters) == 0 {
				counter++
				countedReleases = append(
					countedReleases,
					OutputRelease{
						Namespace: contender.Namespace,
						Name:      contender.Name,
					})
			}
		}
	}

	if printOption == "" {
		cmd.Println("Number of *contenders* that are scheduled only on decommissioned clusters: ", counter)
	} else {
		printCountedRelease(cmd.OutOrStdout(), countedReleases)
	}
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
	countedReleases := []OutputRelease{}
	for _, ns := range namespaceList.Items {
		releaseList, err := shipperClient.ShipperV1alpha1().Releases(ns.Name).List(metav1.ListOptions{})
		if err != nil {
			errList = append(errList, err.Error())
			continue
		}
		for _, rel := range releaseList.Items {
			trueClusters := cmdutil.FilterSelectedClusters(releaseutil.GetSelectedClusters(&rel), clusters)
			if len(trueClusters) == 0 {
				counter++
				countedReleases = append(
					countedReleases,
					OutputRelease{
						Namespace: rel.Namespace,
						Name:      rel.Name,
					})
			}
		}
	}

	if printOption == "" {
		cmd.Println("Number of *releases* that are scheduled only on decommissioned clusters: ", counter)
	} else {
		printCountedRelease(cmd.OutOrStdout(), countedReleases)
	}
	if len(errList) > 0 {
		return fmt.Errorf(strings.Join(errList, ","))
	}
	return nil
}

func printCountedRelease(stdout io.Writer, outputReleases []OutputRelease) {
	var err error
	var data []byte

	switch printOption {
	case "yaml":
		data, err = yaml.Marshal(outputReleases)
	case "json":
		data, err = json.MarshalIndent(outputReleases, "", "    ")
	case "":
		return
	}
	if err != nil {
		stdout.Write(bytes.NewBufferString(err.Error()).Bytes())
		return
	}

	_, _ = stdout.Write(data)
}
