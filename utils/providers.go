package utils

import "fmt"

// DNSProvider represents a supported DNS provider for ACME DNS-01 challenge.
type DNSProvider struct {
	Name        string   // Display name (e.g. "Cloudflare")
	TraefikName string   // Traefik provider identifier
	EnvVars     []string // Required environment variables
	Description string   // Help text for the user
}

var dnsProviders = []DNSProvider{
	{
		Name:        "Cloudflare",
		TraefikName: "cloudflare",
		EnvVars:     []string{"CF_DNS_API_TOKEN"},
		Description: "Cloudflare DNS API token (Zone:DNS:Edit permission)",
	},
	{
		Name:        "AWS Route 53",
		TraefikName: "route53",
		EnvVars:     []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_REGION"},
		Description: "AWS IAM credentials with Route 53 access",
	},
	{
		Name:        "DigitalOcean",
		TraefikName: "digitalocean",
		EnvVars:     []string{"DO_AUTH_TOKEN"},
		Description: "DigitalOcean personal access token",
	},
	{
		Name:        "Hetzner",
		TraefikName: "hetzner",
		EnvVars:     []string{"HETZNER_API_KEY"},
		Description: "Hetzner DNS API token",
	},
	{
		Name:        "GoDaddy",
		TraefikName: "godaddy",
		EnvVars:     []string{"GODADDY_API_KEY", "GODADDY_API_SECRET"},
		Description: "GoDaddy API key and secret",
	},
}

// GetDNSProvider returns a provider by its Traefik name.
func GetDNSProvider(traefikName string) (DNSProvider, error) {
	for _, p := range dnsProviders {
		if p.TraefikName == traefikName {
			return p, nil
		}
	}
	return DNSProvider{}, fmt.Errorf("unknown DNS provider: %s", traefikName)
}

// GetAllDNSProviders returns all supported DNS providers.
func GetAllDNSProviders() []DNSProvider {
	return dnsProviders
}
