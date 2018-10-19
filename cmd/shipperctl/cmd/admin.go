package cmd

import "github.com/spf13/cobra"

var adminCmd = &cobra.Command{
	Use:   "admin",
	Short: "administrate Shipper objects",
}

func init() {
	adminCmd.AddCommand(clustersCmd)
}
