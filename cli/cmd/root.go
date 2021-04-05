package cmd

import (
	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "rfabric",
	Short: "Fabric Operator CLI is a supplementary tool for interacting with Fabric Operator",
	Long: `Fabric Operator CLI is a supplementary tool for interacting with Fabric Operator.

It’s not required for normal operation of Fabric Operator but provided as a tool for convenience.

It performs client-side validation and creates necessary resources in Kubernetes on the fly. 

By using CLI, it's possible to specify supplementary inputs as references to the local file system. 
For example if chaincode.folder is provided in the FabricNetwork CRD like below, CLI will create chaincode ConfigMaps before submitting the FabricNetwork to Kubernetes.

chaincode:
  folder: ../chaincode
`,
}

var (
	verbose       = false
	namespace     string
	allNamespaces = false
	overwrite     = false
	keepResources = false
	shortened     = false
	version       = "dev"
	commit        = "none"
	date          = "unknown"
	outputFormat  = "json"
	outputDir     string
)

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	cobra.CheckErr(rootCmd.Execute())
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "default", "Kubernetes namespace")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")

	// rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
