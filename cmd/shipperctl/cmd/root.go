package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/bookingcom/shipper/cmd/shipperctl/cmd/admin"
	"github.com/bookingcom/shipper/cmd/shipperctl/cmd/chart"
)

var rootCmd = &cobra.Command{
	Use:           "shipperctl [command]",
	Short:         "Command line application to make working with Shipper more enjoyable",
	SilenceErrors: true,
	SilenceUsage:  true,
}

func init() {
	rootCmd.AddCommand(admin.Command)
	rootCmd.AddCommand(chart.Command)
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		fmt.Printf("Error! %s\n", err)
		os.Exit(1)
	}
}
