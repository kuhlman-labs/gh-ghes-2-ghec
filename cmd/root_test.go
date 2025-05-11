package cmd

import (
	"bytes"
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestRootCommand_Help(t *testing.T) {
	cmd := rootCmd
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--help"})
	err := cmd.Execute()
	assert.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "A tool for migrating repositories from GitHub Enterprise Server to GitHub Enterprise Cloud")
}

func TestRootCommand_UnknownFlag(t *testing.T) {
	cmd := rootCmd
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--unknown-flag"})
	err := cmd.Execute()
	assert.Error(t, err)
	output := buf.String()
	assert.Contains(t, output, "unknown flag")
}

func TestExecute_DoesNotExit(t *testing.T) {
	// Patch os.Exit to prevent test from exiting
	exitCalled := false
	oldExit := osExit
	osExit = func(code int) { exitCalled = true }
	defer func() { osExit = oldExit }()

	// Patch rootCmd to a dummy command
	cmd := &cobra.Command{Use: "dummy", Run: func(cmd *cobra.Command, args []string) {}}
	rootCmd = cmd
	Execute()
	assert.False(t, exitCalled, "os.Exit should not be called for successful execution")
}

// Patchable os.Exit for testing
var osExit = os.Exit
