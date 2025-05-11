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
	defer os.Chdir(oldWd)
	os.Chdir(tempDir)

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
	defer os.Chdir(oldWd)
	os.Chdir(tempDir)

	// Create a dummy config.yaml
	f, _ := os.Create("config.yaml")
	f.Close()

	rootCmd.SetArgs([]string{"config", "init"})
	err := rootCmd.Execute()
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "config file already exists")
	}
}
