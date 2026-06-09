package initialize

import (
	"strings"
	"testing"

	"github.com/mightymoud/sidekick/utils"
	"github.com/stretchr/testify/assert"
)

func TestApplyCertificateSettings(t *testing.T) {
	t.Run("sets wildcard mode and domain", func(t *testing.T) {
		server := utils.SidekickServer{Name: "scvd"}

		got, err := applyCertificateSettings(server, utils.CertificateModeWildcard, "Saola.CZ.")

		assert.NoError(t, err)
		assert.Equal(t, utils.CertificateModeWildcard, got.CertificateMode)
		assert.Equal(t, "saola.cz", got.WildcardDomain)
	})

	t.Run("defaults empty mode to per-host and clears stale wildcard domain", func(t *testing.T) {
		server := utils.SidekickServer{
			Name:            "scvd",
			CertificateMode: utils.CertificateModeWildcard,
			WildcardDomain:  "saola.cz",
		}

		got, err := applyCertificateSettings(server, "", "")

		assert.NoError(t, err)
		assert.Equal(t, utils.CertificateModePerHost, got.CertificateMode)
		assert.Empty(t, got.WildcardDomain)
	})

	t.Run("validates wildcard domain with shared helper", func(t *testing.T) {
		server := utils.SidekickServer{Name: "scvd"}

		_, err := applyCertificateSettings(server, utils.CertificateModeWildcard, "*.saola.cz")

		assert.ErrorContains(t, err, `wildcard domain "*.saola.cz" must be a DNS zone like example.com`)
	})
}

func TestValidateCertificateModeFlags(t *testing.T) {
	t.Run("allows wildcard domain in wildcard mode", func(t *testing.T) {
		err := validateCertificateModeFlags(utils.CertificateModeWildcard, "saola.cz")

		assert.NoError(t, err)
	})

	t.Run("fails when wildcard domain is provided outside wildcard mode", func(t *testing.T) {
		err := validateCertificateModeFlags(utils.CertificateModePerHost, "saola.cz")

		assert.EqualError(t, err, `--wildcard-domain requires --certificate-mode=wildcard`)
	})
}

func TestWildcardInitGuidance(t *testing.T) {
	msg := wildcardInitGuidance("saola.cz")
	normalized := strings.ToLower(msg)

	assert.Contains(t, normalized, "optional but recommended")
	assert.Contains(t, msg, "*.example.com")
	assert.Contains(t, msg, "*.saola.cz")
	assert.Contains(t, normalized, "per-app dns records")
	assert.Contains(t, normalized, "wildcard dns record")
	assert.Contains(t, normalized, "all deployed app hostnames")
	assert.Contains(t, normalized, "must stay within saola.cz")
}

func TestDefaultInteractiveCertificateMode(t *testing.T) {
	assert.Equal(t, utils.CertificateModePerHost, defaultInteractiveCertificateMode(""))
	assert.Equal(t, utils.CertificateModePerHost, defaultInteractiveCertificateMode(utils.CertificateModePerHost))
	assert.Equal(t, utils.CertificateModePerHost, defaultInteractiveCertificateMode(utils.CertificateModeWildcard))
}

func TestShouldRewriteTraefikForCertificateMode(t *testing.T) {
	tests := []struct {
		name                    string
		existingMode            string
		requestedMode           string
		existingWildcardDomain  string
		requestedWildcardDomain string
		expected                bool
	}{
		{
			name:          "legacy empty mode matches per-host default",
			existingMode:  "",
			requestedMode: "",
			expected:      false,
		},
		{
			name:                    "per-host to wildcard rewrites",
			existingMode:            utils.CertificateModePerHost,
			requestedMode:           utils.CertificateModeWildcard,
			requestedWildcardDomain: "saola.cz",
			expected:                true,
		},
		{
			name:                   "wildcard to per-host rewrites",
			existingMode:           utils.CertificateModeWildcard,
			existingWildcardDomain: "saola.cz",
			requestedMode:          utils.CertificateModePerHost,
			expected:               true,
		},
		{
			name:                    "wildcard domain change rewrites",
			existingMode:            utils.CertificateModeWildcard,
			existingWildcardDomain:  "saola.cz",
			requestedMode:           utils.CertificateModeWildcard,
			requestedWildcardDomain: "apps.saola.cz",
			expected:                true,
		},
		{
			name:                    "matching wildcard settings do not rewrite",
			existingMode:            utils.CertificateModeWildcard,
			existingWildcardDomain:  "Saola.CZ.",
			requestedMode:           utils.CertificateModeWildcard,
			requestedWildcardDomain: "saola.cz",
			expected:                false,
		},
		{
			name:          "matching per-host settings do not rewrite",
			existingMode:  utils.CertificateModePerHost,
			requestedMode: utils.CertificateModePerHost,
			expected:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(
				t,
				tt.expected,
				shouldRewriteTraefikForCertificateMode(tt.existingMode, tt.requestedMode, tt.existingWildcardDomain, tt.requestedWildcardDomain),
			)
		})
	}
}

func TestServerConfigForPersistence(t *testing.T) {
	t.Run("keeps requested certificate settings when migration was applied", func(t *testing.T) {
		existing := utils.SidekickServer{
			Name:            "scvd",
			Address:         "203.0.113.10",
			CertificateMode: utils.CertificateModePerHost,
		}
		requested := utils.SidekickServer{
			Name:            "scvd",
			Address:         "203.0.113.11",
			DNSProvider:     "digitalocean",
			CertEmail:       "ops@example.com",
			CertificateMode: utils.CertificateModeWildcard,
			WildcardDomain:  "saola.cz",
		}

		got := serverConfigForPersistence(existing, requested, true)

		assert.Equal(t, requested, got)
	})

	t.Run("reverts only certificate mode fields when migration was declined", func(t *testing.T) {
		existing := utils.SidekickServer{
			Name:            "scvd",
			Address:         "203.0.113.10",
			DNSProvider:     "cloudflare",
			CertEmail:       "old@example.com",
			CertificateMode: utils.CertificateModePerHost,
			WildcardDomain:  "",
		}
		requested := utils.SidekickServer{
			Name:            "scvd",
			Address:         "203.0.113.11",
			DNSProvider:     "digitalocean",
			CertEmail:       "ops@example.com",
			CertificateMode: utils.CertificateModeWildcard,
			WildcardDomain:  "saola.cz",
		}

		got := serverConfigForPersistence(existing, requested, false)

		assert.Equal(t, "203.0.113.11", got.Address)
		assert.Equal(t, "digitalocean", got.DNSProvider)
		assert.Equal(t, "ops@example.com", got.CertEmail)
		assert.Equal(t, utils.CertificateModePerHost, got.CertificateMode)
		assert.Empty(t, got.WildcardDomain)
	})
}

func TestShouldPersistRequestedCertificateSettings(t *testing.T) {
	existingPerHost := utils.SidekickServer{
		Name:            "scvd",
		CertificateMode: utils.CertificateModePerHost,
	}
	requestedWildcard := utils.SidekickServer{
		Name:            "scvd",
		CertificateMode: utils.CertificateModeWildcard,
		WildcardDomain:  "saola.cz",
	}

	t.Run("returns false when rewrite was skipped and certificate mode changed", func(t *testing.T) {
		assert.False(t, shouldPersistRequestedCertificateSettings(existingPerHost, requestedWildcard, false))
	})

	t.Run("returns true when rewrite was skipped but certificate mode did not change", func(t *testing.T) {
		assert.True(t, shouldPersistRequestedCertificateSettings(existingPerHost, existingPerHost, false))
	})

	t.Run("returns true when rewrite ran", func(t *testing.T) {
		assert.True(t, shouldPersistRequestedCertificateSettings(existingPerHost, requestedWildcard, true))
	})
}
