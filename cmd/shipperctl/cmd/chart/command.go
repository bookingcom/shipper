package chart

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/bookingcom/shipper/cmd/shipperctl/kubeconfig"
	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperchart "github.com/bookingcom/shipper/pkg/chart"
	"github.com/bookingcom/shipper/pkg/chart/repo"
	"github.com/bookingcom/shipper/pkg/client"
)

var Command = &cobra.Command{
	Use:   "chart",
	Short: "operate on Shipper Charts",
}

const (
	AgentName = "shipperctl/chart"
)

var (
	namespace      string
	appName        string
	releaseName    string
	kubeConfigFile string
)

func init() {
	renderCmd := &cobra.Command{
		Use:   "render --app bikerental",
		Short: "render Shipper Charts for an Application or Release",
		RunE:  renderChart,
	}

	kubeconfig.RegisterFlag(renderCmd.Flags(), &kubeConfigFile)
	renderCmd.Flags().StringVarP(&namespace, "namespace", "n", "", "namespace where the app lives")
	renderCmd.Flags().StringVar(&appName, "app", "", "application that will have its chart rendered")
	renderCmd.Flags().StringVar(&releaseName, "release", "", "release that will have its chart rendered")

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

type ChartRenderConfig struct {
	ChartSpec   shipper.Chart
	ChartValues *shipper.ChartValues
	Namespace   string
	ReleaseName string
}

func newChartRenderConfig() (ChartRenderConfig, error) {
	c := ChartRenderConfig{}

	kubeConfig, err := kubeconfig.Load("", kubeConfigFile)
	if err != nil {
		return c, err
	}

	restConfig, err := kubeConfig.ClientConfig()
	if err != nil {
		return c, err
	}

	if namespace == "" {
		namespace, _, err = kubeConfig.Namespace()
		if err != nil {
			return c, err
		}
	}

	c.Namespace = namespace

	shipperclient := client.NewShipperClientOrDie(restConfig, AgentName, nil).ShipperV1alpha1()
	getoptions := metav1.GetOptions{}

	if releaseName != "" {
		rel, err := shipperclient.Releases(namespace).Get(releaseName, getoptions)
		if err != nil {
			return c, err
		}

		c.ReleaseName = releaseName
		c.ChartSpec = rel.Spec.Environment.Chart
		c.ChartValues = rel.Spec.Environment.Values
	} else if appName != "" {
		app, err := shipperclient.Applications(namespace).Get(appName, getoptions)
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
