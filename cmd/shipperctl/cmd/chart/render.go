package chart

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bookingcom/shipper/cmd/shipperctl/config"
	"github.com/bookingcom/shipper/cmd/shipperctl/configurator"
	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperchart "github.com/bookingcom/shipper/pkg/chart"
	"github.com/bookingcom/shipper/pkg/chart/repo"
)

var (
	namespace                string
	kubeConfigFile           string
	managementClusterContext string

	Command = &cobra.Command{
		Use:   "chart",
		Short: "operate on Shipper Charts",
	}

	renderCmd = &cobra.Command{
		Use:   "render",
		Short: "render Helm Charts for an Application or Release",
	}
)

type ChartRenderConfig struct {
	ChartSpec   shipper.Chart
	ChartValues *shipper.ChartValues
	Namespace   string
	ReleaseName string
}

func init() {
	const kubeConfigFlagName = "kubeconfig"
	config.RegisterFlag(Command.PersistentFlags(), &kubeConfigFile)
	if err := Command.MarkPersistentFlagFilename(kubeConfigFlagName, "yaml"); err != nil {
		Command.Printf("warning: could not mark %q for filename autocompletion: %s\n", kubeConfigFlagName, err)
	}
	Command.PersistentFlags().StringVar(&managementClusterContext, "management-cluster-context", "", "The name of the context to use to communicate with the management cluster. defaults to the current one")
	Command.PersistentFlags().StringVarP(&namespace, "namespace", "n", "", "The namespace where the app lives")

	Command.AddCommand(renderCmd)
}

func render(c ChartRenderConfig) (string, error) {
	chartFetcher := newFetchChartFunc()
	chart, err := chartFetcher(&c.ChartSpec)
	if err != nil {
		return "", err
	}

	rendered, err := shipperchart.Render(
		chart,
		c.ReleaseName,
		c.Namespace,
		c.ChartValues,
	)
	if err != nil {
		return "", err
	}

	return strings.Join(rendered, "%s\n---\n"), nil
}

func newChartRenderConfig() (ChartRenderConfig, error) {
	c := ChartRenderConfig{
	}
	if namespace == "" {
		clientConfig, _, err := configurator.ClientConfig(kubeConfigFile, managementClusterContext)
		if err != nil {
			return c, err
		}
		namespace, _, err = clientConfig.Namespace()
		if err != nil {
			return c, err
		}
	}
	c.Namespace = namespace

	return c, nil
}

func newFetchChartFunc() repo.ChartFetcher {
	stopCh := make(<-chan struct{})

	repoCatalog := repo.NewCatalog(
		repo.DefaultFileCacheFactory(filepath.Join(os.TempDir(), "chart-cache")),
		repo.DefaultRemoteFetcher,
		stopCh)

	return repo.FetchChartFunc(repoCatalog)
}
