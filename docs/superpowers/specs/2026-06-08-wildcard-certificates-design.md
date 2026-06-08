# Wildcard Certificates for Sidekick Servers — Design Spec

## Goal

Add a first-class `Wildcard` certification mode to `sidekick init` so a single server can obtain and reuse one wildcard certificate for a base domain such as `*.saola.cz`, while keeping today's per-host mode as the default and preserving backwards compatibility.

## Product Intent

Today's DNS-01 integration works, but it still behaves like a per-host certificate system:

- every app domain is treated as an independent certificate concern
- deploy-time diagnostics talk about the app hostname first, not the server's certificate strategy
- users still have to think about whether a given app will trigger certificate issuance

For users who host many subdomains under one base domain, Sidekick should feel closer to plug-and-play:

- configure DNS provider and certificate strategy once during `init`
- optionally configure wildcard DNS outside Sidekick
- deploy apps under that zone without repeating certificate setup per app

## Scope

This v1 design covers one wildcard zone per server.

- supported: one server configured for `saola.cz`, serving `app.saola.cz`, `grafana.saola.cz`, `uptimekuma.saola.cz`
- supported: explicit server mode selection during `sidekick init`
- supported: migration of an existing DNS-01 server from per-host mode to wildcard mode
- not supported: multiple wildcard zones on one server
- not supported: automatic DNS record management at the registrar or DNS provider
- not supported: changing app routing away from Traefik `Host(...)` routers

## User Experience

### `sidekick init`

After ACME email and DNS provider selection, Sidekick asks for certificate strategy:

- `Certification mode: Per-host`
- `Certification mode: Wildcard`

If the user selects `Wildcard`, Sidekick prompts for:

- `Wildcard domain`, for example `saola.cz`

Then Sidekick shows a short note:

- wildcard DNS is optional but recommended
- users can either create per-app DNS records manually or configure `*.saola.cz`
- all deployed app hostnames on that server must stay within the wildcard domain

### `sidekick launch` and `sidekick deploy`

In wildcard mode:

- app domains still use the normal Traefik router rule `Host(app.saola.cz)`
- Sidekick validates that the app hostname belongs to the configured wildcard zone
- if the hostname is outside the zone, launch/deploy fails early with a clear message
- certificate validation is reported as a server capability, not as a per-app issuance flow
- app compose files in wildcard mode still set `traefik.http.routers.<app>.tls=true`, but they must stop emitting `traefik.http.routers.<app>.tls.certresolver=default`

That distinction matters:

- `tls=true` keeps HTTPS routing enabled
- omitting the per-app `certresolver` prevents Sidekick from continuing to model wildcard servers as per-host ACME issuers

Example failure:

`app domain foo.example.com is outside wildcard domain saola.cz`

### `sidekick cert-status`

For wildcard-mode servers, diagnostics become more explicit:

- whether the server has a wildcard certificate entry
- whether the requested hostname is covered by the wildcard domain
- whether TLS served on the server is valid for that hostname
- whether public DNS resolves the hostname correctly

This avoids conflating missing public DNS with missing certificate issuance.

## Configuration Model

Extend `SidekickServer` with two new fields:

- `CertificateMode string`
- `WildcardDomain string`

Allowed values:

- `per-host`
- `wildcard`

Rules:

- `WildcardDomain` must be empty in `per-host` mode
- `WildcardDomain` is required in `wildcard` mode
- existing configs without `CertificateMode` should load as `per-host`

Example:

```yaml
servers:
  - name: scvd
    serveraddress: 204.10.194.116
    certemail: ops@example.com
    dnsprovider: digitalocean
    certificateMode: wildcard
    wildcardDomain: saola.cz
```

## Traefik Strategy

The current Traefik setup already uses DNS-01 and a shared resolver:

- `--certificatesresolvers.default.acme.dnschallenge.provider=...`
- `--certificatesresolvers.default.acme.storage=/ssl-certs/acme.json`

Wildcard mode builds on this by making the wildcard certificate an explicit server-level concern.

The generated Traefik configuration should include static entrypoint TLS domain configuration so Traefik requests:

- `main = saola.cz`
- `sans = *.saola.cz`

This should happen in the server-level Traefik setup generated during `init`, not in per-app compose files.

For v1, use the static entrypoint shape because it fits the current `utils/scripts.go` template model cleanly:

- `--entrypoints.websecure.http.tls.certresolver=default`
- `--entrypoints.websecure.http.tls.domains[0].main=saola.cz`
- `--entrypoints.websecure.http.tls.domains[0].sans=*.saola.cz`

This keeps wildcard acquisition in one server-owned place and avoids introducing a synthetic bootstrap router just to trigger ACME.

## Domain Validation Rules

Wildcard mode must enforce hostname boundaries.

Valid:

- `uptimekuma.saola.cz` under `saola.cz`
- `grafana.saola.cz` under `saola.cz`

Invalid:

- `saola.cz` itself for an app hostname unless explicitly allowed by the design
- `foo.example.com`
- `foo.bar.saola.cz`

For v1, the matching rule is strict:

- app hostname must have exactly one additional label before `WildcardDomain`
- app hostname must not equal the apex `WildcardDomain`

That means:

- `uptimekuma.saola.cz` is allowed
- `foo.bar.saola.cz` is rejected
- `saola.cz` is not treated as an app hostname in wildcard mode

## Migration Behavior

If `sidekick init` runs against an already configured DNS-01 server:

- if the server is in per-host mode, Sidekick can offer migration to wildcard mode
- migration rewrites stored server config and regenerates Traefik config
- app-level compose files are migrated lazily on the next `launch` or `deploy`

Source of truth:

- the persisted local `SidekickServer` config is the authoritative certificate-mode state
- migration is triggered when the requested mode or wildcard domain differs from the stored server config, even if the DNS provider itself is unchanged

Compose migration behavior:

- existing remote app compose files may still contain `tls.certresolver=default` from the legacy per-host model
- Sidekick does not perform a bulk remote rewrite during `init`
- instead, each app's compose file is rewritten the next time that app runs through `sidekick deploy`
- new apps launched after migration are written without the per-app `tls.certresolver` label

If a user later switches back from wildcard to per-host mode:

- Sidekick should rewrite server config and Traefik config accordingly
- existing app compose files remain compatible

## Diagnostics and Validation

### TLS validation

Keep the current post-deploy TLS check pattern:

- validate against `serverIP:443`
- preserve `ServerName=app-domain` for SNI

In wildcard mode, the success message should make it clear that:

- the server serves a valid certificate for the app hostname
- the certificate may be backed by the wildcard server config

### Public DNS validation

Keep public DNS as a separate check.

In wildcard mode, warning text should mention both supported routing patterns:

- per-host DNS records
- wildcard DNS record such as `*.saola.cz -> <server-ip>`

### `cert-status`

Add wildcard-aware checks:

- detect server certificate mode from local config
- parse `acme.json` for `main` and `sans` coverage entries such as `saola.cz` and `*.saola.cz`
- test an app hostname against the wildcard zone rules
- report public DNS mismatch separately from TLS validity

## Recommended DNS Guidance

Sidekick should not require wildcard DNS, but should recommend it for convenience.

Recommended setups:

1. Manual per-app DNS records
   Example: `uptimekuma.saola.cz A 204.10.194.116`

2. Wildcard DNS record
   Example: `*.saola.cz A 204.10.194.116`

Sidekick should explain this in `init` and in public DNS warnings, but DNS record creation stays outside the product in v1.

## Backwards Compatibility

- existing servers continue to work as `per-host`
- existing app `sidekick.yml` files remain valid
- existing launch/deploy flows remain valid for per-host mode
- no mandatory migration is introduced

## Risks

### 1. Traefik wildcard acquisition shape

Traefik must be configured in a way that reliably requests the wildcard cert at server setup time. If the chosen config shape does not force acquisition until first matching request, the UX may still feel partially lazy.

Mitigation:

- choose the simplest config shape that is already well supported by Traefik
- test first-run acquisition against a clean `acme.json`

### 2. Confusion between wildcard cert and wildcard DNS

Users may assume wildcard cert alone makes hostnames reachable.

Mitigation:

- explain the difference during `init`
- keep public DNS warning text explicit

### 3. Domain matching edge cases

Naive suffix matching can accidentally allow invalid hostnames.

Mitigation:

- centralize wildcard domain validation in a tested helper
- reject apex and out-of-zone hostnames explicitly

## Testing Strategy

Add coverage for:

- config load/save with new server fields
- `init` prompting and flag parsing for certificate mode and wildcard domain
- Traefik config rendering in per-host and wildcard modes
- hostname-in-zone validation helper
- launch/deploy guardrails for out-of-zone hostnames
- wildcard-aware `cert-status` interpretation
- migration from existing DNS-01 server config to wildcard mode

## Recommended v1 Boundaries

Keep this release intentionally small:

- one wildcard domain per server
- no DNS provider automation for A/CNAME records
- no multi-zone routing model
- no app-level opt-in/opt-out once the server is in wildcard mode

That keeps the implementation aligned with current server-centric Sidekick architecture and leaves room for a future hybrid model if it proves necessary.
