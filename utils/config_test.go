package utils

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
)

func TestSidekickConfigSaveCreatesParentDirectory(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, ".config", "sidekick", "default.yaml")

	config := SidekickConfig{
		Version:        "1",
		CurrentContext: "prod",
		Contexts: []SidekickContext{
			{Name: "prod", Server: "prod"},
		},
		Servers: []SidekickServer{
			{Name: "prod", Address: "203.0.113.10", CertEmail: "ops@example.com"},
		},
	}

	err := config.Save(configPath)
	assert.NoError(t, err)

	content, err := os.ReadFile(configPath)
	assert.NoError(t, err)

	var saved SidekickConfig
	err = yaml.Unmarshal(content, &saved)
	assert.NoError(t, err)
	assert.Equal(t, config, saved)
}
