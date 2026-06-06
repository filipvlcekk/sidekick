# Let's Encrypt DNS-01 Challenge Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace HTTP-01 with DNS-01 ACME challenge in Traefik, add multi-provider support, and implement post-deploy TLS certificate validation.

**Architecture:** Traefik-native DNS-01 challenge with provider credentials passed as env vars. Sidekick collects provider info during `init`, generates the appropriate Docker Compose and `.env` file on the server. A new `certcheck` module validates certificates after deploy, and a new `cert-status` command provides diagnostics.

**Tech Stack:** Go 1.24, Cobra CLI, Charm (huh for select UI, bubbletea for TUI), Traefik v3.6.1 DNS-01 ACME, `crypto/tls` for certificate validation.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `utils/providers.go` (create) | DNS provider registry — types and provider list |
| `utils/providers_test.go` (create) | Tests for provider registry |
| `utils/scripts.go` (modify) | New Traefik Docker Compose template with DNS-01 |
| `utils/stages.go` (modify) | Updated `GetTraefikStage` accepting provider config |
| `utils/types.go` (modify) | Add `DNSProvider` field to `SidekickServer` |
| `utils/certcheck.go` (create) | TLS certificate validation logic |
| `utils/certcheck_test.go` (create) | Tests for cert validation |
| `cmd/initialize/initialize.go` (modify) | DNS provider selection flow in init |
| `cmd/certstatus/certstatus.go` (create) | `cert-status` command implementation |
| `cmd/root.go` (modify) | Register `cert-status` command |
| `cmd/launch/launch.go` (modify) | Post-deploy cert validation call |
| `cmd/deploy/deploy.go` (modify) | Post-deploy cert validation call |

---

## Chunk 1: DNS Provider Registry

### Task 1: Create DNS provider types and registry

**Files:**
- Create: `utils/providers.go`
- Create: `utils/providers_test.go`

- [ ] **Step 1: Write the failing test for provider lookup**

```go
// utils/providers_test.go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./utils/ -run TestGetDNSProvider -v`
Expected: FAIL — `GetDNSProvider` undefined

- [ ] **Step 3: Implement provider registry**

```go
// utils/providers.go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./utils/ -run TestGetDNSProvider -v && go test ./utils/ -run TestGetAllDNSProviders -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add utils/providers.go utils/providers_test.go
git commit -m "feat: add DNS provider registry for ACME DNS-01 challenge"
```

---

### Task 2: Add DNSProvider field to SidekickServer

**Files:**
- Modify: `utils/types.go:101-109`

- [ ] **Step 1: Add the DNSProvider field**

In `utils/types.go`, add `DNSProvider` field to `SidekickServer` struct:

```go
type SidekickServer struct {
	Name        string `yaml:"name"`
	Address     string `yaml:"serveraddress"`
	Distro      string `yaml:"distro"`
	PlatformId  string `yaml:"platformid"`
	CertEmail   string `yaml:"certemail"`
	DNSProvider string `yaml:"dnsprovider"`
	PublicKey   string `yaml:"publickey"`
	SecretKey   string `yaml:"secretkey"`
}
```

- [ ] **Step 2: Verify build passes**

Run: `go build ./...`
Expected: SUCCESS — new field is backward compatible (empty string for existing configs)

- [ ] **Step 3: Commit**

```bash
git add utils/types.go
git commit -m "feat: add DNSProvider field to SidekickServer config"
```

---

## Chunk 2: Traefik DNS-01 Template + Init Command Integration

### Task 3: Create DNS-01 Traefik template and update init command together

This task modifies the Traefik template AND its caller in init simultaneously to keep the build green at every commit.

**Files:**
- Modify: `utils/scripts.go:184-214`
- Modify: `utils/stages.go:65-79`
- Modify: `cmd/initialize/initialize.go`

- [ ] **Step 1: Replace TraefikDockerComposeFile with DNS-01 version**

In `utils/scripts.go`, replace the `TraefikDockerComposeFile` variable:

```go
var TraefikDockerComposeFile = `
services:
  traefik-service:
    image: traefik:v3.6.1
    command:
      - --api.insecure=false
      - --entrypoints.web.address=:80
      - --entrypoints.web.http.redirections.entryPoint.to=websecure
      - --entrypoints.web.http.redirections.entryPoint.scheme=https
      - --entrypoints.websecure.address=:443
      - --entrypoints.websecure.http.tls.certresolver=default
      - --providers.docker.exposedbydefault=false
      - --certificatesresolvers.default.acme.email=$EMAIL
      - --certificatesresolvers.default.acme.storage=/ssl-certs/acme.json
      - --certificatesresolvers.default.acme.dnschallenge.provider=$DNS_PROVIDER
      - --certificatesresolvers.default.acme.dnschallenge.resolvers=1.1.1.1:53,8.8.8.8:53
    env_file:
      - .env
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ./traefik/ssl-certs/:/ssl-certs/
    networks:
      - sidekick

networks:
  sidekick:
    external: true
`
```

- [ ] **Step 2: Update GetTraefikStage to accept provider and credentials**

In `utils/stages.go`, modify `GetTraefikStage`:

```go
func GetTraefikStage(email string, provider DNSProvider, envVars map[string]string) CommandsStage {
	// Build .env file content from credentials
	envFileContent := ""
	for key, value := range envVars {
		envFileContent += fmt.Sprintf("%s=%s\n", key, value)
	}

	// Replace placeholders in compose template
	compose := TraefikDockerComposeFile
	compose = strings.Replace(compose, "$EMAIL", email, 1)
	compose = strings.Replace(compose, "$DNS_PROVIDER", provider.TraefikName, 1)

	return CommandsStage{
		SpinnerSuccessMessage: "Successfully setup Traefik",
		SpinnerFailMessage:    "Something went wrong setting up Traefik on your VPS",
		Commands: []string{
			"mkdir -p traefik",
			fmt.Sprintf("echo '%s' > ./traefik/.env", envFileContent),
			"chmod 600 ./traefik/.env",
			fmt.Sprintf("echo '%s' > ./traefik/docker-compose.yml", compose),
			"mkdir -p ./traefik/ssl-certs/",
			"touch ./traefik/ssl-certs/acme.json",
			"chmod 600 ./traefik/ssl-certs/acme.json",
			"sudo docker network create sidekick || true",
			"cd traefik && sudo docker compose -p sidekick up -d",
		},
	}
}
```

- [ ] **Step 3: Add DNS provider selection, credentials collection, and stage6Traefik update to init command**

In `cmd/initialize/initialize.go`, add `"github.com/charmbracelet/huh"` to the import block (the `render` package is already imported).

After the `certEmail` prompt block (after line 193), insert:

```go
		// DNS provider selection
		var selectedProvider utils.DNSProvider
		dnsProviderFlag, _ := cmd.Flags().GetString("dns-provider")
		dnsEnvFlags, _ := cmd.Flags().GetStringArray("dns-env")

		if dnsProviderFlag != "" {
			var err error
			selectedProvider, err = utils.GetDNSProvider(dnsProviderFlag)
			if err != nil {
				log.Fatalf("Unknown DNS provider: %s. Use one of: cloudflare, route53, digitalocean, hetzner, godaddy", dnsProviderFlag)
			}
		} else {
			// Interactive provider selection
			providers := utils.GetAllDNSProviders()
			options := make([]huh.Option[utils.DNSProvider], 0, len(providers))
			for _, p := range providers {
				options = append(options, huh.NewOption(fmt.Sprintf("%s — %s", p.Name, p.Description), p))
			}

			form := huh.NewForm(
				huh.NewGroup(
					huh.NewSelect[utils.DNSProvider]().
						Title("Select your DNS provider for Let's Encrypt certificates").
						Options(options...).
						Value(&selectedProvider),
				),
			)
			if err := form.Run(); err != nil {
				log.Fatalf("DNS provider selection failed: %s", err)
			}
		}

		// Collect DNS credentials
		dnsEnvVars := make(map[string]string)
		if len(dnsEnvFlags) > 0 {
			// Parse from flags: --dns-env KEY=VALUE
			for _, env := range dnsEnvFlags {
				parts := strings.SplitN(env, "=", 2)
				if len(parts) != 2 {
					log.Fatalf("Invalid --dns-env format: %s (expected KEY=VALUE)", env)
				}
				dnsEnvVars[parts[0]] = parts[1]
			}
		} else {
			// Interactive credential prompts
			for _, envVar := range selectedProvider.EnvVars {
				value := render.GenerateTextQuestion(fmt.Sprintf("Enter %s", envVar), "", "")
				if value == "" {
					log.Fatalf("%s is required for %s", envVar, selectedProvider.Name)
				}
				dnsEnvVars[envVar] = value
			}
		}

		// Validate all required env vars are provided
		for _, required := range selectedProvider.EnvVars {
			if _, ok := dnsEnvVars[required]; !ok {
				log.Fatalf("Missing required env var %s for provider %s", required, selectedProvider.Name)
			}
		}
```

After `sidekickServer.CertEmail = certEmail` (line 213), add:

```go
		sidekickServer.DNSProvider = selectedProvider.TraefikName
```

- [ ] **Step 4: Update stage6Traefik function to accept provider info**

Replace the existing `stage6Traefik` function with this intermediate version (Task 7 will expand it with migration logic):

```go
func stage6Traefik(client *ssh.Client, email string, provider utils.DNSProvider, envVars map[string]string, skipPrompts bool, p *tea.Program) error {
	traefikSetup := false
	outChan, _, err := utils.RunCommand(client, `[ -d "traefik" ] && echo "1" || echo "0"`)
	if err == nil {
		output := <-outChan
		if output == "1" {
			traefikSetup = true
		}
	}

	if !traefikSetup {
		traefikStage := utils.GetTraefikStage(email, provider, envVars)
		if err := utils.RunCommandsWithTUIHook(client, traefikStage.Commands, p); err != nil {
			return err
		}
	}
	return nil
}
```

Update the goroutine call to `stage6Traefik`:

```go
			if err := stage6Traefik(sidekickClient, certEmail, selectedProvider, dnsEnvVars, skipPromptsFlag, p); err != nil {
```

- [ ] **Step 5: Register new CLI flags in init()**

Add to the `init()` function at the bottom of the file:

```go
	InitCmd.Flags().String("dns-provider", "", "DNS provider for ACME DNS-01 challenge (cloudflare, route53, digitalocean, hetzner, godaddy)")
	InitCmd.Flags().StringArray("dns-env", []string{}, "DNS provider environment variable (KEY=VALUE, can be repeated)")
```

- [ ] **Step 6: Verify build passes**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add utils/scripts.go utils/stages.go cmd/initialize/initialize.go
git commit -m "feat: switch to DNS-01 challenge and add provider selection to init"
```

---

## Chunk 3: TLS Certificate Validation

### Task 4: Implement TLS certificate validation module

**Files:**
- Create: `utils/certcheck.go`
- Create: `utils/certcheck_test.go`

- [ ] **Step 1: Write failing tests for cert validation**

```go
// utils/certcheck_test.go
package utils

import (
	"crypto/x509"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestIsSelfSigned_True(t *testing.T) {
	// Subject == Issuer means self-signed
	cert := &x509.Certificate{
		Subject: x509.Name{CommonName: "TRAEFIK DEFAULT CERT"},
		Issuer:  x509.Name{CommonName: "TRAEFIK DEFAULT CERT"},
	}
	assert.True(t, isSelfSigned(cert))
}

func TestIsSelfSigned_False(t *testing.T) {
	cert := &x509.Certificate{
		Subject: x509.Name{CommonName: "example.com"},
		Issuer:  x509.Name{Organization: []string{"Let's Encrypt"}},
	}
	assert.False(t, isSelfSigned(cert))
}

func TestIsLetsEncrypt_True(t *testing.T) {
	cert := &x509.Certificate{
		Issuer: x509.Name{Organization: []string{"Let's Encrypt"}},
	}
	assert.True(t, isLetsEncrypt(cert))
}

func TestIsLetsEncrypt_ISRG(t *testing.T) {
	cert := &x509.Certificate{
		Issuer: x509.Name{Organization: []string{"Internet Security Research Group"}},
	}
	assert.True(t, isLetsEncrypt(cert))
}

func TestIsLetsEncrypt_False(t *testing.T) {
	cert := &x509.Certificate{
		Issuer: x509.Name{Organization: []string{"DigiCert Inc"}},
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./utils/ -run "TestIsSelfSigned|TestIsLetsEncrypt|TestCertCheckResult" -v`
Expected: FAIL — types not defined

- [ ] **Step 3: Implement cert validation module**

```go
// utils/certcheck.go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./utils/ -run "TestIsSelfSigned|TestIsLetsEncrypt|TestCertCheckResult" -v`
Expected: PASS

- [ ] **Step 5: Verify build**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add utils/certcheck.go utils/certcheck_test.go
git commit -m "feat: add TLS certificate validation module with retry support"
```

---

## Chunk 4: Post-Deploy Validation Integration

### Task 5: Add cert validation to launch and deploy commands

**Files:**
- Modify: `cmd/launch/launch.go` (after successful deploy, approximately after the app is running)
- Modify: `cmd/deploy/deploy.go` (same pattern)

- [ ] **Step 1: Add a helper function for post-deploy validation output**

Create a shared helper in `utils/certcheck.go` (append):

```go
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
```

- [ ] **Step 2: Integrate into launch command**

In `cmd/launch/launch.go`, after `p.Run()` returns successfully (after the TUI completes), add a synchronous cert validation call:

```go
		// Post-deploy TLS validation — runs synchronously after TUI completes
		fmt.Println("\nValidating TLS certificate...")
		result, err := utils.ValidateTLSCertWithRetry(appDomain)
		if err != nil {
			fmt.Printf("\n%s\n", fmt.Sprintf("⚠ Could not validate TLS certificate for %s: %s", appDomain, err))
		} else {
			fmt.Printf("\n%s\n", utils.FormatCertCheckOutput(result))
		}
```

Place this AFTER `p.Run()` returns and the success message is printed. This runs synchronously so the process won't exit before validation completes.

- [ ] **Step 3: Integrate into deploy command**

Apply the same pattern in `cmd/deploy/deploy.go` — after deploy completes successfully, run the cert validation.

- [ ] **Step 4: Verify build passes**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/launch/launch.go cmd/deploy/deploy.go utils/certcheck.go
git commit -m "feat: add post-deploy TLS certificate validation to launch and deploy"
```

---

## Chunk 5: cert-status Command

### Task 6: Implement cert-status command

**Files:**
- Create: `cmd/certstatus/certstatus.go`
- Modify: `cmd/root.go`

- [ ] **Step 1: Create the cert-status command**

```go
// cmd/certstatus/certstatus.go
package certstatus

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/mightymoud/sidekick/utils"
	"github.com/spf13/cobra"
)

var CertStatusCmd = &cobra.Command{
	Use:   "cert-status",
	Short: "Check TLS certificate status for deployed apps",
	Long:  `Connects to your VPS, inspects Traefik logs for ACME errors, and validates TLS certificates for all deployed apps.`,
	Run: func(cmd *cobra.Command, args []string) {
		config, err := utils.GetSidekickConfigFromCmdContext(cmd)
		if err != nil {
			log.Fatalf("Failed to load config: %s", err)
		}

		serverName, _ := cmd.Flags().GetString("server")
		appFilter, _ := cmd.Flags().GetString("app")

		// Resolve server
		var server utils.SidekickServer
		if serverName != "" {
			server, err = config.FindServer(serverName)
			if err != nil {
				log.Fatalf("Server '%s' not found in config", serverName)
			}
		} else {
			ctx, err := config.FindContext(config.CurrentContext)
			if err != nil {
				log.Fatalf("No current context set. Use --server flag or run 'sidekick init'")
			}
			server, err = config.FindServer(ctx.Server)
			if err != nil {
				log.Fatalf("Server for context '%s' not found", config.CurrentContext)
			}
		}

		// SSH into server
		client, err := utils.Login(server.Address, "sidekick")
		if err != nil {
			log.Fatalf("SSH connection failed: %s", err)
		}
		defer client.Close()

		fmt.Printf("Certificate Status for server \"%s\" (%s)\n", server.Name, server.Address)
		fmt.Println(strings.Repeat("─", 50))
		fmt.Println()

		// Get Traefik ACME logs
		outChan, _, err := utils.RunCommand(client, "cd traefik && sudo docker compose -p sidekick logs traefik-service 2>&1 | grep -i 'acme\\|certificate\\|error' | tail -20")
		var acmeLogs string
		if err == nil {
			acmeLogs = <-outChan
		}

		// Check acme.json for per-domain cert entries
		outChan, _, err = utils.RunCommand(client, `cat traefik/ssl-certs/acme.json 2>/dev/null || echo "{}"`)
		var acmeJSON string
		if err == nil {
			acmeJSON = <-outChan
		}

		// List deployed apps (containers with traefik labels)
		outChan, _, err = utils.RunCommand(client, `docker ps --format '{{.Labels}}' | grep -oP 'traefik\.http\.routers\.\K[^.]+(?=\.rule)' | sort -u`)
		if err != nil {
			log.Fatalf("Failed to list apps: %s", err)
		}
		appsOutput := <-outChan
		apps := strings.Split(strings.TrimSpace(appsOutput), "\n")

		if len(apps) == 0 || (len(apps) == 1 && apps[0] == "") {
			fmt.Println("No deployed apps found on this server")
			return
		}

		// Get domains for each app
		for _, app := range apps {
			if appFilter != "" && app != appFilter {
				continue
			}

			outChan, _, err = utils.RunCommand(client, fmt.Sprintf(`docker ps --format '{{.Labels}}' | grep -oP 'traefik\.http\.routers\.%s\.rule=Host\(\x60\K[^\x60]+' | head -1`, app))
			if err != nil {
				fmt.Printf("%s\n  ✗ Could not determine domain\n\n", app)
				continue
			}
			domain := strings.TrimSpace(<-outChan)
			if domain == "" {
				fmt.Printf("%s\n  ✗ Could not determine domain\n\n", app)
				continue
			}

			fmt.Printf("%s\n", domain)

			// Check if domain has entry in acme.json
			if acmeJSON != "{}" && acmeJSON != "" {
				if strings.Contains(acmeJSON, domain) {
					fmt.Printf("  ✓ ACME storage: certificate entry found\n")
				} else {
					fmt.Printf("  ✗ ACME storage: no certificate entry for this domain\n")
				}
			}

			// Validate TLS cert
			result, err := utils.ValidateTLSCert(domain)
			if err != nil {
				fmt.Printf("  ✗ Connection failed: %s\n", err)
			} else if result.Valid {
				expDays := int(result.ExpiresAt.Sub(time.Now()).Hours() / 24)
				fmt.Printf("  ✓ Certificate: %s\n", result.Issuer)
				fmt.Printf("  ✓ Expires: %s (%d days)\n", result.ExpiresAt.Format("2006-01-02"), expDays)
			} else {
				fmt.Printf("  ✗ Certificate: %s", result.Issuer)
				if result.IsSelfSigned {
					fmt.Printf(" (self-signed)")
				}
				fmt.Println()
			}

			// Check ACME logs for this domain
			if acmeLogs != "" {
				relevantLogs := filterLogsForDomain(acmeLogs, domain)
				if relevantLogs != "" {
					fmt.Printf("  ✗ ACME logs: %s\n", relevantLogs)
				} else {
					fmt.Printf("  ✓ ACME logs: no errors\n")
				}
			}

			fmt.Println()
		}
	},
}

func filterLogsForDomain(logs, domain string) string {
	lines := strings.Split(logs, "\n")
	var relevant []string
	for _, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, strings.ToLower(domain)) &&
			(strings.Contains(lower, "error") || strings.Contains(lower, "unable")) {
			relevant = append(relevant, strings.TrimSpace(line))
		}
	}
	if len(relevant) > 0 {
		return relevant[len(relevant)-1] // most recent error
	}
	return ""
}

func init() {
	CertStatusCmd.Flags().String("server", "", "Target server name (defaults to current context)")
	CertStatusCmd.Flags().String("app", "", "Check only a specific app")
}
```

- [ ] **Step 2: Register command in root.go**

In `cmd/root.go`, add import and registration:

```go
import (
	// existing imports...
	"github.com/mightymoud/sidekick/cmd/certstatus"
)
```

In `func init()`, add:

```go
	rootCmd.AddCommand(certstatus.CertStatusCmd)
```

- [ ] **Step 3: Verify build passes**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/certstatus/certstatus.go cmd/root.go
git commit -m "feat: add cert-status command for TLS diagnostics"
```

---

## Chunk 6: Migration Support

### Task 7: Add migration detection to init command

**Files:**
- Modify: `cmd/initialize/initialize.go` (the `stage6Traefik` function)

- [ ] **Step 1: Update stage6Traefik to handle migration**

Replace the existing `stage6Traefik` function to detect existing setups:

```go
func stage6Traefik(client *ssh.Client, email string, provider utils.DNSProvider, envVars map[string]string, skipPrompts bool, p *tea.Program) error {
	traefikSetup := false
	existingHTTP01 := false
	existingDNS01Provider := ""

	outChan, _, err := utils.RunCommand(client, `[ -d "traefik" ] && echo "1" || echo "0"`)
	if err == nil {
		output := <-outChan
		if output == "1" {
			traefikSetup = true
			// Check if existing setup uses HTTP-01 or DNS-01
			outChan, _, err = utils.RunCommand(client, `grep -o "httpchallenge" traefik/docker-compose.yml 2>/dev/null || echo ""`)
			if err == nil {
				output = <-outChan
				if strings.Contains(output, "httpchallenge") {
					existingHTTP01 = true
				}
			}
			// Check for existing DNS provider
			outChan, _, err = utils.RunCommand(client, `grep -oP 'dnschallenge\.provider=\K\S+' traefik/docker-compose.yml 2>/dev/null || echo ""`)
			if err == nil {
				output = <-outChan
				existingDNS01Provider = strings.TrimSpace(output)
			}
		}
	}

	if !traefikSetup {
		// Fresh install
		traefikStage := utils.GetTraefikStage(email, provider, envVars)
		if err := utils.RunCommandsWithTUIHook(client, traefikStage.Commands, p); err != nil {
			return err
		}
		return nil
	}

	// Migration: HTTP-01 → DNS-01
	if existingHTTP01 {
		if !skipPrompts {
			confirm := render.GenerateTextQuestion("Existing HTTP-01 setup detected. Migrate to DNS-01? (y/n)", "y", "")
			if strings.ToLower(confirm) != "y" {
				fmt.Println("Skipping Traefik migration")
				return nil
			}
		}
		traefikStage := utils.GetTraefikMigrationStage(email, provider, envVars)
		if err := utils.RunCommandsWithTUIHook(client, traefikStage.Commands, p); err != nil {
			return err
		}
		return nil
	}

	// Migration: DNS-01 provider change
	if existingDNS01Provider != "" && existingDNS01Provider != provider.TraefikName {
		if !skipPrompts {
			confirm := render.GenerateTextQuestion(
				fmt.Sprintf("Current DNS provider is %s. Switch to %s? (y/n)", existingDNS01Provider, provider.TraefikName), "y", "")
			if strings.ToLower(confirm) != "y" {
				fmt.Println("Skipping DNS provider change")
				return nil
			}
		}
		traefikStage := utils.GetTraefikMigrationStage(email, provider, envVars)
		if err := utils.RunCommandsWithTUIHook(client, traefikStage.Commands, p); err != nil {
			return err
		}
		return nil
	}

	// Already configured with same provider — skip
	return nil
}
```

Update the goroutine call to pass `skipPromptsFlag`:

```go
			if err := stage6Traefik(sidekickClient, certEmail, selectedProvider, dnsEnvVars, skipPromptsFlag, p); err != nil {
```

- [ ] **Step 2: Add GetTraefikMigrationStage to stages.go**

In `utils/stages.go`, add:

```go
// GetTraefikMigrationStage replaces existing Traefik config and restarts.
func GetTraefikMigrationStage(email string, provider DNSProvider, envVars map[string]string) CommandsStage {
	envFileContent := ""
	for key, value := range envVars {
		envFileContent += fmt.Sprintf("%s=%s\n", key, value)
	}

	compose := TraefikDockerComposeFile
	compose = strings.Replace(compose, "$EMAIL", email, 1)
	compose = strings.Replace(compose, "$DNS_PROVIDER", provider.TraefikName, 1)

	return CommandsStage{
		SpinnerSuccessMessage: "Successfully migrated Traefik to DNS-01",
		SpinnerFailMessage:    "Something went wrong migrating Traefik",
		Commands: []string{
			fmt.Sprintf("echo '%s' > ./traefik/.env", envFileContent),
			"chmod 600 ./traefik/.env",
			fmt.Sprintf("echo '%s' > ./traefik/docker-compose.yml", compose),
			"cd traefik && sudo docker compose -p sidekick down",
			"cd traefik && sudo docker compose -p sidekick up -d",
		},
	}
}
```

- [ ] **Step 3: Verify build passes**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/initialize/initialize.go utils/stages.go
git commit -m "feat: add migration support for HTTP-01 to DNS-01 and provider switching"
```

---

## Chunk 7: Final Integration and Verification

### Task 8: End-to-end build verification and cleanup

**Files:**
- All modified files

- [ ] **Step 1: Run full build**

Run: `go build ./...`
Expected: PASS — all packages compile

- [ ] **Step 2: Run all tests**

Run: `go test ./... -v`
Expected: PASS — all existing and new tests pass

- [ ] **Step 3: Run vet and lint**

Run: `go vet ./...`
Expected: No issues

- [ ] **Step 4: Verify CLI help text**

Run: `go run main.go init --help`
Expected: Shows `--dns-provider` and `--dns-env` flags

Run: `go run main.go cert-status --help`
Expected: Shows cert-status command with `--server` and `--app` flags

- [ ] **Step 5: Final commit if any fixes needed**

```bash
git add -A
git commit -m "fix: address build/test issues from DNS-01 integration"
```
