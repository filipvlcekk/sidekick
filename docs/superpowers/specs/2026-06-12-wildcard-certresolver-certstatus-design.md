# Wildcard Certresolver and Cert-Status Design

## Problem

Sidekick currently omits `traefik.http.routers.<app>.tls.certresolver=default` for app routers generated in wildcard certificate mode. In practice this leaves newly launched or deployed wildcard-hosted apps serving Traefik's default self-signed certificate until users manually patch the generated compose file.

Sidekick also has a `cert-status` reliability bug: when SSH commands return no stdout, the command blocks waiting on a channel read and never finishes.

## Goal

Make `sidekick launch` and `sidekick deploy` generate correct Traefik TLS router labels for wildcard-hosted apps by default, and make `sidekick cert-status` complete reliably even when Traefik logs or ACME storage reads produce no stdout.

## Constraints

- Keep wildcard domain validation behavior unchanged.
- Keep Traefik entrypoint-level wildcard TLS domain flags unchanged.
- Minimize behavioral changes outside TLS label generation and `cert-status` diagnostics.
- Cover the regression with tests in both launch and deploy paths.

## Approaches Considered

### 1. Minimal conditional removal

Change the existing conditional in `utils/wildcard.go` so `tls.certresolver=default` is always appended.

Pros:
- Smallest code change

Cons:
- Leaves the TLS label logic implicit and easy to regress again

### 2. Small helper-oriented cleanup

Make TLS router label generation explicit in `BuildAppTraefikLabels`, always including `tls=true` and `tls.certresolver=default`, then update the wildcard-mode tests to match the intended behavior. Separately, make `cert-status` use non-blocking output collection helpers so empty command output is treated as empty diagnostics instead of a hang.

Pros:
- Small scope
- Clearer intent
- Good regression coverage

Cons:
- Slightly broader than the absolute minimum

### 3. Broader diagnostics refactor

Introduce a larger SSH-command abstraction for `cert-status` and related commands.

Pros:
- Stronger long-term abstraction

Cons:
- Unnecessary scope expansion for the current bug

## Chosen Design

Use approach 2.

### TLS label generation

- Update `utils.BuildAppTraefikLabels` so every app router always gets:
  - `traefik.http.routers.<service>.tls=true`
  - `traefik.http.routers.<service>.tls.certresolver=default`
- Keep wildcard-mode domain validation in place.
- Keep Traefik wildcard entrypoint TLS domain flags in `utils/stages.go` unchanged.

### Cert-status output handling

- Add a small helper in `cmd/certstatus/certstatus.go` to safely read zero-or-one lines from a `RunCommand` stdout channel without blocking forever.
- Use that helper for:
  - Traefik ACME log reads
  - `acme.json` reads
  - app label listing
  - per-app domain extraction
- Treat empty output as empty diagnostics rather than a fatal condition unless the SSH command itself returned an error for a required operation.

## Files

- Modify `utils/wildcard.go`
- Modify `cmd/launch/launch_test.go`
- Modify `cmd/deploy/deploy_test.go`
- Modify `cmd/certstatus/certstatus.go`
- Add or extend tests in `cmd/certstatus/certstatus_test.go`

## Testing

- Update launch wildcard test to require `tls.certresolver=default`
- Update deploy wildcard test to require `tls.certresolver=default`
- Keep out-of-zone wildcard rejection tests passing
- Add `cert-status` tests proving empty stdout does not block helper logic and yields empty diagnostics
- Run targeted Go tests for touched packages, then broader package tests for confidence
