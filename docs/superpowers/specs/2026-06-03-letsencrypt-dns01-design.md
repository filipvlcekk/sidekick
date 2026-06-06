# Let's Encrypt DNS-01 Challenge — Design Spec

## Problem

Traefik currently uses HTTP-01 ACME challenge for certificate issuance. When the challenge fails (port 80 blocked, DNS not propagated, firewall issues), Traefik silently falls back to serving its default self-signed certificate (`TRAEFIK DEFAULT CERT`). Users have no visibility into this failure and end up with insecure connections.

## Solution

Replace HTTP-01 with DNS-01 challenge (Traefik-native), add multi-provider support, and implement post-deploy TLS certificate validation with clear CLI diagnostics.

## Requirements

- DNS-01 challenge only (no HTTP-01)
- Multi-provider support (Cloudflare, AWS Route 53, DigitalOcean, Hetzner, GoDaddy)
- Post-deploy certificate validation with CLI output
- Robust error handling with retry and diagnostics
- Migration path for existing HTTP-01 setups
- Secure credential storage

## Architecture

### Current State

```
sidekick init → email → Traefik with HTTP-01 → port 80 must be reachable for challenge
```

### New State

```
sidekick init → email + DNS provider + API credentials → Traefik with DNS-01 → no port 80 dependency for ACME
```

Port 80 remains for HTTP→HTTPS redirect but is not required for certificate issuance.

## Components

### 1. DNS Provider Registry (`utils/providers.go`)

Extensible map of supported DNS providers with their Traefik identifiers and required environment variables.

```go
type DNSProvider struct {
    Name        string   // Display name (e.g. "Cloudflare")
    TraefikName string   // Traefik provider identifier (e.g. "cloudflare")
    EnvVars     []string // Required env vars (e.g. ["CF_DNS_API_TOKEN"])
    Description string   // Help text for the user
}
```

Supported providers:

| Provider     | Traefik Name   | Required Env Vars                                      |
|--------------|----------------|--------------------------------------------------------|
| Cloudflare   | cloudflare     | `CF_DNS_API_TOKEN`                                     |
| AWS Route 53 | route53        | `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION` |
| DigitalOcean | digitalocean   | `DO_AUTH_TOKEN`                                        |
| Hetzner      | hetzner        | `HETZNER_API_KEY`                                      |
| GoDaddy      | godaddy        | `GODADDY_API_KEY`, `GODADDY_API_SECRET`                |

### 2. Modified `sidekick init` Flow

New steps after email collection:

1. Interactive select list for DNS provider
2. Dynamic credential prompts based on selected provider
3. Store provider name in sidekick config
4. Deploy credentials securely to server

CLI flags for non-interactive use:
```
--dns-provider cloudflare
--dns-env CF_DNS_API_TOKEN=xxxxx
```

Multiple env vars are passed by repeating the flag:
```
sidekick init --dns-provider route53 \
  --dns-env AWS_ACCESS_KEY_ID=AKIA... \
  --dns-env AWS_SECRET_ACCESS_KEY=secret... \
  --dns-env AWS_REGION=us-east-1
```

### 3. Traefik Docker Compose Template

Replace `httpchallenge` with `dnschallenge`:

```yaml
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
```

### 4. Post-Deploy TLS Validation (`utils/certcheck.go`)

```go
type CertCheckResult struct {
    Domain    string
    Valid     bool
    Issuer    string
    ExpiresAt time.Time
    IsSelfSigned bool
    Error     error
}

func ValidateTLSCert(domain string) (*CertCheckResult, error)
```

Behavior:
1. TLS handshake on `domain:443`
2. Verify certificate is NOT self-signed (Issuer != Subject)
3. Verify Issuer organization contains "Let's Encrypt" or ISRG root (checking `Issuer.Organization` field rather than specific intermediate names, as these rotate over time)
4. Check expiration (warn if < 7 days)
5. Retry with exponential backoff: 3 attempts at 30s/60s/120s intervals (accounts for Traefik processing + DNS propagation delay)

Integration points:
- Called automatically after `sidekick launch` and `sidekick deploy`
- Results displayed in CLI

Success output:
```
✓ TLS certificate valid for app.example.com
  Issuer: Let's Encrypt (R10)
  Expires: 2026-09-01
```

Failure output:
```
✗ TLS certificate issue for app.example.com
  Certificate is self-signed (TRAEFIK DEFAULT CERT)
  Possible causes:
    - DNS not yet propagated to server IP
    - DNS API credentials invalid
    - Domain not matching server
  Run 'sidekick cert-status' for diagnostics
```

### 5. New Command: `sidekick cert-status`

```go
// cmd/certstatus/certstatus.go
func NewCertStatusCmd() *cobra.Command
```

**Arguments and flags:**
- `sidekick cert-status` — checks all deployed apps on the current server context
- `sidekick cert-status --app <name>` — checks a specific app
- `--server <name>` — target a specific server (defaults to current context)

**Behavior:**
1. Resolves target server from current context (or `--server` flag)
2. SSHs into the server
3. Lists running app containers with Traefik labels to discover domains
4. For each domain:
   a. Reads Traefik logs (`docker logs traefik-service 2>&1 | grep -i acme`) for ACME errors
   b. Checks `acme.json` content — parses JSON to see if domain has a certificate entry
   c. Performs TLS handshake on the domain from the local machine
5. Outputs per-domain summary

**Output format:**
```
Certificate Status for server "production"
──────────────────────────────────────────

app.example.com
  ✓ Certificate: Let's Encrypt (R10)
  ✓ Expires: 2026-09-01 (89 days)
  ✓ ACME logs: no errors

api.example.com
  ✗ Certificate: TRAEFIK DEFAULT CERT (self-signed)
  ✗ ACME logs: "unable to generate a certificate... DNS problem: NXDOMAIN"
  → Suggestion: Verify DNS A record for api.example.com points to 1.2.3.4
```

**Error scenarios in output:**
- No containers with Traefik labels → "No deployed apps found on this server"
- SSH connection fails → standard sidekick SSH error handling
- Domain unreachable from local machine → "Could not connect to domain:443 — check DNS resolution"

### 6. Config Changes (`SidekickServer` struct)

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

New field: `DNSProvider` — stores the Traefik provider name (e.g. "cloudflare"). Credentials are NOT stored in the config file.

### 7. Credential Security

- Credentials stored in `.env` file on server at `~/traefik/.env` with `chmod 600`
- Docker Compose references credentials via `env_file: .env`
- Sidekick config only stores provider name, never credentials
- On re-deploy, credentials are not re-transmitted (already on server)
- If user needs to update credentials: `sidekick init` detects existing setup and offers update

### 8. Migration (Existing HTTP-01 Users)

When `sidekick init` runs on a server with existing Traefik HTTP-01 setup:
1. Detect existing `docker-compose.yml` with `httpchallenge`
2. Prompt: "Existing HTTP-01 setup detected. Migrate to DNS-01? (y/n)"
3. If yes: collect DNS provider info, rewrite compose, create `.env`, restart Traefik
4. Existing certificates in `acme.json` remain valid until expiration; Traefik renews via DNS-01

**Switching DNS providers** (existing DNS-01 → different DNS-01):
1. `sidekick init` detects existing DNS-01 setup with a different provider
2. Prompt: "Current DNS provider is cloudflare. Switch to route53? (y/n)"
3. If yes: collect new provider credentials, rewrite `.env`, update compose `dnschallenge.provider`, restart Traefik
4. Existing certificates remain valid; renewal uses new provider

## Data Flow

```
User runs `sidekick init`
  → selects DNS provider
  → enters API credentials
  → sidekick SSHs to VPS
  → creates ~/traefik/.env (credentials, chmod 600)
  → creates ~/traefik/docker-compose.yml (DNS-01 config)
  → starts Traefik container

User runs `sidekick launch`
  → deploys app with Traefik labels
  → Traefik detects new route
  → Traefik requests cert via DNS-01 (creates TXT record via API)
  → Let's Encrypt validates TXT record
  → Certificate issued and stored in acme.json
  → sidekick runs TLS validation
  → reports success/failure to user
```

## Error Handling

| Scenario | Detection | User Action |
|----------|-----------|-------------|
| Invalid API credentials | Traefik logs show auth error | Re-run `sidekick init` to update credentials |
| DNS not propagated | TLS check shows self-signed after retries | Wait and run `sidekick cert-status` |
| Wrong provider selected | Traefik logs show provider error | Re-run `sidekick init` |
| Rate limited by Let's Encrypt | Traefik logs show 429 | Wait 1 hour, certs will auto-retry |
| acme.json corrupted | cert-status shows empty/invalid | Delete and restart Traefik |

## Testing Strategy

- Unit tests for `DNSProvider` registry and validation logic
- Unit tests for `ValidateTLSCert` with mocked TLS connections
- Integration test for Docker Compose template generation
- Manual testing with real DNS provider (Cloudflare recommended for dev)

## Out of Scope

- Wildcard certificates (possible with DNS-01 but not in initial implementation)
- Multiple cert resolvers per server
- Custom CA support
- Webhook notifications
