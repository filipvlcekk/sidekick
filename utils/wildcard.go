package utils

import (
	"fmt"
	"regexp"
	"strconv"
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

	if !areValidDNSLabels(hostnameLabels) || !areValidDNSLabels(wildcardLabels) {
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
	if len(labels) < 2 || !areValidDNSLabels(labels) {
		return false
	}

	return true
}

func ValidateAppDomainForServer(domain string, server SidekickServer) error {
	normalizedServer := server
	NormalizeSidekickServer(&normalizedServer)

	if normalizedServer.CertificateMode != CertificateModeWildcard {
		return nil
	}

	if !IsHostnameWithinWildcardDomain(domain, normalizedServer.WildcardDomain) {
		return fmt.Errorf("app domain %s is outside wildcard domain %s", domain, normalizedServer.WildcardDomain)
	}

	return nil
}

func BuildAppTraefikLabels(serviceName, domain string, port uint64, server SidekickServer) ([]string, error) {
	if err := ValidateAppDomainForServer(domain, server); err != nil {
		return nil, err
	}

	labels := []string{
		"traefik.enable=true",
		fmt.Sprintf("traefik.http.routers.%s.rule=Host(`%s`)", serviceName, domain),
		fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port=%s", serviceName, strconv.FormatUint(port, 10)),
		fmt.Sprintf("traefik.http.routers.%s.tls=true", serviceName),
		fmt.Sprintf("traefik.http.routers.%s.tls.certresolver=default", serviceName),
		"traefik.docker.network=sidekick",
	}

	return labels, nil
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

func areValidDNSLabels(labels []string) bool {
	for _, label := range labels {
		if !dnsLabelPattern.MatchString(label) {
			return false
		}
	}

	return true
}
