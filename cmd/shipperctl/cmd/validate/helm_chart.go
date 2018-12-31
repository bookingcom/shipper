package validate

import (
	"fmt"

	"github.com/spf13/cobra"
	"k8s.io/helm/pkg/chartutil"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperchart "github.com/bookingcom/shipper/pkg/chart"
)

var helmChartCmd = &cobra.Command{
	Use:   "chart",
	Short: "Validate Helm chart",
	RunE:  runValidateHelmChartCommand,
}

func HelmChartCmd() *cobra.Command {
	return helmChartCmd
}

func runValidateHelmChartCommand(cmd *cobra.Command, args []string) error {
	for _, chartPath := range args {
		// TODO: make it understand remote chart URLs
		// use chart.downloadChart/3
		chart, loadErr := chartutil.Load(chartPath)
		if loadErr != nil {
			return fmt.Errorf("Failed to load chart under path %q: %s", chartPath, loadErr.Error())
		}
		name, namespace := "shipperctl", "shipperctl"
		var values *shipper.ChartValues
		if validateErr := shipperchart.Validate(chart, name, namespace, values); validateErr != nil {
			return fmt.Errorf("Chart validation failed: %s\n", validateErr.Error())
		} else {
			cmd.Printf("Chart %s successfully passed all validation checks\n", chartPath)
		}
	}

	return nil
}
