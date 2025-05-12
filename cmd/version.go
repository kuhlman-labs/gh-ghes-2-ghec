// Package cmd provides CLI commands for the application.
package cmd

import (
	"fmt"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/version"
	"github.com/spf13/cobra"
)

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show the version information",
	Long:  `Display the version, build time, and other version-related information.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(version.GetVersionInfo())
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
