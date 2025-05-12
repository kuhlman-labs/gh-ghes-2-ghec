package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfigInitCmd_CreatesFile(t *testing.T) {
	tempDir := t.TempDir()
	oldWd, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Errorf("Failed to change back to original directory: %v", err)
		}
	}()

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Remove config.yaml if it exists
	_ = os.Remove("config.yaml")

	rootCmd.SetArgs([]string{"config", "init"})
	err := rootCmd.Execute()
	assert.NoError(t, err)

	// Check that the config file was created
	configPath := filepath.Join(tempDir, "config.yaml")
	_, err = os.Stat(configPath)
	assert.NoError(t, err)
}

func TestConfigInitCmd_AlreadyExists(t *testing.T) {
	tempDir := t.TempDir()
	oldWd, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Errorf("Failed to change back to original directory: %v", err)
		}
	}()

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Create a dummy config.yaml
	f, _ := os.Create("config.yaml")
	defer func() {
		if err := f.Close(); err != nil {
			t.Errorf("Failed to close file: %v", err)
		}
	}()

	rootCmd.SetArgs([]string{"config", "init"})
	err := rootCmd.Execute()
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "config file already exists")
	}
}
