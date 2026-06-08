package utils

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"strings"
	"time"
)

type tlsConnection interface {
	ConnectionState() tls.ConnectionState
	Close() error
}

var dialTLSWithDialer = func(dialer *net.Dialer, network, address string, config *tls.Config) (tlsConnection, error) {
	return tls.DialWithDialer(dialer, network, address, config)
}

var lookupPublicIPs = func(domain string) ([]net.IP, error) {
	resolvers := []string{"1.1.1.1:53", "8.8.8.8:53"}
	var lastErr error

	for _, resolverAddr := range resolvers {
		resolver := &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
				d := net.Dialer{Timeout: 5 * time.Second}
				return d.DialContext(ctx, "udp", resolverAddr)
			},
		}

		ipAddrs, err := resolver.LookupIPAddr(context.Background(), domain)
		if err != nil {
			lastErr = err
			continue
		}

		ips := make([]net.IP, 0, len(ipAddrs))
		for _, ipAddr := range ipAddrs {
			ips = append(ips, ipAddr.IP)
		}
		return ips, nil
	}

	return nil, lastErr
}

// CertCheckResult holds the result of a TLS certificate validation.
type CertCheckResult struct {
	Domain       string
	Valid        bool
	Issuer       string
	ExpiresAt    time.Time
	IsSelfSigned bool
}

// DNSCheckResult describes whether public DNS resolves a domain to the expected server IP.
type DNSCheckResult struct {
	Domain          string
	ExpectedIP      string
	ResolvedIPs     []string
	MatchesExpected bool
	LookupErr       error
}

// IsExpiringSoon returns true if the certificate expires within 7 days.
func (r CertCheckResult) IsExpiringSoon() bool {
	return time.Until(r.ExpiresAt) < 7*24*time.Hour
}

// ValidateTLSCert connects to domain:443 and validates the TLS certificate.
func ValidateTLSCert(domain string) (*CertCheckResult, error) {
	return ValidateTLSCertAtAddress(domain, net.JoinHostPort(domain, "443"))
}

// ValidateTLSCertAtAddress connects to an explicit address while preserving
// the requested domain as TLS SNI and the reported result domain.
func ValidateTLSCertAtAddress(domain, address string) (*CertCheckResult, error) {
	conn, err := dialTLSWithDialer(
		&net.Dialer{Timeout: 10 * time.Second},
		"tcp",
		address,
		&tls.Config{
			ServerName:         domain,
			InsecureSkipVerify: true, // We inspect the cert ourselves
		},
	)
	if err != nil {
		return nil, fmt.Errorf("TLS connection to %s failed: %w", address, err)
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
// Retries 3 times at 30s, 60s, 120s intervals. Prints progress to stdout.
func ValidateTLSCertWithRetry(domain string) (*CertCheckResult, error) {
	return ValidateTLSCertWithRetryAtAddress(domain, net.JoinHostPort(domain, "443"))
}

// ValidateTLSCertWithRetryAtAddress retries certificate validation against a
// specific address while keeping the requested domain as SNI.
func ValidateTLSCertWithRetryAtAddress(domain, address string) (*CertCheckResult, error) {
	delays := []time.Duration{30 * time.Second, 60 * time.Second, 120 * time.Second}

	for i, delay := range delays {
		result, err := ValidateTLSCertAtAddress(domain, address)
		if err != nil {
			if i < len(delays)-1 {
				fmt.Printf("  Certificate not ready yet, retrying in %ds...\n", int(delay.Seconds()))
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
			fmt.Printf("  Certificate is self-signed, retrying in %ds...\n", int(delay.Seconds()))
			time.Sleep(delay)
			continue
		}

		return result, nil
	}

	return nil, fmt.Errorf("certificate validation failed after retries")
}

// CheckPublicDNS resolves the domain through the system resolver and compares the
// returned IP set against the expected server IP.
func CheckPublicDNS(domain, expectedIP string) DNSCheckResult {
	result := DNSCheckResult{
		Domain:     domain,
		ExpectedIP: expectedIP,
	}

	ips, err := lookupPublicIPs(domain)
	if err != nil {
		result.LookupErr = err
		return result
	}

	seen := make(map[string]struct{}, len(ips))
	for _, ip := range ips {
		ipStr := ip.String()
		if _, ok := seen[ipStr]; ok {
			continue
		}
		seen[ipStr] = struct{}{}
		result.ResolvedIPs = append(result.ResolvedIPs, ipStr)
		if ipStr == expectedIP {
			result.MatchesExpected = true
		}
	}

	return result
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

// FormatDNSCheckOutput returns a CLI-friendly summary of public DNS state.
func FormatDNSCheckOutput(result DNSCheckResult) string {
	if result.LookupErr != nil {
		return fmt.Sprintf("⚠ Public DNS lookup failed for %s: %v", result.Domain, result.LookupErr)
	}

	if result.MatchesExpected {
		return fmt.Sprintf("✓ Public DNS resolves %s -> %s", result.Domain, strings.Join(result.ResolvedIPs, ", "))
	}

	if len(result.ResolvedIPs) == 0 {
		return fmt.Sprintf("⚠ Public DNS for %s returned no IPs", result.Domain)
	}

	return fmt.Sprintf("⚠ Public DNS resolves %s elsewhere: %s (expected %s)",
		result.Domain, strings.Join(result.ResolvedIPs, ", "), result.ExpectedIP)
}
