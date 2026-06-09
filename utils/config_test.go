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

func TestSidekickConfigSaveIncludesWildcardServerFields(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "default.yaml")

	config := SidekickConfig{
		Version:        "1",
		CurrentContext: "scvd",
		Contexts: []SidekickContext{
			{Name: "scvd", Server: "scvd"},
		},
		Servers: []SidekickServer{
			{
				Name:            "scvd",
				Address:         "204.10.194.116",
				CertificateMode: CertificateModeWildcard,
				WildcardDomain:  "saola.cz",
			},
		},
	}

	err := config.Save(configPath)
	assert.NoError(t, err)

	content, err := os.ReadFile(configPath)
	assert.NoError(t, err)
	assert.Contains(t, string(content), "certificateMode: wildcard")
	assert.Contains(t, string(content), "wildcardDomain: saola.cz")
}

func TestSidekickConfigLoadLegacyServerDefaultsToPerHost(t *testing.T) {
	const configYAML = `
version: "1"
servers:
  - name: scvd
    serveraddress: 204.10.194.116
contexts:
  - name: scvd
    server: scvd
current-context: scvd
`

	var config SidekickConfig
	err := yaml.Unmarshal([]byte(configYAML), &config)
	assert.NoError(t, err)

	NormalizeSidekickConfig(&config)

	if assert.Len(t, config.Servers, 1) {
		server := config.Servers[0]
		assert.Equal(t, CertificateModePerHost, server.CertificateMode)
		assert.Empty(t, server.WildcardDomain)
	}
}

func TestNormalizeSidekickServer(t *testing.T) {
	server := SidekickServer{
		Name:            "scvd",
		CertificateMode: "Wildcard",
		WildcardDomain:  "Saola.CZ.",
	}

	NormalizeSidekickServer(&server)

	assert.Equal(t, CertificateModeWildcard, server.CertificateMode)
	assert.Equal(t, "saola.cz", server.WildcardDomain)
}
