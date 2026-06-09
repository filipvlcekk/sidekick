package utils

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"net"
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

func TestValidateTLSCertAtAddressUsesDomainAsSNI(t *testing.T) {
	originalDialTLSWithDialer := dialTLSWithDialer
	defer func() {
		dialTLSWithDialer = originalDialTLSWithDialer
	}()

	var gotAddress string
	var gotServerName string
	dialTLSWithDialer = func(_ *net.Dialer, _ string, address string, config *tls.Config) (tlsConnection, error) {
		gotAddress = address
		gotServerName = config.ServerName
		return fakeTLSConnection{
			state: tls.ConnectionState{
				PeerCertificates: []*x509.Certificate{
					{
						Subject:  pkix.Name{CommonName: "example.com"},
						Issuer:   pkix.Name{CommonName: "example.com"},
						NotAfter: time.Now().Add(time.Hour),
					},
				},
			},
		}, nil
	}

	result, err := ValidateTLSCertAtAddress("example.com", "203.0.113.10:443")
	assert.NoError(t, err)

	assert.Equal(t, "203.0.113.10:443", gotAddress)
	assert.Equal(t, "example.com", gotServerName)
	assert.Equal(t, "example.com", result.Domain)
	assert.False(t, result.Valid)
	assert.True(t, result.IsSelfSigned)
}

func TestValidateTLSCertWithRetryAtAddressReturnsOnFirstValidCert(t *testing.T) {
	originalDialTLSWithDialer := dialTLSWithDialer
	defer func() {
		dialTLSWithDialer = originalDialTLSWithDialer
	}()

	callCount := 0
	dialTLSWithDialer = func(_ *net.Dialer, _ string, address string, config *tls.Config) (tlsConnection, error) {
		callCount++
		assert.Equal(t, "203.0.113.10:443", address)
		assert.Equal(t, "example.com", config.ServerName)
		return fakeTLSConnection{
			state: tls.ConnectionState{
				PeerCertificates: []*x509.Certificate{
					{
						Subject:  pkix.Name{CommonName: "example.com"},
						Issuer:   pkix.Name{Organization: []string{"Let's Encrypt"}},
						NotAfter: time.Now().Add(time.Hour),
					},
				},
			},
		}, nil
	}

	result, err := ValidateTLSCertWithRetryAtAddress("example.com", "203.0.113.10:443")
	assert.NoError(t, err)
	assert.True(t, result.Valid)
	assert.Equal(t, 1, callCount)
}

func TestCheckPublicDNS_MatchExpectedIP(t *testing.T) {
	originalLookupPublicIPs := lookupPublicIPs
	defer func() {
		lookupPublicIPs = originalLookupPublicIPs
	}()

	lookupPublicIPs = func(string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("203.0.113.10")}, nil
	}

	result := CheckPublicDNS("example.com", "203.0.113.10")

	assert.True(t, result.MatchesExpected)
	assert.Equal(t, []string{"203.0.113.10"}, result.ResolvedIPs)
	assert.Contains(t, FormatDNSCheckOutput(result), "Public DNS resolves example.com -> 203.0.113.10")
}

func TestCheckPublicDNS_MismatchWarns(t *testing.T) {
	originalLookupPublicIPs := lookupPublicIPs
	defer func() {
		lookupPublicIPs = originalLookupPublicIPs
	}()

	lookupPublicIPs = func(string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("198.51.100.7")}, nil
	}

	result := CheckPublicDNS("example.com", "203.0.113.10")

	assert.False(t, result.MatchesExpected)
	assert.Equal(t, []string{"198.51.100.7"}, result.ResolvedIPs)
	assert.Contains(t, FormatDNSCheckOutput(result), "Public DNS resolves example.com elsewhere")
}

func TestCheckPublicDNS_LookupFailureWarns(t *testing.T) {
	originalLookupPublicIPs := lookupPublicIPs
	defer func() {
		lookupPublicIPs = originalLookupPublicIPs
	}()

	lookupPublicIPs = func(string) ([]net.IP, error) {
		return nil, assert.AnError
	}

	result := CheckPublicDNS("example.com", "203.0.113.10")

	assert.ErrorIs(t, result.LookupErr, assert.AnError)
	assert.Contains(t, FormatDNSCheckOutput(result), "Public DNS lookup failed for example.com")
}

func TestFormatDNSCheckOutputForServerAddsWildcardGuidance(t *testing.T) {
	output := FormatDNSCheckOutputForServer(DNSCheckResult{
		Domain:     "uptimekuma.saola.cz",
		ExpectedIP: "203.0.113.10",
	}, SidekickServer{
		CertificateMode: CertificateModeWildcard,
		WildcardDomain:  "saola.cz",
	})

	assert.Contains(t, output, "per-app DNS records")
	assert.Contains(t, output, "*.saola.cz")
}

type fakeTLSConnection struct {
	state tls.ConnectionState
}

func (f fakeTLSConnection) ConnectionState() tls.ConnectionState {
	return f.state
}

func (f fakeTLSConnection) Close() error {
	return nil
}
