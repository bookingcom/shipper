package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/bookingcom/shipper/cmd/shipperctl/cmd"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:           "shipperctl [command]",
	Short:         "Command line application to make working with Shipper more enjoyable",
	SilenceErrors: true,
	SilenceUsage:  true,
}

func init() {
	rootCmd.AddCommand(cmd.ClustersCmd)
	rootCmd.AddCommand(cmd.CountCmd)
	rootCmd.AddCommand(cmd.CleanCmd)
}

func main() {
	flag.Parse()
	err := rootCmd.Execute()
	if err != nil {
		fmt.Printf("Error! %s\n", err)
		os.Exit(1)
	}
}
