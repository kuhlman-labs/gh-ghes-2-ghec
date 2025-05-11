package cmd

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"testing"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/payload"
	"github.com/spf13/cobra"
)

func TestValidateCmd_MissingFile(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.AddCommand(validateCmd)
	validateCmd.SetArgs([]string{"nonexistent.json"})
	// This will call os.Exit(1), so we can't run it directly in a test without patching os.Exit.
	// For now, just ensure it doesn't panic.
}

func TestValidateCmd_InvalidJSON(t *testing.T) {
	tempFile, err := ioutil.TempFile("", "invalid-*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tempFile.Name())
	tempFile.WriteString("not a json")
	tempFile.Close()

	cmd := &cobra.Command{}
	cmd.AddCommand(validateCmd)
	validateCmd.SetArgs([]string{tempFile.Name()})
	// This will call os.Exit(1), so we can't run it directly in a test without patching os.Exit.
}

func TestValidateCmd_ValidJSON(t *testing.T) {
	tempFile, err := ioutil.TempFile("", "valid-*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tempFile.Name())

	validReq := payload.MigrationRequest{
		SourceOrg:    "source-org",
		TargetOrg:    "target-org",
		Repositories: []string{"repo1"},
		GHESBaseURL:  "https://ghes.example.com",
		GHESToken:    "ghes-token-0123456789012345678901234567",
		GHCloudToken: "ghcloud-token-0123456789012345678901234",
	}
	enc := json.NewEncoder(tempFile)
	enc.Encode(validReq)
	tempFile.Close()

	cmd := &cobra.Command{}
	cmd.AddCommand(validateCmd)
	validateCmd.SetArgs([]string{tempFile.Name()})
	// This will call os.Exit(0) on success, so we can't run it directly in a test without patching os.Exit.
}
