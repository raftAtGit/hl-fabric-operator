package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	goVersion "go.hein.dev/go-version"
)

var (
	versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Long:  `Print version information`,
		Run: func(_ *cobra.Command, _ []string) {
			resp := goVersion.FuncWithOutput(shortened, version, commit, date, outputFormat)
			fmt.Print(resp)
			return
		},
	}
)

func init() {
	rootCmd.AddCommand(versionCmd)

	versionCmd.Flags().BoolVarP(&shortened, "short", "s", false, "Print just the version number.")
	versionCmd.Flags().StringVarP(&outputFormat, "output", "o", "json", "Output format. One of 'yaml' or 'json'.")
}
