package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateCertificateModeConfig(t *testing.T) {
	assert.Error(t, ValidateCertificateModeConfig(CertificateModeWildcard, ""))
	assert.Error(t, ValidateCertificateModeConfig(CertificateModePerHost, "saola.cz"))
	assert.Error(t, ValidateCertificateModeConfig("", "saola.cz"))
	assert.Error(t, ValidateCertificateModeConfig(CertificateModeWildcard, "*.saola.cz"))
	assert.Error(t, ValidateCertificateModeConfig(CertificateModeWildcard, "foo..saola.cz"))
	assert.Error(t, ValidateCertificateModeConfig(CertificateModeWildcard, "localhost"))
	assert.NoError(t, ValidateCertificateModeConfig(CertificateModeWildcard, "saola.cz"))
	assert.NoError(t, ValidateCertificateModeConfig("", ""))
}

func TestIsHostnameWithinWildcardDomain(t *testing.T) {
	assert.True(t, IsHostnameWithinWildcardDomain("uptimekuma.saola.cz", "saola.cz"))
	assert.True(t, IsHostnameWithinWildcardDomain("grafana.saola.cz", "saola.cz"))
	assert.False(t, IsHostnameWithinWildcardDomain("saola.cz", "saola.cz"))
	assert.False(t, IsHostnameWithinWildcardDomain("foo.bar.saola.cz", "saola.cz"))
	assert.False(t, IsHostnameWithinWildcardDomain("foo.example.com", "saola.cz"))
	assert.False(t, IsHostnameWithinWildcardDomain("localhost", "saola.cz"))
}

func TestNormalizeCertificateMode(t *testing.T) {
	assert.Equal(t, CertificateModeWildcard, NormalizeCertificateMode("Wildcard"))
	assert.Equal(t, CertificateModePerHost, NormalizeCertificateMode("PER-HOST"))
	assert.Equal(t, CertificateModePerHost, NormalizeCertificateMode(""))
	assert.Equal(t, "unknown", NormalizeCertificateMode("UNKNOWN"))
}
