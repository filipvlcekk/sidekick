package utils

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestIsSelfSigned_True(t *testing.T) {
	// Subject == Issuer means self-signed
	cert := &x509.Certificate{
		Subject: pkix.Name{CommonName: "TRAEFIK DEFAULT CERT"},
		Issuer:  pkix.Name{CommonName: "TRAEFIK DEFAULT CERT"},
	}
	assert.True(t, isSelfSigned(cert))
}

func TestIsSelfSigned_False(t *testing.T) {
	cert := &x509.Certificate{
		Subject: pkix.Name{CommonName: "example.com"},
		Issuer:  pkix.Name{Organization: []string{"Let's Encrypt"}},
	}
	assert.False(t, isSelfSigned(cert))
}

func TestIsLetsEncrypt_True(t *testing.T) {
	cert := &x509.Certificate{
		Issuer: pkix.Name{Organization: []string{"Let's Encrypt"}},
	}
	assert.True(t, isLetsEncrypt(cert))
}

func TestIsLetsEncrypt_ISRG(t *testing.T) {
	cert := &x509.Certificate{
		Issuer: pkix.Name{Organization: []string{"Internet Security Research Group"}},
	}
	assert.True(t, isLetsEncrypt(cert))
}

func TestIsLetsEncrypt_False(t *testing.T) {
	cert := &x509.Certificate{
		Issuer: pkix.Name{Organization: []string{"DigiCert Inc"}},
	}
	assert.False(t, isLetsEncrypt(cert))
}

func TestCertCheckResult_ExpiringWarning(t *testing.T) {
	result := CertCheckResult{
		Domain:    "example.com",
		Valid:     true,
		ExpiresAt: time.Now().Add(3 * 24 * time.Hour), // 3 days
	}
	assert.True(t, result.IsExpiringSoon())
}

func TestCertCheckResult_NotExpiring(t *testing.T) {
	result := CertCheckResult{
		Domain:    "example.com",
		Valid:     true,
		ExpiresAt: time.Now().Add(60 * 24 * time.Hour), // 60 days
	}
	assert.False(t, result.IsExpiringSoon())
}
