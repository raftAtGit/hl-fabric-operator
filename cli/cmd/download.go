package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// downloadCmd represents the download command
var downloadCmd = &cobra.Command{
	Use:   "download FABRIC_NETWORK_NAME",
	Args:  cobra.ExactArgs(1),
	Short: "Download resources of a FabricNetwork",
	Long:  `Download resources of a FabricNetwork from Kubernetes cluster`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("TODO implement download")
	},
}

func init() {
	rootCmd.AddCommand(downloadCmd)
}
