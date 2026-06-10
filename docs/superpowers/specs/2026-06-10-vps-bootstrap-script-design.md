# VPS Bootstrap Script Design

## Goal

Add a new Ubuntu-focused bootstrap script that prepares a clean VPS for `sidekick init` without the manual SSH user/key repair steps we had to do during debugging.

## Problem

The current `scripts/install-sidekick-ubuntu.sh` installs local prerequisites, but it does not ensure the `sidekick` server user exists with working key-based login. In practice, `sidekick init` also fails hard when `SSH_AUTH_SOCK` is missing, even though the code already knows how to read private keys from `~/.ssh`. This makes a clean self-hosted VPS setup brittle and forces manual repair steps.

## Desired Outcome

After running the new script on a clean Ubuntu VPS as a normal sudo-capable user:

- `sidekick` is installed in `/usr/local/bin`
- the current user has an SSH keypair if missing
- the `sidekick` user exists on the machine
- `/home/sidekick/.ssh/authorized_keys` contains the current user's public key
- permissions for `/home/sidekick/.ssh` are correct
- Docker, `age`, `sops`, and Go are installed
- a readiness check confirms `ssh sidekick@<server-ip>` works with public key auth
- `sidekick init` can run afterward without requiring a live `ssh-agent`

## Scope

### In Scope

- new bootstrap shell script under `scripts/`
- safer SSH auth behavior in `utils/auth.go` so agent-less keyfile auth works
- shell syntax validation for the new script
- unit tests for the SSH auth fallback logic

### Out of Scope

- making `sidekick init` non-interactive
- editing `sshd_config`
- changing Traefik, Docker, or deploy semantics
- replacing the existing installer script unless needed for documentation

## Approach

### 1. New bootstrap script

Create `scripts/bootstrap-sidekick-vps.sh` as a focused self-hosted bootstrap flow. It will:

- install/verify `age`, `curl`, `git`, `openssh-client`, Docker, Go, and `sops`
- build and install the latest `sidekick`
- create `~/.config/sidekick`
- create `~/.ssh/id_ed25519` if missing
- ensure the current user's public key is present in their own `authorized_keys`
- create the `sidekick` user if missing
- copy the current user's `authorized_keys` to `/home/sidekick/.ssh/authorized_keys`
- fix ownership and permissions
- add the host key to `known_hosts`
- run a final readiness summary including a direct key-based SSH self-check to `sidekick@<ip>`

### 2. SSH auth fallback

Refactor `utils/auth.go` so missing `SSH_AUTH_SOCK` is no longer fatal. The client should:

- always load usable key files from `~/.ssh`
- optionally append ssh-agent signers when `SSH_AUTH_SOCK` exists and is reachable
- return a normal error only if no auth methods are available at all

This change preserves agent support but removes the unnecessary hard dependency on it.

## Risks

- Self-SSH by public IP can fail on unusual provider networking setups. The script should fail clearly and say that bootstrap is incomplete rather than pretending success.
- The current installer and the new script will overlap. That is acceptable in v1 as long as the new script is clearly positioned as the reliable self-hosted bootstrap path.

## Validation

- unit tests for SSH auth method selection
- `bash -n scripts/bootstrap-sidekick-vps.sh`
- `go test ./...`
- `go build ./...`
