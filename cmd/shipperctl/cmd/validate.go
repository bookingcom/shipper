package cmd

import (
	"github.com/bookingcom/shipper/cmd/shipperctl/cmd/validate"
	"github.com/spf13/cobra"
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate external objects",
}

func init() {
	validateCmd.AddCommand(validate.HelmChartCmd())
}
