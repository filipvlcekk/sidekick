# TLS DNS-01 Remediation Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the verified DNS-01/TLS regressions so Traefik persists ACME certificates in the expected location, `cert-status` reports accurate data, and the repo returns to a verifiable green build/test state.

**Architecture:** Keep the existing DNS-01 approach, but remove the current path divergence by defining one canonical host-side ACME directory and using it consistently in setup, migration, compose generation, and diagnostics. Restore trustworthy verification by fixing the render package vet failure, isolating the `sops` dependency in tests, and adding targeted regression coverage around the new TLS flow.

**Tech Stack:** Go 1.24, Cobra CLI, Traefik v3.6.1, `crypto/tls`, `testify`, shell-based VPS provisioning.

---

## Problem Summary

### Verified issue 1: ACME storage path mismatch

**What is wrong**

- `utils/stages.go` creates `./traefik/ssl-certs/acme.json`.
- `cmd/certstatus/certstatus.go` reads `traefik/ssl-certs/acme.json`.
- `utils/scripts.go` mounts `./traefik/ssl/:/ssl-certs/`.

This means the setup path and the mounted path are not the same host directory. Traefik may write certificates into one location while setup and diagnostics inspect another.

**Impact**

- Initial provisioning can leave an unused `acme.json` on disk.
- `sidekick cert-status` can incorrectly report that no ACME entry exists.
- Migration behavior is harder to reason about because host storage is ambiguous.

**Fix direction**

- Introduce one canonical host directory, preferably `./traefik/ssl-certs/`, because setup and diagnostics already use it.
- Use that directory everywhere: setup, compose template, migration, diagnostics, and any related docs/tests.

### Verified issue 2: repo-level verification is not green

**What is wrong**

- `GOCACHE=$(pwd)/.gocache go test ./...` currently fails in `render/utils.go` because `Printf` is called with non-constant format strings.
- The same command also fails in `utils/utils_test.go` because `TestHandleEnvFile` requires `sops` to exist on the machine.

**Impact**

- We cannot honestly claim the repo is passing full verification.
- Future TLS fixes would still ship under a broken verification baseline.

**Fix direction**

- Replace incorrect `Printf` usage with `Print`, `Println`, or `Printf("%s", ...)`.
- Refactor the env encryption path so tests can verify behavior without requiring the external `sops` binary, or explicitly split integration coverage from unit coverage.

### Verified issue 3: missing regression coverage for the new TLS flow

**What is wrong**

- Existing tests only cover helper logic in `utils/providers_test.go` and `utils/certcheck_test.go`.
- There is no regression test guarding the ACME path contract.
- There is no focused test for `cert-status` behavior when ACME storage exists or is missing.

**Impact**

- The exact bug we found would be easy to reintroduce.
- Future refactors to setup or diagnostics have no guardrail.

**Fix direction**

- Add focused tests around Traefik config generation and ACME storage path consistency.
- Add at least one unit-level seam for testing `cert-status` parsing logic without a real SSH target.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `utils/scripts.go` | Canonical Traefik compose template and ACME mount path |
| `utils/stages.go` | Setup/migration commands creating the ACME host directory and file |
| `cmd/certstatus/certstatus.go` | Runtime diagnostics that read ACME storage and report cert issues |
| `render/utils.go` | Fix repo-wide verification blocker caused by incorrect `Printf` usage |
| `utils/utils.go` | Env-file encryption seam currently hard-bound to external `sops` |
| `utils/utils_test.go` | Adjust `HandleEnvFile` tests to remove implicit host dependency |
| `utils/providers_test.go` | Existing provider tests; keep green while repo verification is restored |
| `utils/certcheck_test.go` | Existing TLS helper tests; keep green while repo verification is restored |
| `utils/stages_test.go` (create) | Regression tests for ACME path consistency and Traefik config generation |
| `cmd/certstatus/certstatus_test.go` (create if practical) | Focused tests for parsing/formatting seams in `cert-status` |
| `docs/superpowers/specs/2026-06-03-letsencrypt-dns01-design.md` | Update only if implementation details change from the documented path |
| `docs/superpowers/plans/2026-06-03-letsencrypt-dns01.md` | Update only if it remains the implementation source of truth after remediation |

---

## Chunk 1: Fix ACME Storage Path Contract

### Task 1: Add regression coverage for the host ACME path

**Files:**
- Create: `utils/stages_test.go`
- Read for reference: `utils/scripts.go`
- Read for reference: `utils/stages.go`

- [ ] **Step 1: Write a failing test for the compose mount path**

Add a test that asserts the generated Traefik compose template mounts the same host directory the setup stage creates.

```go
func TestTraefikComposeUsesCanonicalACMEDir(t *testing.T) {
	assert.Contains(t, TraefikDockerComposeFile, "./traefik/ssl-certs/:/ssl-certs/")
}
```

- [ ] **Step 2: Write a failing test for stage setup commands**

Add a test that asserts setup commands create the same host directory and `acme.json`.

```go
func TestTraefikStageCreatesCanonicalACMEDir(t *testing.T) {
	stage := GetTraefikStage("ops@example.com", DNSProvider{TraefikName: "cloudflare"}, map[string]string{"CF_DNS_API_TOKEN": "x"})
	assert.Contains(t, stage.Commands, "mkdir -p ./traefik/ssl-certs/")
	assert.Contains(t, stage.Commands, "touch ./traefik/ssl-certs/acme.json")
}
```

- [ ] **Step 3: Run targeted tests to confirm they fail**

Run: `GOCACHE=$(pwd)/.gocache go test ./utils -run 'TestTraefikComposeUsesCanonicalACMEDir|TestTraefikStageCreatesCanonicalACMEDir' -v`

Expected: FAIL because the compose template still uses `./traefik/ssl/`.

- [ ] **Step 4: Commit the failing tests if working TDD-first**

```bash
git add utils/stages_test.go
git commit -m "test: add regression coverage for Traefik ACME path contract"
```

### Task 2: Make setup, compose, and diagnostics use one canonical path

**Files:**
- Modify: `utils/scripts.go:186-209`
- Modify: `utils/stages.go:99-115`
- Modify: `cmd/certstatus/certstatus.go:62-67`

- [ ] **Step 1: Introduce a single canonical host directory constant**

Prefer a constant in `utils/stages.go` or a shared `utils` file:

```go
const TraefikACMEHostDir = "./traefik/ssl-certs/"
```

If the compose template cannot consume constants directly, centralize the literal in a small builder/helper instead of duplicating it.

- [ ] **Step 2: Update the compose mount path**

Change:

```yaml
- ./traefik/ssl/:/ssl-certs/
```

to:

```yaml
- ./traefik/ssl-certs/:/ssl-certs/
```

- [ ] **Step 3: Keep setup commands aligned with the same path**

Ensure these remain exactly aligned with the compose mount:

```go
"mkdir -p ./traefik/ssl-certs/",
"touch ./traefik/ssl-certs/acme.json",
"chmod 600 ./traefik/ssl-certs/acme.json",
```

- [ ] **Step 4: Verify `cert-status` reads the same path**

Keep or update the diagnostic read command so it matches the canonical directory:

```go
`cat traefik/ssl-certs/acme.json 2>/dev/null || echo "{}"`
```

- [ ] **Step 5: Run targeted tests to confirm they pass**

Run: `GOCACHE=$(pwd)/.gocache go test ./utils -run 'TestTraefikComposeUsesCanonicalACMEDir|TestTraefikStageCreatesCanonicalACMEDir' -v`

Expected: PASS

- [ ] **Step 6: Run package build for impacted code**

Run: `GOCACHE=$(pwd)/.gocache go build ./utils ./cmd/certstatus`

Expected: SUCCESS

- [ ] **Step 7: Commit**

```bash
git add utils/scripts.go utils/stages.go cmd/certstatus/certstatus.go utils/stages_test.go
git commit -m "fix: align Traefik ACME storage path across setup and diagnostics"
```

---

## Chunk 2: Restore Trustworthy Repo Verification

### Task 3: Fix render package vet/build failure

**Files:**
- Modify: `render/utils.go:94-96`

- [ ] **Step 1: Replace incorrect `Printf` calls with explicit print semantics**

Current code passes already-rendered strings into `Printf`. Replace with one of:

```go
pterm.DefaultCenter.Print(pterm.FgYellow.Sprintf("This is the ASCII art and fingerprint of your VPS's public key at %s", hostname))
pterm.DefaultCenter.Print(pterm.FgYellow.Sprint("Please confirm you want to continue with the connection"))
pterm.DefaultCenter.Print(pterm.FgYellow.Sprint("Sidekick will add this host/key pair to known_hosts"))
```

or:

```go
pterm.DefaultCenter.Printf("%s", pterm.FgYellow.Sprintf(...))
```

- [ ] **Step 2: Run targeted verification**

Run: `GOCACHE=$(pwd)/.gocache go test ./render -v`

Expected: package builds successfully, even if it has no tests.

- [ ] **Step 3: Commit**

```bash
git add render/utils.go
git commit -m "fix: remove invalid pterm Printf usage in render package"
```

### Task 4: Remove implicit `sops` host dependency from unit tests

**Files:**
- Modify: `utils/utils.go:221-255`
- Modify: `utils/utils_test.go:14-42`

- [ ] **Step 1: Add a seam for the encryption command**

Refactor `HandleEnvFile` so the `sops` execution is injectable. One minimal pattern:

```go
var runSopsEncrypt = func(publicKey, envFileName, outputFile string) error {
	cmd := exec.Command("sops", "encrypt", "--output-type", "dotenv", "--age", publicKey, envFileName)
	outfile, err := os.Create(outputFile)
	if err != nil {
		return err
	}
	defer outfile.Close()
	cmd.Stdout = outfile
	return cmd.Run()
}
```

Then `HandleEnvFile` calls:

```go
if err := runSopsEncrypt(publicKey, fmt.Sprintf("./%s", envFileName), "encrypted.env"); err != nil {
	return err
}
```

- [ ] **Step 2: Update the unit test to stub the seam**

In `utils/utils_test.go`, temporarily replace `runSopsEncrypt` with a stub that writes deterministic output and returns `nil`.

```go
old := runSopsEncrypt
runSopsEncrypt = func(publicKey, envFileName, outputFile string) error {
	return os.WriteFile(outputFile, []byte("KEY1=value1\nKEY2=value2\n"), 0644)
}
defer func() { runSopsEncrypt = old }()
```

- [ ] **Step 3: Add a negative-path unit test**

Add a test that stubs `runSopsEncrypt` to return an error and asserts `HandleEnvFile` returns that error.

- [ ] **Step 4: Run targeted verification**

Run: `GOCACHE=$(pwd)/.gocache go test ./utils -run 'TestHandleEnvFile|TestLoadAppConfig' -v`

Expected: PASS without requiring `sops` installed.

- [ ] **Step 5: Commit**

```bash
git add utils/utils.go utils/utils_test.go
git commit -m "test: decouple HandleEnvFile unit tests from local sops binary"
```

### Task 5: Re-run full repo verification

**Files:**
- No code changes expected

- [ ] **Step 1: Run the full repo test suite**

Run: `GOCACHE=$(pwd)/.gocache go test ./...`

Expected: PASS

- [ ] **Step 2: Run the full repo build**

Run: `GOCACHE=$(pwd)/.gocache go build ./...`

Expected: SUCCESS

- [ ] **Step 3: If anything still fails, stop and capture the exact failing package before continuing**

Do not continue to feature work until the repo has a trustworthy green baseline.

---

## Chunk 3: Add Focused Regression Coverage for TLS Diagnostics

### Task 6: Add unit coverage for `cert-status` parsing seams

**Files:**
- Modify: `cmd/certstatus/certstatus.go`
- Create: `cmd/certstatus/certstatus_test.go`

- [ ] **Step 1: Extract small pure helpers from `cert-status`**

Pull out logic that does not need SSH:

```go
func acmeEntryExists(acmeJSON, domain string) bool
func filterLogsForDomain(logs, domain string) string
```

`filterLogsForDomain` already exists; keep it pure and test it directly. Add a pure helper for ACME entry presence instead of inline `strings.Contains`.

- [ ] **Step 2: Write tests for ACME entry detection**

```go
func TestACMEEntryExists(t *testing.T) {
	assert.True(t, acmeEntryExists(`{"Certificates":[{"domain":{"main":"app.example.com"}}]}`, "app.example.com"))
	assert.False(t, acmeEntryExists(`{"Certificates":[]}`, "app.example.com"))
}
```

- [ ] **Step 3: Write tests for log filtering**

```go
func TestFilterLogsForDomainReturnsLatestRelevantError(t *testing.T) {
	logs := `
time=1 msg="unable to generate a certificate for app.example.com"
time=2 msg="error renewing app.example.com"
`
	assert.Contains(t, filterLogsForDomain(logs, "app.example.com"), "error renewing")
}
```

- [ ] **Step 4: Run targeted verification**

Run: `GOCACHE=$(pwd)/.gocache go test ./cmd/certstatus -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/certstatus/certstatus.go cmd/certstatus/certstatus_test.go
git commit -m "test: add focused regression coverage for cert-status diagnostics"
```

### Task 7: Add a smoke test for Traefik config generation

**Files:**
- Modify: `utils/stages_test.go`

- [ ] **Step 1: Assert provider substitution and storage path together**

Add one smoke test that validates the generated compose output includes:

- `--certificatesresolvers.default.acme.storage=/ssl-certs/acme.json`
- `--certificatesresolvers.default.acme.dnschallenge.provider=cloudflare`
- `./traefik/ssl-certs/:/ssl-certs/`

- [ ] **Step 2: Run targeted verification**

Run: `GOCACHE=$(pwd)/.gocache go test ./utils -run 'TestTraefik' -v`

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add utils/stages_test.go
git commit -m "test: add Traefik compose smoke coverage for DNS-01 config"
```

---

## Chunk 4: Final Verification and Documentation Sync

### Task 8: Verify the repaired TLS feature end-to-end at code level

**Files:**
- Review only unless doc text changes are required

- [ ] **Step 1: Re-run full verification**

Run:

```bash
GOCACHE=$(pwd)/.gocache go test ./...
GOCACHE=$(pwd)/.gocache go build ./...
```

Expected: both succeed.

- [ ] **Step 2: Re-check the original failure points manually**

Confirm:

- compose template mounts `./traefik/ssl-certs/:/ssl-certs/`
- setup commands create `./traefik/ssl-certs/acme.json`
- `cert-status` reads `traefik/ssl-certs/acme.json`
- `launch` and `deploy` still call `ValidateTLSCertWithRetry`

- [ ] **Step 3: Update docs only if implementation details changed**

If the previous spec/plan still mentions `./traefik/ssl/`, update:

- `docs/superpowers/specs/2026-06-03-letsencrypt-dns01-design.md`
- `docs/superpowers/plans/2026-06-03-letsencrypt-dns01.md`

to reflect the canonical host ACME directory.

- [ ] **Step 4: Commit any doc sync**

```bash
git add docs/superpowers/specs/2026-06-03-letsencrypt-dns01-design.md docs/superpowers/plans/2026-06-03-letsencrypt-dns01.md
git commit -m "docs: sync TLS DNS-01 docs with canonical ACME storage path"
```

