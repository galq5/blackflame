# Blackflame — Roadmap

This roadmap is ordered by dependency, not by calendar date. The goal is to ship a useful Flame replacement early, then add the larger homelab integrations without destabilizing the core.

## Release strategy

- **0.1** — Flame replacement plus discovery and status.
- **0.2** — Additional widgets, forward-auth, PWA, and operational polish.
- **0.3** — OIDC/RBAC, quick actions, generic widgets, and NAS integration.
- **1.0** — Stable config schema, reliable upgrades, backups, migration guarantees, and a security-reviewed release.

Use `0.x` until the config schema and migration path are boring. Do not call the first release 1.0.

## 0.1 — Flame replacement plus differentiators

### Scope

- Apps, bookmarks, categories/sections, pinning, ordering, public/private.
- Multiple boards with `/b/{slug}` routes and per-board visibility.
- Search prefixes (`/g`, `/yt`, `/gh`, `/w`), custom providers, local filtering.
- Themes from CSS variables, presets, custom CSS, dark/light presets.
- Layout toggles for the board header, search bar, clock, and weather.
- Public read-only mode and password-protected edit mode.
- One-click Flame `db.sqlite` import plus CLI import.
- dashboard-icons cache, icon picker, custom uploads, letter-tile fallback.
- Status checks: HTTP/HTTPS, latency, status dots, TLS certificate expiry, SSE.
- Docker discovery by labels; Kubernetes Ingress/HTTPRoute discovery by annotations.
- Flame label and annotation compatibility.
- Two config modes: UI/SQLite and GitOps/YAML.
- Weather widget using Open-Meteo (Flame parity).
- Custom dashboard name, description, and logo.
- Optional Pi-hole, Proxmox, Kubernetes, Prometheus/NAS, and iCal integrations.
- Layout toggles for the board header, search bar, clock, and weather.
- `/healthz`, `/readyz`, `/metrics`, structured logs.
- Alpine multi-arch image, Compose, Helm, systemd unit.
- GitHub Actions CI and signed releases.

### Non-goals for 0.1

- Additional widgets beyond the built-in optional integrations.
- Direct NAS/SMART collection; use Prometheus exporters instead.
- OIDC, forward-auth, multi-user roles.
- Quick actions, PWA, plugin system.
- Write access to Docker or Kubernetes.
- High availability or multiple replicas.

## Milestones

### M0 — Foundation

**Deliverables**

- Repository structure, contribution docs, security policy, license decision.
- CI skeleton: Go lint/test, embedded frontend smoke test, cross-build checks.
- Binary with the embedded SPA shell.
- Configuration model and `ConfigStore` interface.
- SQLite connection, migrations, and runtime store.
- UI mode and GitOps mode startup paths.
- Health/readiness endpoints.
- Structured logging and graceful shutdown.
- CLI skeleton: `serve`, `validate`, `version`, `healthcheck`.

**Exit criteria**

- `blackflame serve --mode ui` starts with a temporary data directory.
- `blackflame serve --mode gitops` loads a minimal YAML file.
- Invalid YAML fails with a useful error.
- `blackflame validate` returns non-zero on invalid config.
- CI builds `linux/amd64`, `linux/arm64`, and `linux/arm/v7`.
- No Node.js is required at runtime.

### M1 — Startpage core

**Deliverables**

- Boards, sections, and items CRUD in UI mode.
- Public/private visibility and server-side filtering.
- Pinning and transactional ordering.
- Multiple boards and board switcher.
- Search with prefixes, local filtering, and keyboard shortcuts.
- Theme presets, CSS variables, custom CSS, per-board override in YAML mode.
- Edit-mode password, sessions, CSRF, rate limiting.
- Weather widget via Open-Meteo.
- Icon index, on-demand fetch, SQLite cache, icon picker, custom upload.
- Letter-tile fallback.
- Basic responsive layout and accessibility pass.

**Exit criteria**

- Fresh install → setup password → add/pin/reorder app → reload persists.
- Anonymous user cannot see private boards/items through API or search.
- `/g query` opens the provider; local search filters immediately.
- Keyboard flow works without a mouse.
- Icons load locally after first fetch; no CDN request on page load.
- Theme changes apply without a rebuild.

### M2 — Status, discovery, and import

**Deliverables**

- Status scheduler with global interval, per-item enable/disable, jitter, timeout, and concurrency limit.
- HTTP/HTTPS checks, latency, TLS expiry, status dots, SSE, polling fallback.
- `/metrics` for status, discovery, HTTP, and build info.
- Docker discovery with Flame and Blackflame labels.
- Kubernetes Ingress/HTTPRoute discovery with Flame and Blackflame annotations.
- Discovery overrides: rename, icon, hide, pin, and reorder.
- Discovery status UI and metrics.
- Flame database import: CLI and UI upload, preview, replace/merge.
- Import fixtures and mapping tests.

**Exit criteria**

- A labeled container appears within one discovery interval.
- An annotated Ingress appears in a real k3s cluster; RBAC errors are actionable.
- Overrides survive subsequent discovery syncs.
- Status reflects 200, 500, timeout, and certificate expiry correctly.
- Private item status is absent from anonymous API/SSE/metrics.
- Flame import preserves counts, visibility, pins, ordering, descriptions, and icons.
- GitOps mode reloads valid YAML and keeps the last good config on invalid YAML.

### M3 — Release readiness

**Deliverables**

- API integration tests for UI and GitOps modes plus a browser smoke test.
- Helm chart with PVC/ConfigMap modes, RBAC, probes, ServiceMonitor, NetworkPolicy.
- Compose and systemd examples.
- Multi-arch image published to GHCR.
- GoReleaser binaries, checksums, SBOM, provenance, cosign signing.
- Documentation: quickstart, config reference, discovery labels/annotations, Flame migration, reverse proxy, troubleshooting.
- Security review: auth, CSRF, private filtering, SVG sanitization, SSRF policy, dependency scan.
- Load test with 100+ items and 50 checks.

**Exit criteria**

- `docker run` → useful dashboard in under five minutes.
- Helm install works on k3s with UI mode and GitOps mode.
- Image runs on amd64 and arm64; armv7 builds and smoke-tests.
- Idle RSS below 50 MB with 100 items and 50 checks.
- Image at or below ~30 MB uncompressed, with a hard ceiling of 35 MB.
- No high/critical vulnerabilities in the release scan.
- Release artifacts are signed and reproducible enough for contributors.

## 0.2 — Homelab extras

- Weather improvements (forecast, units, location search).
- Greeting widget.
- iCal recurrence expansion.
- More curated widgets.
- TCP status checks.
- Forward-auth support for Authelia/Authentik with trusted-proxy enforcement.
- PWA: offline shell, cached icons, update prompt; no private API caching.
- Better theme editor and theme import/export.
- Backup/restore command.
- More discovery sources: Traefik `IngressRoute`, additional Gateway API kinds.
- Kubeconfig parsing for standalone Kubernetes discovery.

## 0.3 — Automation and identity

- OIDC authorization code + PKCE with group/role mapping.
- Viewer/editor/admin roles; per-board permissions.
- Quick actions:
  - webhook actions first;
  - Wake-on-LAN with documented network constraints;
  - Kubernetes deployment restart with separate opt-in RBAC and audit logging.
- Generic HTTP/JSON widget.
- NAS/OMV integration via a small optional agent; Prometheus exporters are already supported.
- Audit log UI.
- API tokens for automation.
- Multi-cluster discovery (carefully; not a 0.3 guarantee).

## 1.0 — Stability

1.0 requires:

- Stable, versioned config schema with deprecation policy.
- Tested upgrade path across at least two minor versions.
- Backup/restore and migration commands.
- Documented security model and threat model.
- Auth integrations and private-data filtering covered by E2E tests.
- No known high/critical security issues.
- Multi-arch releases and Helm chart versioned together.
- Clear contribution, release, and support process.

## Deferred ideas

These are intentionally not scheduled:

- Plugin marketplace or third-party widget runtime.
- Container management (start/stop/delete).
- Kubernetes workload management.
- Full uptime monitoring, alerting, or incident management.
- Multi-tenant SaaS.
- Mobile native app.
- AI-generated dashboards or chat features.
- Arbitrary shell command actions.

## Release checklist

- [ ] `go test -race ./...`
- [ ] `govulncheck ./...`
- [ ] embedded frontend smoke test and browser check
- [ ] dependency vulnerability scan (`govulncheck`) and license check
- [ ] cross-builds for amd64/arm64/armv7
- [ ] E2E in UI mode and GitOps mode
- [ ] Flame import fixture passes
- [ ] private-data leakage tests pass
- [ ] image scan clean of high/critical issues
- [ ] SBOM, provenance, and signatures attached
- [ ] docs updated for new flags/config fields
- [ ] Helm chart linted and installed on k3s
- [ ] upgrade test from previous release
- [ ] release notes and migration notes written

## Risks to the roadmap

- Widget APIs change frequently; keep the widget set curated.
- Discovery edge cases can consume disproportionate time; ship Ingress + HTTPRoute first.
- Auth integrations expand the security surface; do not rush OIDC/RBAC.
- NAS/SMART requires host access; prefer an agent or Prometheus exporters.
- PWA caching can leak private data; review the service worker carefully.
- SQLite limits HA; do not promise multiple replicas.
- Flame 3.0 may change schemas and conventions; keep import fixtures versioned.
