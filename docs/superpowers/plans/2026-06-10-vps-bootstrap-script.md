# VPS Bootstrap Script Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a reliable Ubuntu VPS bootstrap script and remove the hard ssh-agent requirement so `sidekick init` works on a clean self-hosted server without manual SSH repair steps.

**Architecture:** Keep the current CLI flow intact, but fix the SSH auth layer so keyfile auth works even when `SSH_AUTH_SOCK` is absent. Add a new shell bootstrap script that prepares the local user, the server-side `sidekick` user, and final self-checks before the user runs interactive `sidekick init`.

**Tech Stack:** Bash, Go, Cobra CLI, `golang.org/x/crypto/ssh`

---

## Chunk 1: Spec and auth fallback tests

### Task 1: Add SSH auth fallback tests

**Files:**
- Modify: `utils/auth.go`
- Create: `utils/auth_test.go`

- [ ] **Step 1: Write failing tests for auth method selection**

Add tests that cover:
- no `SSH_AUTH_SOCK`, keyfile auth available -> succeeds without error
- missing `SSH_AUTH_SOCK`, no keyfile auth -> returns error
- present `SSH_AUTH_SOCK`, agent available -> appends agent auth method

- [ ] **Step 2: Run focused tests to verify they fail**

Run: `GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./utils -run 'TestGetSSHAuthMethods'`

- [ ] **Step 3: Implement minimal auth fallback**

Refactor `utils/auth.go` to:
- load keyfile auth methods first
- treat agent auth as optional
- return regular errors instead of `log.Fatal` on missing `SSH_AUTH_SOCK`

- [ ] **Step 4: Re-run focused tests**

Run: `GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./utils -run 'TestGetSSHAuthMethods'`

- [ ] **Step 5: Commit**

```bash
git add utils/auth.go utils/auth_test.go
git commit -m "fix: allow ssh key auth without ssh-agent"
```

## Chunk 2: Bootstrap script

### Task 2: Add new VPS bootstrap script

**Files:**
- Create: `scripts/bootstrap-sidekick-vps.sh`
- Optionally Modify: `README.md`

- [ ] **Step 1: Write script behavior checklist as comments and functions**

Structure the script around focused functions:
- package install
- Go and `sops` install
- Sidekick build/install
- local SSH key setup
- `sidekick` user setup
- final readiness checks

- [ ] **Step 2: Implement the script**

Make the script:
- run as non-root sudo-capable user
- prompt only for server IPv4
- create/fix `sidekick` user and SSH permissions
- perform `ssh -o PreferredAuthentications=publickey sidekick@<ip> whoami`
- fail with clear output when readiness is incomplete

- [ ] **Step 3: Validate shell syntax**

Run: `bash -n scripts/bootstrap-sidekick-vps.sh`

- [ ] **Step 4: Commit**

```bash
git add scripts/bootstrap-sidekick-vps.sh
git commit -m "feat: add self-hosted VPS bootstrap script"
```

## Chunk 3: Full verification

### Task 3: Run repo-level verification

**Files:**
- Verify only

- [ ] **Step 1: Run full tests**

Run: `GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./...`

- [ ] **Step 2: Run full build**

Run: `GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go build ./...`

- [ ] **Step 3: Summarize usage**

Document the expected flow:
- run `scripts/bootstrap-sidekick-vps.sh`
- confirm readiness output
- run interactive `sidekick init`
