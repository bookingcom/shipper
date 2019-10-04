package admin

import "github.com/spf13/cobra"

var Command = &cobra.Command{
	Use:   "admin",
	Short: "administrate Shipper objects",
}

var clustersCmd = &cobra.Command{
	Use:   "clusters",
	Short: "manage Shipper Clusters",
}

func init() {
	clustersCmd.AddCommand(applyCmd)
	Command.AddCommand(clustersCmd)
}
