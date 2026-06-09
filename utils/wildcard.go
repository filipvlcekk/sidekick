package utils

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	CertificateModePerHost  = "per-host"
	CertificateModeWildcard = "wildcard"
)

var dnsLabelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

// NormalizeCertificateMode treats an empty mode as the legacy per-host default.
func NormalizeCertificateMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", CertificateModePerHost:
		return CertificateModePerHost
	case CertificateModeWildcard:
		return CertificateModeWildcard
	default:
		return strings.ToLower(strings.TrimSpace(mode))
	}
}

func NormalizeSidekickConfig(config *SidekickConfig) {
	for i := range config.Servers {
		NormalizeSidekickServer(&config.Servers[i])
	}
}

func NormalizeSidekickServer(server *SidekickServer) {
	server.CertificateMode = NormalizeCertificateMode(server.CertificateMode)
	server.WildcardDomain = normalizeWildcardName(server.WildcardDomain)
}

func ValidateCertificateModeConfig(mode, wildcardDomain string) error {
	normalizedMode := NormalizeCertificateMode(mode)
	normalizedDomain := normalizeWildcardName(wildcardDomain)

	switch normalizedMode {
	case CertificateModePerHost:
		if normalizedDomain != "" {
			return fmt.Errorf("wildcard domain is only allowed in %q mode", CertificateModeWildcard)
		}
		return nil
	case CertificateModeWildcard:
		if normalizedDomain == "" {
			return fmt.Errorf("wildcard domain is required in %q mode", CertificateModeWildcard)
		}
		if !IsValidWildcardDomain(normalizedDomain) {
			return fmt.Errorf("wildcard domain %q must be a DNS zone like example.com", wildcardDomain)
		}
		return nil
	default:
		return fmt.Errorf("unsupported certificate mode %q", mode)
	}
}

func IsHostnameWithinWildcardDomain(hostname, wildcardDomain string) bool {
	hostnameLabels := splitDomainLabels(hostname)
	wildcardLabels := splitDomainLabels(wildcardDomain)

	if len(hostnameLabels) != len(wildcardLabels)+1 {
		return false
	}

	for i := range wildcardLabels {
		if hostnameLabels[i+1] != wildcardLabels[i] {
			return false
		}
	}

	return true
}

func IsValidWildcardDomain(domain string) bool {
	labels := splitDomainLabels(domain)
	if len(labels) < 2 {
		return false
	}

	for _, label := range labels {
		if !dnsLabelPattern.MatchString(label) {
			return false
		}
	}

	return true
}

func normalizeWildcardName(name string) string {
	return strings.Trim(strings.ToLower(strings.TrimSpace(name)), ".")
}

func splitDomainLabels(name string) []string {
	normalized := normalizeWildcardName(name)
	if normalized == "" {
		return nil
	}

	labels := strings.Split(normalized, ".")
	for _, label := range labels {
		if label == "" {
			return nil
		}
	}

	return labels
}
