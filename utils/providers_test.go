package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetDNSProvider_Cloudflare(t *testing.T) {
	provider, err := GetDNSProvider("cloudflare")
	assert.NoError(t, err)
	assert.Equal(t, "Cloudflare", provider.Name)
	assert.Equal(t, "cloudflare", provider.TraefikName)
	assert.Equal(t, []string{"CF_DNS_API_TOKEN"}, provider.EnvVars)
}

func TestGetDNSProvider_Route53(t *testing.T) {
	provider, err := GetDNSProvider("route53")
	assert.NoError(t, err)
	assert.Equal(t, "AWS Route 53", provider.Name)
	assert.Equal(t, "route53", provider.TraefikName)
	assert.Contains(t, provider.EnvVars, "AWS_ACCESS_KEY_ID")
	assert.Contains(t, provider.EnvVars, "AWS_SECRET_ACCESS_KEY")
	assert.Contains(t, provider.EnvVars, "AWS_REGION")
}

func TestGetDNSProvider_Unknown(t *testing.T) {
	_, err := GetDNSProvider("nonexistent")
	assert.Error(t, err)
}

func TestGetAllDNSProviders(t *testing.T) {
	providers := GetAllDNSProviders()
	assert.GreaterOrEqual(t, len(providers), 5)
}
