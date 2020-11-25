package chart

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/bookingcom/shipper/cmd/shipperctl/config"
	"github.com/bookingcom/shipper/cmd/shipperctl/configurator"
	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperchart "github.com/bookingcom/shipper/pkg/chart"
	"github.com/bookingcom/shipper/pkg/chart/repo"
)

var (
	namespace                string
	appName                  string
	releaseName              string
	kubeConfigFile           string
	managementClusterContext string

	Command = &cobra.Command{
		Use:   "chart",
		Short: "operate on Shipper Charts",
	}

	renderCmd = &cobra.Command{
		Use:   "render --app bikerental",
		Short: "render Shipper Charts for an Application or Release",
		RunE:  renderChart,
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
	Command.PersistentFlags().StringVarP(&namespace, "namespace", "n", "", "namespace where the app lives")
	Command.PersistentFlags().StringVar(&appName, "app", "", "application that will have its chart rendered")
	Command.PersistentFlags().StringVar(&releaseName, "release", "", "release that will have its chart rendered")

	Command.AddCommand(renderCmd)
}

func renderChart(cmd *cobra.Command, args []string) error {
	c, err := newChartRenderConfig()
	if err != nil {
		return err
	}

	chartFetcher := newFetchChartFunc()
	chart, err := chartFetcher(&c.ChartSpec)
	if err != nil {
		return err
	}

	rendered, err := shipperchart.Render(
		chart,
		c.ReleaseName,
		c.Namespace,
		c.ChartValues,
	)
	if err != nil {
		return err
	}

	for _, v := range rendered {
		fmt.Printf("%s\n---\n", v)
	}

	return nil
}

func newChartRenderConfig() (ChartRenderConfig, error) {
	c := ChartRenderConfig{
	}
	clientConfig, _, err := configurator.ClientConfig(kubeConfigFile, managementClusterContext)
	if err != nil {
		return c, err
	}
	if namespace == "" {
		namespace, _, err = clientConfig.Namespace()
		if err != nil {
			return c, err
		}
	}
	c.Namespace = namespace

	_, shipperClient, err := config.Load(kubeConfigFile, managementClusterContext)
	if err != nil {
		return c, err
	}

	getoptions := metav1.GetOptions{}

	if releaseName != "" {
		rel, err := shipperClient.ShipperV1alpha1().Releases(namespace).Get(releaseName, getoptions)
		if err != nil {
			return c, err
		}

		c.ReleaseName = releaseName
		c.ChartSpec = rel.Spec.Environment.Chart
		c.ChartValues = rel.Spec.Environment.Values
	} else if appName != "" {
		app, err := shipperClient.ShipperV1alpha1().Applications(namespace).Get(appName, getoptions)
		if err != nil {
			return c, err
		}

		c.ReleaseName = fmt.Sprintf("%s-%s-%d", appName, "foobar", 0)
		c.ChartSpec = app.Spec.Template.Chart
		c.ChartValues = app.Spec.Template.Values
	}

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
