package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/logging"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/validation"
	"github.com/spf13/cobra"
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate a migration request JSON file",
	Long: `Validate a migration request JSON file without executing the migration.
This helps to check if your migration parameters are valid before submitting them to the server.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		filePath := args[0]
		logger := logging.Get()

		// Check if file exists
		file, err := os.Open(filePath)
		if err != nil {
			logger.Error("Failed to open file", "error", err, "path", filePath)
			fmt.Printf("Error: Failed to open file %s: %v\n", filePath, err)
			os.Exit(1)
		}
		defer file.Close()

		// Read file contents
		data, err := io.ReadAll(file)
		if err != nil {
			logger.Error("Failed to read file", "error", err, "path", filePath)
			fmt.Printf("Error: Failed to read file %s: %v\n", filePath, err)
			os.Exit(1)
		}

		// Decode JSON
		var req payload.MigrationRequest
		if err := json.Unmarshal(data, &req); err != nil {
			logger.Error("Failed to parse JSON", "error", err, "path", filePath)
			fmt.Printf("Error: Invalid JSON format: %v\n", err)
			os.Exit(1)
		}

		// Validate request
		if err := req.Validate(); err != nil {
			logger.Error("Validation failed", "error", err, "path", filePath)
			fmt.Printf("Error: Validation failed: %v\n", err)
			os.Exit(1)
		}

		// Additional connection testing
		testConnections, _ := cmd.Flags().GetBool("test-connections")
		if testConnections {
			fmt.Println("Testing connection to GHES instance...")
			if err := validation.TestGHESURL(req.GHESBaseURL, req.GHESToken); err != nil {
				logger.Error("GHES connection test failed", "error", err)
				fmt.Printf("Error: GHES connection test failed: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("✅ GHES connection successful")
		}

		// Print summary
		fmt.Println("✅ Migration request is valid!")
		fmt.Println("\nSummary:")
		fmt.Printf("  Source Organization: %s\n", req.SourceOrg)
		fmt.Printf("  Target Organization: %s\n", req.TargetOrg)
		fmt.Printf("  GHES Instance: %s\n", req.GHESBaseURL)
		fmt.Printf("  Repositories: %d\n", len(req.Repositories))
		if req.MaxDuration != "" {
			fmt.Printf("  Maximum Duration: %s\n", req.MaxDuration)
		} else {
			fmt.Printf("  Maximum Duration: Default (24h)\n")
		}

		fmt.Println("\nTo run this migration, start the server and submit this JSON to the /migrate endpoint.")
	},
}

func init() {
	rootCmd.AddCommand(validateCmd)
	validateCmd.Flags().Bool("test-connections", false, "Test connections to both GHES and GHEC instances")
}
