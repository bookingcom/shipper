package chart

import (
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/bookingcom/shipper/cmd/shipperctl/config"
)

var (
	releaseName string

	renderRelCmd = &cobra.Command{
		Use:   "release bikerental-7abf46d4-0",
		Short: "render Shipper Charts for a Release",
		RunE:  renderChartFromRel,
		Args: cobra.ExactArgs(1),
	}
)

func init() {
	renderCmd.AddCommand(renderRelCmd)
}

func renderChartFromRel(cmd *cobra.Command, args []string) error {
	releaseName = args[0]
	c, err := newChartRenderConfig()
	if err != nil {
		return err
	}
	_, shipperClient, err := config.Load(kubeConfigFile, managementClusterContext)
	if err != nil {
		return err
	}

	rel, err := shipperClient.ShipperV1alpha1().Releases(namespace).Get(releaseName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	c.ReleaseName = rel.Name
	c.ChartSpec = rel.Spec.Environment.Chart
	c.ChartValues = rel.Spec.Environment.Values

	rendered, err := render(c)
	if err != nil {
		return err
	}

	cmd.Println(rendered)
	return nil
}
