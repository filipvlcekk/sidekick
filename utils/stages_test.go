package utils

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTraefikComposeUsesCanonicalACMEDir(t *testing.T) {
	assert.Contains(t, TraefikDockerComposeFile, "./traefik/ssl-certs/:/ssl-certs/")
}

func TestTraefikStageCreatesCanonicalACMEDir(t *testing.T) {
	stage := GetTraefikStage(
		"ops@example.com",
		DNSProvider{TraefikName: "cloudflare"},
		map[string]string{"CF_DNS_API_TOKEN": "token"},
		TraefikTLSModeConfig{},
	)

	assert.Contains(t, stage.Commands, "mkdir -p ./traefik/ssl-certs/")
	assert.Contains(t, stage.Commands, "touch ./traefik/ssl-certs/acme.json")
	assert.Contains(t, stage.Commands, "chmod 600 ./traefik/ssl-certs/acme.json")
}

func TestBuildTraefikConfigIncludesDNS01ProviderAndCanonicalStorage(t *testing.T) {
	_, composeB64 := buildTraefikConfig(
		"ops@example.com",
		DNSProvider{TraefikName: "cloudflare"},
		map[string]string{"CF_DNS_API_TOKEN": "token"},
		TraefikTLSModeConfig{},
	)

	composeBytes, err := base64.StdEncoding.DecodeString(composeB64)
	assert.NoError(t, err)

	compose := string(composeBytes)
	assert.Contains(t, compose, "--certificatesresolvers.default.acme.storage=/ssl-certs/acme.json")
	assert.Contains(t, compose, "--certificatesresolvers.default.acme.dnschallenge.provider=cloudflare")
	assert.True(t, strings.Contains(compose, "./traefik/ssl-certs/:/ssl-certs/"))
}

func TestBuildTraefikConfigWildcardModeAddsStaticTLSDomains(t *testing.T) {
	_, composeB64 := buildTraefikConfig(
		"ops@example.com",
		DNSProvider{TraefikName: "digitalocean"},
		map[string]string{"DO_AUTH_TOKEN": "token"},
		TraefikTLSModeConfig{
			CertificateMode: CertificateModeWildcard,
			WildcardDomain:  "saola.cz",
		},
	)

	composeBytes, err := base64.StdEncoding.DecodeString(composeB64)
	assert.NoError(t, err)

	compose := string(composeBytes)
	assert.Contains(t, compose, "--entrypoints.websecure.http.tls.certresolver=default")
	assert.Contains(t, compose, "--entrypoints.websecure.http.tls.domains[0].main=saola.cz")
	assert.Contains(t, compose, "--entrypoints.websecure.http.tls.domains[0].sans=*.saola.cz")
}

func TestBuildTraefikConfigPerHostModeDoesNotAddStaticTLSDomains(t *testing.T) {
	_, composeB64 := buildTraefikConfig(
		"ops@example.com",
		DNSProvider{TraefikName: "digitalocean"},
		map[string]string{"DO_AUTH_TOKEN": "token"},
		TraefikTLSModeConfig{
			CertificateMode: CertificateModePerHost,
		},
	)

	composeBytes, err := base64.StdEncoding.DecodeString(composeB64)
	assert.NoError(t, err)

	compose := string(composeBytes)
	assert.NotContains(t, compose, "--entrypoints.websecure.http.tls.domains[0].main=")
	assert.NotContains(t, compose, "--entrypoints.websecure.http.tls.domains[0].sans=")
}
