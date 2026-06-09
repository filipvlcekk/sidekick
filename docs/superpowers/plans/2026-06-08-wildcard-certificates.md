# Wildcard Certificates Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a server-level `Wildcard` certification mode so one Sidekick server can acquire and reuse a wildcard certificate for a single base domain such as `*.saola.cz`, while preserving today's default per-host mode.

**Architecture:** Extend `SidekickServer` with explicit certificate-mode metadata, teach `init` to configure and migrate that mode, render Traefik differently for wildcard servers, and make `launch`, `deploy`, and `cert-status` wildcard-aware through a shared domain-validation helper. Keep public DNS external to Sidekick, but make the UX and diagnostics clearly explain the expected DNS patterns.

**Tech Stack:** Go, Cobra, YAML config serialization, Traefik ACME DNS-01, existing Bubble Tea / Huh CLI prompts, `go test`.

---

## File Map

| File | Action | Responsibility |
| --- | --- | --- |
| `utils/types.go` | Modify | Add `CertificateMode` and `WildcardDomain` to `SidekickServer` |
| `utils/config_test.go` | Modify | Cover save/load expectations for the new server fields |
| `cmd/initialize/initialize.go` | Modify | Collect certification mode, validate wildcard domain input, support migration prompts, persist new server fields |
| `utils/scripts.go` | Modify | Render Traefik config for per-host vs wildcard mode |
| `utils/stages.go` | Modify if needed | Pass wildcard config into Traefik stage generation |
| `utils/stages_test.go` | Modify | Cover wildcard Traefik render paths |
| `utils/wildcard.go` | Create | Shared helpers for certificate-mode constants and hostname-in-zone validation |
| `utils/wildcard_test.go` | Create | Focused validation coverage for wildcard hostname rules |
| `cmd/launch/launch.go` | Modify | Enforce wildcard-zone guardrails, generate wildcard-mode app labels, and improve messaging |
| `cmd/deploy/deploy.go` | Modify | Enforce wildcard-zone guardrails, generate wildcard-mode app labels, and wildcard-aware post-deploy validation |
| `cmd/certstatus/certstatus.go` | Modify | Report wildcard coverage and separate cert coverage from DNS status |
| `cmd/certstatus/certstatus_test.go` | Modify | Add wildcard-aware diagnostics tests |
| `docs/superpowers/specs/2026-06-08-wildcard-certificates-design.md` | Reference | Product spec approved before implementation |

## Chunk 1: Configuration Model

### Task 1: Add explicit certificate-mode fields to server config

**Files:**
- Modify: `utils/types.go`
- Modify: `utils/config_test.go`

- [ ] **Step 1: Write a failing config serialization test**

Add a test that marshals a `SidekickConfig` containing:

```go
SidekickServer{
    Name:            "scvd",
    Address:         "204.10.194.116",
    CertificateMode: "wildcard",
    WildcardDomain:  "saola.cz",
}
```

Assert the YAML contains:

```yaml
certificateMode: wildcard
wildcardDomain: saola.cz
```

- [ ] **Step 2: Run the focused test and verify it fails**

Run:

```bash
GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./utils -run TestSidekickConfigSaveIncludesWildcardServerFields
```

Expected: FAIL because the fields do not exist yet.

- [ ] **Step 3: Add the new fields to `SidekickServer`**

Update:

```go
type SidekickServer struct {
    ...
    CertificateMode string `yaml:"certificateMode,omitempty"`
    WildcardDomain  string `yaml:"wildcardDomain,omitempty"`
}
```

Document in code comments or helper constants that an empty mode means legacy `per-host`.

- [ ] **Step 4: Re-run the focused test**

Run:

```bash
GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./utils -run TestSidekickConfigSaveIncludesWildcardServerFields
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add utils/types.go utils/config_test.go
git commit -m "feat: add certificate mode fields to server config"
```

### Task 1b: Preserve legacy config behavior and invariants

**Files:**
- Modify: `utils/config_test.go`
- Modify: `utils/wildcard.go` if invariant helpers live there

- [ ] **Step 1: Write a failing legacy-load test**

Load YAML that omits both new fields:

```yaml
servers:
  - name: scvd
    serveraddress: 204.10.194.116
```

Assert the loaded server normalizes to:

```go
NormalizeCertificateMode(server.CertificateMode) == CertificateModePerHost
server.WildcardDomain == ""
```

- [ ] **Step 2: Add invariant tests**

Cover cases such as:

```go
assert.Error(t, ValidateCertificateModeConfig("wildcard", ""))
assert.Error(t, ValidateCertificateModeConfig("per-host", "saola.cz"))
assert.NoError(t, ValidateCertificateModeConfig("wildcard", "saola.cz"))
```

- [ ] **Step 3: Run the focused tests**

Run:

```bash
GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./utils -run 'TestSidekickConfigLoadLegacyServerDefaultsToPerHost|TestValidateCertificateModeConfig'
```

Expected: FAIL.

- [ ] **Step 4: Implement normalization and invariant helpers**

Keep the normalization logic centralized so `init`, `launch`, `deploy`, and `cert-status` all agree on legacy behavior.

- [ ] **Step 5: Re-run the focused tests**

Run:

```bash
GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./utils -run 'TestSidekickConfigLoadLegacyServerDefaultsToPerHost|TestValidateCertificateModeConfig'
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add utils/config_test.go utils/wildcard.go utils/wildcard_test.go
git commit -m "test: cover legacy config and certificate mode invariants"
```

### Task 2: Centralize wildcard mode constants and domain validation rules

**Files:**
- Create: `utils/wildcard.go`
- Create: `utils/wildcard_test.go`

- [ ] **Step 1: Write failing tests for hostname matching**

Add cases for:

```go
assert.True(t, IsHostnameWithinWildcardDomain("uptimekuma.saola.cz", "saola.cz"))
assert.True(t, IsHostnameWithinWildcardDomain("grafana.saola.cz", "saola.cz"))
assert.False(t, IsHostnameWithinWildcardDomain("saola.cz", "saola.cz"))
assert.False(t, IsHostnameWithinWildcardDomain("foo.bar.saola.cz", "saola.cz"))
assert.False(t, IsHostnameWithinWildcardDomain("foo.example.com", "saola.cz"))
assert.False(t, IsHostnameWithinWildcardDomain("localhost", "saola.cz"))
```

- [ ] **Step 2: Run the new test to verify it fails**

Run:

```bash
GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./utils -run TestIsHostnameWithinWildcardDomain
```

Expected: FAIL because the helper does not exist.

- [ ] **Step 3: Implement a small helper module**

Create helpers such as:

```go
const (
    CertificateModePerHost = "per-host"
    CertificateModeWildcard = "wildcard"
)

func NormalizeCertificateMode(mode string) string
func IsHostnameWithinWildcardDomain(hostname, wildcardDomain string) bool
```

Keep the matching logic pure and side-effect free.

- [ ] **Step 4: Re-run the focused test**

Run:

```bash
GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./utils -run TestIsHostnameWithinWildcardDomain
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add utils/wildcard.go utils/wildcard_test.go
git commit -m "feat: add wildcard hostname validation helpers"
```

## Chunk 2: `sidekick init` UX and Persistence

### Task 3: Add certification-mode selection to `init`

**Files:**
- Modify: `cmd/initialize/initialize.go`
- Test: `cmd/initialize/initialize_test.go` (create if missing)

- [ ] **Step 1: Add tests for mode selection and persistence seams**

Extract a small pure helper if needed, then test:

```go
server := applyCertificateSettings(server, "wildcard", "saola.cz")
assert.Equal(t, "wildcard", server.CertificateMode)
assert.Equal(t, "saola.cz", server.WildcardDomain)
```

Also cover legacy/default behavior:

```go
server := applyCertificateSettings(server, "", "")
assert.Equal(t, "per-host", NormalizeCertificateMode(server.CertificateMode))
assert.Empty(t, server.WildcardDomain)
```

- [ ] **Step 2: Run the focused test and verify it fails**

Run:

```bash
GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./cmd/initialize -run TestApplyCertificateSettings
```

Expected: FAIL because the helper and mode handling do not exist.

- [ ] **Step 3: Implement the `init` mode prompt**

The interactive `init` flow must:

- ask `Certification mode`
- default to the current saved server mode when rerunning `init`, otherwise fall back to `Per-host`
- if `Wildcard`, ask for `Wildcard domain`
- print a short explanation that wildcard DNS is optional but recommended
- explain that users can either create per-app records or a wildcard record like `*.saola.cz`

Prefer a small helper for testability rather than embedding all logic in `Run`.

- [ ] **Step 4: Re-run the focused test**

Run:

```bash
GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./cmd/initialize -run TestApplyCertificateSettings
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/initialize/initialize.go cmd/initialize/initialize_test.go
git commit -m "feat: add certificate mode selection to init"
```

### Task 3b: Cover wildcard DNS guidance text in `init`

**Files:**
- Modify: `cmd/initialize/initialize.go`
- Modify: `cmd/initialize/initialize_test.go`

- [ ] **Step 1: Write a failing message-format test**

If needed, extract a helper such as:

```go
msg := wildcardInitGuidance("saola.cz")
assert.Contains(t, msg, "*.saola.cz")
assert.Contains(t, msg, "optional but recommended")
assert.Contains(t, msg, "per-app DNS records")
```

- [ ] **Step 2: Run the focused test**

Run:

```bash
GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./cmd/initialize -run TestWildcardInitGuidance
```

Expected: FAIL.

- [ ] **Step 3: Implement the guidance seam and wire it into `init`**

Keep the wording short but explicit so users do not confuse wildcard certs with wildcard DNS.

- [ ] **Step 4: Re-run the focused test**

Run:

```bash
GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./cmd/initialize -run TestWildcardInitGuidance
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/initialize/initialize.go cmd/initialize/initialize_test.go
git commit -m "feat: explain wildcard dns guidance during init"
```

### Task 4: Support migration of an existing DNS-01 server to wildcard mode

**Files:**
- Modify: `cmd/initialize/initialize.go`
- Test: `cmd/initialize/initialize_test.go`

- [ ] **Step 1: Write a failing migration decision test**

Cover a helper like:

```go
shouldRewriteTraefikForCertificateMode(
    existingMode: "per-host",
    requestedMode: "wildcard",
    existingWildcardDomain: "",
    requestedWildcardDomain: "saola.cz",
)
```

Expected outcome: rewrite required.

- [ ] **Step 2: Run the focused test**

Run:

```bash
GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./cmd/initialize -run TestShouldRewriteTraefikForCertificateMode
```

Expected: FAIL.

- [ ] **Step 3: Implement migration decision logic**

The migration helper should trigger Traefik rewrite when:

- `per-host -> wildcard`
- `wildcard -> per-host`
- `wildcard domain` changes

It should not rewrite when the requested mode and domain already match the stored server config.
Use the persisted local `SidekickServer` entry as the source of truth, not only remote Traefik inspection.

- [ ] **Step 4: Re-run the focused test**

Run:

```bash
GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./cmd/initialize -run TestShouldRewriteTraefikForCertificateMode
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/initialize/initialize.go cmd/initialize/initialize_test.go
git commit -m "feat: add wildcard migration logic to init"
```

## Chunk 3: Traefik Wildcard Rendering

### Task 5: Render Traefik differently for wildcard-mode servers

**Files:**
- Modify: `utils/scripts.go`
- Modify: `utils/stages.go`
- Modify: `utils/stages_test.go`

- [ ] **Step 1: Add failing render tests**

Add one per-host baseline test and one wildcard-mode test. The wildcard test should assert the rendered Traefik config includes both:

```text
saola.cz
*.saola.cz
```

and still includes:

```text
--certificatesresolvers.default.acme.dnschallenge.provider=digitalocean
--certificatesresolvers.default.acme.storage=/ssl-certs/acme.json
```

- [ ] **Step 2: Run the focused test**

Run:

```bash
GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./utils -run 'TestTraefikComposeUsesCanonicalACMEDir|TestTraefikComposeIncludesWildcardDomains'
```

Expected: FAIL on the new wildcard assertion.

- [ ] **Step 3: Refactor Traefik config generation to accept certificate mode**

Avoid string hacking directly in `initialize.go`. Prefer a render helper that takes:

```go
type TraefikTLSConfig struct {
    Email           string
    DNSProvider     DNSProvider
    EnvVars         map[string]string
    CertificateMode string
    WildcardDomain  string
}
```

Per-host mode should keep today's output.
Wildcard mode should add the extra TLS-domain configuration needed to trigger acquisition of `saola.cz` and `*.saola.cz`.

- [ ] **Step 4: Re-run the focused test**

Run:

```bash
GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./utils -run 'TestTraefikComposeUsesCanonicalACMEDir|TestTraefikComposeIncludesWildcardDomains'
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add utils/scripts.go utils/stages.go utils/stages_test.go
git commit -m "feat: render wildcard traefik configuration"
```

## Chunk 4: Launch and Deploy Guardrails

### Task 6: Generate wildcard-safe app labels during `launch`

**Files:**
- Modify: `cmd/launch/launch.go`
- Test: `cmd/launch/launch_test.go`

- [ ] **Step 1: Write a failing label-generation test**

Extract a helper if necessary, then cover both modes:

```go
labels := buildTraefikLabels("uptimekuma", "uptimekuma.saola.cz", 3001, SidekickServer{
    CertificateMode: "wildcard",
    WildcardDomain:  "saola.cz",
})
assert.NotContains(t, labels, "traefik.http.routers.uptimekuma.tls.certresolver=default")
assert.Contains(t, labels, "traefik.http.routers.uptimekuma.tls=true")
```

- [ ] **Step 2: Run the focused test**

Run:

```bash
GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./cmd/launch -run TestBuildTraefikLabels
```

Expected: FAIL.

- [ ] **Step 3: Implement mode-aware Traefik labels**

In wildcard mode:

- keep `traefik.http.routers.<app>.tls=true`
- omit `traefik.http.routers.<app>.tls.certresolver=default`

In per-host mode:

- keep today's existing label set unchanged

- [ ] **Step 4: Add a failing wildcard boundary test**

Cover:

```go
err := validateAppDomainForServer("foo.example.com", SidekickServer{
    CertificateMode: "wildcard",
    WildcardDomain:  "saola.cz",
})
assert.EqualError(t, err, "app domain foo.example.com is outside wildcard domain saola.cz")
```

- [ ] **Step 5: Run the focused boundary test**

Run:

```bash
GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./cmd/launch -run TestValidateAppDomainForServer
```

Expected: FAIL.

- [ ] **Step 6: Implement launch-time validation**

Call the helper before writing `sidekick.yml` and before generating the remote compose file.

Per-host mode should skip the wildcard-zone check.

- [ ] **Step 7: Re-run the focused tests**

Run:

```bash
GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./cmd/launch -run 'TestBuildTraefikLabels|TestValidateAppDomainForServer'
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add cmd/launch/launch.go cmd/launch/launch_test.go
git commit -m "feat: make launch wildcard-aware"
```

### Task 7: Generate wildcard-safe app labels during `deploy`

**Files:**
- Modify: `cmd/deploy/deploy.go`
- Modify: `cmd/deploy/deploy_test.go`

- [ ] **Step 1: Write a failing deploy label-generation test**

Add a focused test that wildcard-mode deploy compose generation omits:

```go
"traefik.http.routers.uptimekuma.tls.certresolver=default"
```

while per-host mode still includes it.

- [ ] **Step 2: Run the focused test**

Run:

```bash
GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./cmd/deploy -run TestBuildDockerComposeFile
```

Expected: FAIL.

- [ ] **Step 3: Implement mode-aware deploy compose generation**

Keep the existing shared label structure, but omit the per-app certresolver label in wildcard mode.

- [ ] **Step 4: Add a failing deploy guardrail test**

Add a focused test for the same validation path used during deploy.

- [ ] **Step 5: Run the focused boundary test**

Run:

```bash
GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./cmd/deploy -run TestValidateAppDomainForServer
```

Expected: FAIL.

- [ ] **Step 6: Implement deploy-time validation**

Run the same shared domain validation before image build or remote deploy work begins.

- [ ] **Step 7: Re-run the focused tests**

Run:

```bash
GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./cmd/deploy -run 'TestBuildDockerComposeFile|TestValidateAppDomainForServer'
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add cmd/deploy/deploy.go cmd/deploy/deploy_test.go
git commit -m "feat: make deploy wildcard-aware"
```

## Chunk 5: Wildcard-Aware Diagnostics

### Task 8: Make `cert-status` understand wildcard-mode servers

**Files:**
- Modify: `cmd/certstatus/certstatus.go`
- Modify: `cmd/certstatus/certstatus_test.go`
- Modify: `utils/certcheck.go` only if a small helper is needed for reuse

- [ ] **Step 1: Write failing `cert-status` tests**

Add tests for:

- wildcard app hostname under the configured zone
- hostname outside the configured zone
- `acme.json` containing wildcard/base-domain coverage

Example seam:

```go
status := summarizeWildcardCoverage("uptimekuma.saola.cz", "saola.cz", acmeJSON)
assert.True(t, status.DomainWithinZone)
assert.True(t, status.ACMEEntryFound)
```

- [ ] **Step 2: Run the focused tests**

Run:

```bash
GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./cmd/certstatus -run 'TestAcmeEntryExists|TestSummarizeWildcardCoverage'
```

Expected: FAIL because the wildcard helper does not exist.

- [ ] **Step 3: Implement wildcard-aware status helpers**

Keep the command body thin. Prefer extracting helpers for:

- wildcard zone coverage
- ACME wildcard/base-domain entry detection
- formatting explicit wildcard-aware user messages

Do not regress the current separation between TLS validity and public DNS warnings.

- [ ] **Step 4: Re-run the focused tests**

Run:

```bash
GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./cmd/certstatus -run 'TestAcmeEntryExists|TestSummarizeWildcardCoverage'
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/certstatus/certstatus.go cmd/certstatus/certstatus_test.go utils/certcheck.go
git commit -m "feat: add wildcard-aware certificate diagnostics"
```

### Task 9: Improve wildcard-mode TLS and DNS post-deploy messaging

**Files:**
- Modify: `cmd/launch/launch.go`
- Modify: `cmd/deploy/deploy.go`
- Modify: tests in `cmd/launch/launch_test.go` and `cmd/deploy/deploy_test.go` if message seams exist

- [ ] **Step 1: Add a failing formatting test**

If a formatting seam exists or is extracted, assert wildcard-mode warnings mention both supported DNS setups:

- per-host DNS records
- wildcard DNS such as `*.saola.cz`

- [ ] **Step 2: Run the focused test**

Run:

```bash
GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./cmd/launch ./cmd/deploy -run TestFormatDNSCheckOutputForWildcardMode
```

Expected: FAIL.

- [ ] **Step 3: Implement minimal messaging changes**

Do not change validation semantics. Only improve the user-facing explanation based on `server.CertificateMode`.

- [ ] **Step 4: Re-run the focused test**

Run:

```bash
GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./cmd/launch ./cmd/deploy -run TestFormatDNSCheckOutputForWildcardMode
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/launch/launch.go cmd/deploy/deploy.go cmd/launch/launch_test.go cmd/deploy/deploy_test.go
git commit -m "feat: clarify wildcard dns guidance in deploy output"
```

## Chunk 6: End-to-End Verification and Docs

### Task 10: Verify migration and wildcard server behavior end to end at the repo level

**Files:**
- Modify: tests only if gaps remain

- [ ] **Step 1: Run focused package tests first**

Run:

```bash
GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./utils ./cmd/initialize ./cmd/launch ./cmd/deploy ./cmd/certstatus
```

Expected: PASS.

- [ ] **Step 2: Run the full suite**

Run:

```bash
GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./...
```

Expected: PASS.

- [ ] **Step 3: Verify wildcard acquisition against a clean Traefik state**

Use either an integration seam or a documented manual verification path, but do not skip this step. At minimum, prove that a fresh wildcard-mode Traefik config can obtain coverage for `saola.cz` and `*.saola.cz` without relying on legacy per-app `tls.certresolver` labels.

Recommended manual check:

1. provision a clean test server with wildcard mode enabled
2. ensure `traefik/ssl-certs/acme.json` starts empty
3. run the Traefik setup
4. confirm `acme.json` contains `saola.cz` and `*.saola.cz`
5. confirm a newly launched app in-zone succeeds without a per-app `tls.certresolver` label

Capture the exact command sequence in the implementation notes or PR description.

- [ ] **Step 4: Run a full build**

Run:

```bash
GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go build ./...
```

Expected: PASS.

- [ ] **Step 5: Update docs if implementation differs from the approved spec**

Only touch:

- `docs/superpowers/specs/2026-06-08-wildcard-certificates-design.md`

if there was a deliberate implementation adjustment.

- [ ] **Step 6: Commit final polish**

```bash
git add docs/superpowers/specs/2026-06-08-wildcard-certificates-design.md
git commit -m "docs: align wildcard certificate spec with implementation details"
```

## Notes for the Implementer

- Reuse the current DNS-01 provider model; do not introduce DNS record CRUD.
- Keep the new logic server-centric. Do not add app-level certificate mode in v1.
- Prefer extracting small pure helpers for testability instead of expanding large Cobra command bodies.
- Preserve backwards compatibility for legacy config files by normalizing empty `CertificateMode` to `per-host`.
- Avoid broad Traefik refactors. This feature should be an incremental extension of the existing config generator.

## Verification Checklist

- `sidekick init` offers `Certification mode`
- `Wildcard` mode requires `Wildcard domain`
- wildcard servers render Traefik config that requests `saola.cz` and `*.saola.cz`
- `launch` fails early for out-of-zone hostnames
- `deploy` fails early for out-of-zone hostnames
- `cert-status` distinguishes wildcard coverage, TLS validity, and public DNS state
- existing per-host servers continue to work unchanged

Plan complete and saved to `docs/superpowers/plans/2026-06-08-wildcard-certificates.md`. Ready to execute?
