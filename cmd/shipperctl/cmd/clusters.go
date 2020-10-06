package cmd

import "github.com/spf13/cobra"

var clustersCmd = &cobra.Command{
	Use:   "clusters",
	Short: "manage Shipper Clusters",
}

func init() {
	clustersCmd.AddCommand(applyCmd)
}
