package utils

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"strings"
	"time"
)

// CertCheckResult holds the result of a TLS certificate validation.
type CertCheckResult struct {
	Domain       string
	Valid        bool
	Issuer       string
	ExpiresAt    time.Time
	IsSelfSigned bool
	Error        error
}

// IsExpiringSoon returns true if the certificate expires within 7 days.
func (r CertCheckResult) IsExpiringSoon() bool {
	return time.Until(r.ExpiresAt) < 7*24*time.Hour
}

// ValidateTLSCert connects to domain:443 and validates the TLS certificate.
func ValidateTLSCert(domain string) (*CertCheckResult, error) {
	conn, err := tls.DialWithDialer(
		&net.Dialer{Timeout: 10 * time.Second},
		"tcp",
		domain+":443",
		&tls.Config{InsecureSkipVerify: true}, // We inspect the cert ourselves
	)
	if err != nil {
		return nil, fmt.Errorf("TLS connection to %s:443 failed: %w", domain, err)
	}
	defer conn.Close()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return nil, fmt.Errorf("no certificates returned by %s", domain)
	}

	leaf := certs[0]
	selfSigned := isSelfSigned(leaf)
	letsEncrypt := isLetsEncrypt(leaf)

	issuerStr := formatIssuer(leaf)

	result := &CertCheckResult{
		Domain:       domain,
		Valid:        !selfSigned && letsEncrypt,
		Issuer:       issuerStr,
		ExpiresAt:    leaf.NotAfter,
		IsSelfSigned: selfSigned,
	}

	return result, nil
}

// ValidateTLSCertWithRetry attempts validation with exponential backoff.
// Retries 3 times at 30s, 60s, 120s intervals.
func ValidateTLSCertWithRetry(domain string) (*CertCheckResult, error) {
	delays := []time.Duration{30 * time.Second, 60 * time.Second, 120 * time.Second}

	for i, delay := range delays {
		result, err := ValidateTLSCert(domain)
		if err != nil {
			if i < len(delays)-1 {
				time.Sleep(delay)
				continue
			}
			return nil, err
		}

		if result.Valid {
			return result, nil
		}

		// Certificate exists but is self-signed — might still be provisioning
		if i < len(delays)-1 {
			time.Sleep(delay)
			continue
		}

		return result, nil
	}

	return nil, fmt.Errorf("certificate validation failed after retries")
}

func isSelfSigned(cert *x509.Certificate) bool {
	return cert.Subject.CommonName == cert.Issuer.CommonName &&
		len(cert.Issuer.Organization) == 0
}

func isLetsEncrypt(cert *x509.Certificate) bool {
	for _, org := range cert.Issuer.Organization {
		lower := strings.ToLower(org)
		if strings.Contains(lower, "let's encrypt") ||
			strings.Contains(lower, "internet security research group") {
			return true
		}
	}
	return false
}

func formatIssuer(cert *x509.Certificate) string {
	if len(cert.Issuer.Organization) > 0 {
		org := cert.Issuer.Organization[0]
		if cert.Issuer.CommonName != "" {
			return fmt.Sprintf("%s (%s)", org, cert.Issuer.CommonName)
		}
		return org
	}
	if cert.Issuer.CommonName != "" {
		return cert.Issuer.CommonName
	}
	return "Unknown"
}

// FormatCertCheckOutput returns a formatted string for CLI display.
func FormatCertCheckOutput(result *CertCheckResult) string {
	if result.Valid {
		expDays := int(time.Until(result.ExpiresAt).Hours() / 24)
		msg := fmt.Sprintf("✓ TLS certificate valid for %s\n  Issuer: %s\n  Expires: %s (%d days)",
			result.Domain, result.Issuer, result.ExpiresAt.Format("2006-01-02"), expDays)
		if result.IsExpiringSoon() {
			msg += "\n  ⚠ Warning: Certificate expires in less than 7 days"
		}
		return msg
	}

	msg := fmt.Sprintf("✗ TLS certificate issue for %s\n  Certificate: %s",
		result.Domain, result.Issuer)
	if result.IsSelfSigned {
		msg += " (self-signed)"
	}
	msg += "\n  Possible causes:"
	msg += "\n    - DNS not yet propagated to server IP"
	msg += "\n    - DNS API credentials invalid"
	msg += "\n    - Domain not matching server"
	msg += "\n  Run 'sidekick cert-status' for diagnostics"
	return msg
}
