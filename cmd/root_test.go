package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mightymoud/sidekick/utils"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func TestInitConfigNormalizesLoadedServers(t *testing.T) {
	t.Cleanup(viper.Reset)

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "default.yaml")

	const configYAML = `
version: "1"
servers:
  - name: legacy
    serveraddress: 203.0.113.10
  - name: wildcard
    serveraddress: 203.0.113.11
    certificateMode: Wildcard
    wildcardDomain: Saola.CZ.
contexts:
  - name: legacy
    server: legacy
current-context: legacy
`

	err := os.WriteFile(configPath, []byte(configYAML), 0600)
	assert.NoError(t, err)

	testCmd := &cobra.Command{Use: "deploy"}
	testCmd.Flags().String("config", configPath, "")
	testCmd.SetContext(context.Background())

	initConfig(testCmd)

	config, err := utils.GetSidekickConfigFromCmdContext(testCmd)
	assert.NoError(t, err)
	if assert.Len(t, config.Servers, 2) {
		assert.Equal(t, utils.CertificateModePerHost, config.Servers[0].CertificateMode)
		assert.Empty(t, config.Servers[0].WildcardDomain)
		assert.Equal(t, utils.CertificateModeWildcard, config.Servers[1].CertificateMode)
		assert.Equal(t, "saola.cz", config.Servers[1].WildcardDomain)
	}
}
