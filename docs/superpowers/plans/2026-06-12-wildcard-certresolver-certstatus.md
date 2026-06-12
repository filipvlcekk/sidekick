# Wildcard Certresolver and Cert-Status Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make launch/deploy always generate a usable TLS certresolver label for wildcard-hosted apps and make cert-status finish reliably when SSH diagnostics produce empty stdout.

**Architecture:** Keep Traefik wildcard domain configuration unchanged, but make app-router TLS label generation explicit and unconditional for `tls.certresolver=default`. In `cert-status`, add a small non-blocking output helper so SSH commands with no stdout are interpreted as empty diagnostics instead of causing a hang.

**Tech Stack:** Go, Cobra CLI, testify, GitNexus-indexed Go codebase

---

## Chunk 1: TLS Label Generation

### Task 1: Update app router TLS label generation

**Files:**
- Modify: `utils/wildcard.go`
- Test: `cmd/launch/launch_test.go`
- Test: `cmd/deploy/deploy_test.go`

- [ ] **Step 1: Write the failing tests**

Update the wildcard-mode tests so they require:

```go
assert.Contains(t, service.Labels, "traefik.http.routers.uptimekuma.tls.certresolver=default")
```

in both:
- `TestBuildDockerServiceOmitsPerAppCertresolverInWildcardMode`
- `TestBuildDockerComposeFileOmitsPerAppCertresolverInWildcardMode`

Rename the tests to reflect the intended behavior.

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./cmd/launch ./cmd/deploy
```

Expected: wildcard-mode assertions fail because the generated labels omit `tls.certresolver=default`.

- [ ] **Step 3: Write the minimal implementation**

In `utils/wildcard.go`, update `BuildAppTraefikLabels` so the returned labels always include:

```go
fmt.Sprintf("traefik.http.routers.%s.tls=true", serviceName),
fmt.Sprintf("traefik.http.routers.%s.tls.certresolver=default", serviceName),
```

Keep wildcard-domain validation unchanged.

- [ ] **Step 4: Run tests to verify they pass**

Run:

```bash
go test ./cmd/launch ./cmd/deploy
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add utils/wildcard.go cmd/launch/launch_test.go cmd/deploy/deploy_test.go
git commit -m "fix: always add certresolver to app routers"
```

## Chunk 2: Cert-Status Empty Output Handling

### Task 2: Make cert-status resilient to empty stdout

**Files:**
- Modify: `cmd/certstatus/certstatus.go`
- Test: `cmd/certstatus/certstatus_test.go`

- [ ] **Step 1: Write the failing tests**

Add focused tests for a helper that safely consumes command stdout:

```go
func TestReadFirstLineOrEmptyReturnsEmptyForNilChannel(t *testing.T)
func TestReadFirstLineOrEmptyReturnsEmptyWhenChannelClosesWithoutData(t *testing.T)
func TestReadFirstLineOrEmptyReturnsLineWhenPresent(t *testing.T)
```

Also add a small test for app listing/domain parsing behavior if needed by the chosen helper.

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./cmd/certstatus
```

Expected: FAIL because the helper does not exist yet.

- [ ] **Step 3: Write the minimal implementation**

Add a helper in `cmd/certstatus/certstatus.go` that:
- returns `""` for nil channel
- returns `""` when the channel closes without data
- returns the first received line when present

Use it for:
- ACME log retrieval
- `acme.json` retrieval
- app list retrieval
- per-app domain retrieval

Preserve existing fatal behavior only for required command errors, not empty stdout.

- [ ] **Step 4: Run tests to verify they pass**

Run:

```bash
go test ./cmd/certstatus
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/certstatus/certstatus.go cmd/certstatus/certstatus_test.go
git commit -m "fix: prevent cert-status hangs on empty diagnostics"
```

## Chunk 3: Integrated Verification

### Task 3: Verify the full regression surface

**Files:**
- Verify only

- [ ] **Step 1: Run focused package tests**

Run:

```bash
go test ./cmd/launch ./cmd/deploy ./cmd/certstatus
```

Expected: PASS

- [ ] **Step 2: Run broader regression tests**

Run:

```bash
go test ./utils ./cmd/...
```

Expected: PASS

- [ ] **Step 3: Review git diff**

Run:

```bash
git diff -- utils/wildcard.go cmd/launch/launch_test.go cmd/deploy/deploy_test.go cmd/certstatus/certstatus.go cmd/certstatus/certstatus_test.go
```

Expected: only intended TLS label and cert-status resilience changes

- [ ] **Step 4: Run GitNexus change detection**

Run:

```bash
npx gitnexus detect-changes --scope unstaged
```

Expected: changes map only to wildcard label generation and cert-status diagnostics behavior

- [ ] **Step 5: Commit**

```bash
git add utils/wildcard.go cmd/launch/launch_test.go cmd/deploy/deploy_test.go cmd/certstatus/certstatus.go cmd/certstatus/certstatus_test.go docs/superpowers/specs/2026-06-12-wildcard-certresolver-certstatus-design.md docs/superpowers/plans/2026-06-12-wildcard-certresolver-certstatus.md
git commit -m "fix: repair wildcard certresolver generation and cert-status hangs"
```
