# Blackflame — Architecture

## 1. Overview

Blackflame is a single Go binary that serves an embedded single-page application and exposes a JSON API. It supports two configuration modes:

- **UI mode** (`--mode ui`): SQLite is the authoritative content store; the browser can edit it.
- **GitOps mode** (`--mode gitops`): YAML is the authoritative content store; the browser is read-only.

In both modes, runtime state (sessions, status snapshots, icon cache, discovery cache) is separate from content configuration.

The design prioritizes:

- a small static binary;
- predictable behavior in Docker and Kubernetes;
- server-side filtering of private content;
- no heavy vendor SDKs for Docker or Kubernetes;
- a frontend that can be replaced without touching the backend.

## 2. Goals and constraints

### Goals

- One binary, no Node.js runtime.
- Pure-Go SQLite (`CGO_ENABLED=0`).
- Multi-arch images for `linux/amd64`, `linux/arm64`, and `linux/arm/v7`.
- UI and GitOps modes from day one.
- Read-only Docker and Kubernetes discovery.
- Status checks and metrics without an external monitoring stack.
- Flame database, label, and annotation compatibility.

### Constraints

- Latest stable dependencies only; no prereleases or deprecated stacks.
- No `client-go` or Docker SDK in the default build.
- Single replica when SQLite is used.
- No write access to Docker or Kubernetes in 0.1.
- No arbitrary command execution.
- No telemetry.

## 3. System context

```
┌─────────────────────────────────────────────────────────────┐
│ Browser                                                     │
│  SPA: boards, search, edit mode, status, settings           │
└──────────────────────────────┬──────────────────────────────┘
                               │ JSON + SSE
┌──────────────────────────────▼──────────────────────────────┐
│ Blackflame binary                                           │
│                                                             │
│  HTTP server (stdlib net/http, middleware, auth, CSRF)      │
│  ├── Content config                                          │
│  │    ├── UI mode: SQLite                                   │
│  │    └── GitOps mode: YAML snapshot                         │
│  ├── Runtime store                                           │
│  │    ├── SQLite (optional in GitOps mode)                   │
│  │    └── In-memory fallback                                 │
│  ├── Discovery                                               │
│  │    ├── Docker API over Unix socket/TCP                    │
│  │    └── Kubernetes REST over HTTPS                         │
│  ├── Status scheduler                                        │
│  ├── Icon cache                                              │
│  └── Prometheus metrics                                      │
└───────────────┬──────────────────────────────┬──────────────┘
                │                              │
        Docker daemon                  Kubernetes API
        (read-only, proxy              (read-only RBAC)
         recommended)
```

## 4. Runtime components

### 4.1 HTTP server

- Go standard library `net/http` with method/path patterns.
- Middleware chain: request ID, structured logging, recovery, security headers, base-path handling, session/auth, CSRF, rate limiting.
- JSON API under `/api/v1`.
- Embedded SPA with history fallback.
- SSE endpoint for status/config events.
- Graceful shutdown with a configurable timeout.

### 4.2 Content config

A `ConfigStore` interface hides whether content comes from SQLite or YAML:

- `Snapshot()` returns an immutable, validated content model.
- `Reload()` re-reads the authoritative source.
- `Mutate()` is implemented only by the UI store.
- `Watch()` emits change notifications for GitOps reloads.

The HTTP layer and frontend consume the same snapshot shape in both modes. This is the key to avoiding two applications.

### 4.3 Runtime store

Runtime state is not content configuration:

- sessions;
- status latest results;
- icon cache;
- discovery cache;
- optional audit log.

In UI mode, runtime state lives in the same SQLite file as content. In GitOps mode, it can live in SQLite via `--data` or in memory when `--data` is omitted. In-memory mode loses sessions, status, and icons on restart; this is documented as a tradeoff.

### 4.4 Discovery

Two independent discoverers implement a common interface:

```
Discover(ctx) ([]DiscoveredItem, error)
```

Results are merged with content config and local overrides at query time. Discovery never writes content config or remote systems.

### 4.5 Status scheduler

A single scheduler owns a worker pool and a jittered ticker. It reads the merged item list, schedules checks, and publishes results to the runtime store and event bus.

### 4.6 Icon cache

A service resolves icon names against a curated embedded list and arbitrary dashboard-icons slugs, fetches missing icons from a pinned dashboard-icons revision, stores them in SQLite, and serves them from `/icons/{name}`.

## 5. Configuration modes

### 5.1 UI mode

- Flag: `--mode ui`.
- Required: `--data` directory.
- SQLite is authoritative for boards, sections, items, overrides, and settings.
- The browser can create, update, delete, reorder, and import content.
- YAML export is available for backup and migration.
- Schema migrations run automatically on startup; a `migrate` subcommand is available for controlled upgrades.

### 5.2 GitOps mode

- Flag: `--mode gitops`.
- Required: `--config` file.
- Optional: `--data` for persistent runtime state.
- YAML is authoritative for content and settings.
- The UI is read-only; mutation endpoints return `409 Conflict`.
- The file is validated and atomically replaced in memory. A failed reload keeps the last good snapshot and exposes an error via the API and logs.
- Reload triggers: periodic hash check for ConfigMap-style symlink swaps and `SIGHUP`; filesystem notifications can be added later.
- Secret-bearing widgets (later) will use `valueFrom: env|file` references. In 0.1 the edit password comes from the environment or a file.
- Discovery overrides live in YAML. Discovery results remain runtime data and are not written back to YAML.

### 5.3 Invariants

- Exactly one authoritative source per mode.
- No bidirectional sync.
- Runtime state is never exported as content.
- The same internal model and API shape are used in both modes.
- Import from Flame can target either mode: SQLite for UI mode, YAML for GitOps mode.

## 6. Data model

### 6.1 Content tables (UI mode)

| Table | Key fields |
|---|---|
| `boards` | `id`, `slug`, `name`, `is_public`, `theme_json`, `sort_order`, timestamps |
| `sections` | `id`, `board_id`, `name`, `is_public`, `is_pinned`, `sort_order` |
| `items` | `id`, `board_id`, `section_id`, `kind`, `name`, `url`, `icon`, `description`, `is_public`, `is_pinned`, `is_hidden`, `check_enabled`, `sort_order`, `config_json`, `source`, `external_id`, `group_name`, `is_override`, timestamps |
| `settings` | `key`, `value` |

`kind` in 0.1 is `app` or `bookmark` (the schema also accepts `weather` for future use). `source` is `manual`, `docker`, or `kubernetes`. Discovered items live in `items` with a source and external ID. Editing a discovered item sets `is_override=1`, and the next discovery sync preserves the user's changes.

### 6.2 Runtime tables

| Table | Purpose |
|---|---|
| `status_latest` | One row per item key: up, status code, latency, cert expiry, error, checked at |
| `icons` | Name, content type, bytes, source URL, ETag, fetched at |
| `sessions` | Session ID, role, created/expires |
| `goose_db_version` | Applied migration versions (managed by goose) |

An audit log is planned for quick actions in a later release.

### 6.3 Item identity

A stable `item_key` identifies a checkable target:

- `item:<item_id>` for manual/UI items with a database ID;
- `discovered:<source>:<external_id>` for discovered items;
- `yaml:<board>:<kind>:<slug>` for YAML items without an ID.

Status, SSE events, and metrics use `item_key`. Public responses never expose keys that reveal private names.

### 6.4 Visibility

Effective visibility is computed server-side:

```
visible(item) = item.public
             && section.public
             && board.public
```

Private content is removed from:

- anonymous bootstrap responses;
- local search;
- status responses and SSE;
- widget data (when widgets arrive);
- metrics labels.

### 6.5 Ordering

- `sort_order` is an integer within a board or section.
- A move rewrites sibling positions in a transaction.
- The API accepts a full ordered list for drag-and-drop operations to avoid race conditions.

## 7. YAML schema

Top-level keys:

| Key | Purpose |
|---|---|
| `version` | Schema version; required |
| `branding` | Dashboard name, description, and logo |
| `theme` | Global theme, CSS variables, custom CSS reference |
| `layout` | Show/hide board header, search bar, clock, and weather |
| `widgets` | Optional Pi-hole, Proxmox, Kubernetes, Prometheus, and calendar cards |
| `search` | Search providers and prefixes |
| `boards` | Boards, sections, items |
| `weather` | Open-Meteo location and units |
| `discovery` | Docker/Kubernetes discovery settings |
| `status` | Default interval, timeout, expected status, concurrency |
| `icons` | Icon source revision and cache policy |

Shape:

- `boards[]` contains `slug`, `name`, `public`, optional `theme`, and `sections[]`.
- `sections[]` contains `name`, `public`, `pinned`, and `items[]`.
- `items[]` contains `id` (optional), `kind`, `name`, `url`, `icon`, `description`, `public`, `pinned`, and kind-specific `config`.
- Weather is configured in the top-level `weather` block in 0.1. `kind: weather` is reserved for a later widget model.

Validation rules:

- `version` must be supported.
- Unknown fields are rejected to catch typos.
- IDs must be unique within a board; derived IDs must not collide.
- URLs must be absolute `http` or `https` URLs, except internal app URLs that may use `http`.
- Visibility and ordering values must be valid.
- Secret-bearing widget config (later) must use `valueFrom` references.
- Errors include file, line, and field where possible.

Runtime flags are not content config: `--listen`, `--data`, `--config`, `--base-path`, `--external-url`, `--log-level`, `--metrics-listen`.

## 8. API

### 8.1 Public

| Endpoint | Purpose |
|---|---|
| `GET /api/v1/bootstrap` | Boards, sections, visible items, theme, search providers, auth state, config status |
| `GET /api/v1/status` | Latest status for visible items |
| `GET /api/v1/widgets` | Data for enabled integrations, filtered by public/authenticated visibility |
| `GET /api/v1/events` | SSE stream for status and config events |
| `GET /api/v1/version` | Build version and mode |
| `GET /icons/{name}` | Cached icon |
| `GET /healthz` | Liveness |
| `GET /readyz` | Readiness |
| `GET /metrics` | Prometheus metrics on the main port (a separate listener/token is a later option) |

### 8.2 Authentication

| Endpoint | Purpose |
|---|---|
| `POST /api/v1/auth/login` | Unlock edit mode |
| `POST /api/v1/auth/logout` | End session |
| `GET /api/v1/auth/me` | Current role and session state |

### 8.3 Editor (UI mode only)

| Endpoint | Purpose |
|---|---|
| `POST/PATCH/DELETE /api/v1/boards/...` | Board management |
| `POST/PATCH/DELETE /api/v1/sections/...` | Section management |
| `POST/PATCH/DELETE /api/v1/items/...` | Item management |
| `POST /api/v1/items/reorder` | Reorder within a section or board |
| `POST /api/v1/import/flame` | Upload and import a Flame database |
| `GET /api/v1/export` | Export current content as YAML |
| `POST /api/v1/discovery/refresh` | Trigger discovery |
| `POST /api/v1/icons` | Upload a custom icon |
| `PATCH /api/v1/settings` | Branding, layout, theme, widgets, search, weather, discovery, status settings |

Mutation responses in GitOps mode: `409 Conflict` with a message explaining that the instance is config-managed.

### 8.4 Error format

```json
{
  "error": {
    "code": "invalid_config",
    "message": "item url must be http or https",
    "field": "boards[0].sections[1].items[2].url"
  }
}
```

## 9. Frontend

The frontend is an SPA embedded into the binary. The API is frontend-agnostic.

**0.1 implementation:** a dependency-free ES module application under `internal/web/dist`. It uses plain HTML, CSS, and JavaScript, so the project has no Node build step and no runtime framework dependency. The initial payload is roughly 60 KB of uncompressed JavaScript and 18 KB of CSS. A future Svelte 5 + Vite or React 19 + Vite migration can replace only the embedded directory; the API and backend do not change.

Frontend architecture:

- Static assets served from `go:embed`, with history fallback for `/b/{slug}`.
- Client-side routing for boards; settings and login are modal flows.
- Small API wrapper with CSRF handling and native `fetch`; no axios.
- Plain state object plus event delegation; no Redux.
- Native HTML5 drag/drop for tiles, with move-up/move-down buttons as a keyboard-accessible alternative.
- CSS custom properties for theming; no CSS framework required.
- Text and dot status indicators; no chart library.
- Bundle budget: under ~150 KB gzip initial JS.
- Accessibility: focus management, ARIA labels, reduced motion, keyboard navigation.

Pages:

- Board view: pinned row, sections, tiles, weather card, search, status dots.
- Layout toggles can hide the large board header, search bar, clock, and weather. When the header is hidden, clock/weather move into the top bar.
- Login/unlock and first-run setup.
- Settings: theme, search, status, weather, discovery, import/export, about.
- Edit mode: inline add/edit/delete, drag/drop, visibility, icon picker, board/section management.

## 10. Discovery

### 10.1 Docker

- Connect over Unix socket or TCP.
- Default socket: `/var/run/docker.sock`.
- Read-only operations only: list containers, inspect labels.
- Labels:
  - `blackflame.enable`
  - `blackflame.name`
  - `blackflame.url`
  - `blackflame.icon`
  - `blackflame.group`
  - `blackflame.hidden`
- Flame compatibility:
  - `flame.type=application` or `flame.type=app`
  - `flame.name`
  - `flame.url`
  - `flame.icon`
  - `;`-separated multiple apps.
- URL is required. Containers without a URL are skipped.
- Poll every 30 seconds by default.
- A read-only socket proxy is recommended; direct socket access is root-equivalent.

### 10.2 Kubernetes

- In-cluster authentication: service account token and CA.
- Standalone authentication: API URL, token file, and CA file are configured in Settings → Discovery or the YAML `discovery.kubernetes` block. Kubeconfig parsing is planned after 0.1.
- Read-only RBAC: list/get Ingresses and HTTPRoutes.
- Endpoints:
  - `/apis/networking.k8s.io/v1/ingresses`
  - `/apis/gateway.networking.k8s.io/v1/httproutes` (404 tolerated)
- Annotations:
  - Flame: `flame.pawelmalak/type|name|url|icon`
  - Blackflame: `blackflame.dev/type|name|url|icon|group|hidden|order`
- URL derivation:
  - Use the explicit annotation URL if present.
  - Otherwise use the first rule host/path; scheme is HTTPS when TLS is configured.
  - Skip wildcard-only hosts unless an explicit URL is provided.
- Filters: namespaces, label selector, ingress class.
- Poll every 30 seconds; handle pagination with `limit`/`continue`.
- Surface 401/403 errors with actionable RBAC messages.
- No writes to the cluster.
- Traefik `IngressRoute` and additional Gateway API kinds are planned after 0.1.

### 10.3 Merge and overrides

- Discovery results are stored as items with `source` and `external_id`.
- Editing a discovered item in UI mode marks it as an override (`is_override=1`); later syncs preserve the user's changes.
- GitOps mode is read-only; discovered items are merged into the runtime view and cannot be edited in the browser.
- UI export includes discovered items because they are stored as items; GitOps export uses the YAML source only.
- Last sync time, item count, and errors are exposed in the UI and metrics.

## 11. Status checks

- Scheduler with a bounded worker pool (default concurrency 16).
- Global interval (default 60 seconds), jitter to avoid synchronized bursts, and per-item enable/disable.
- HTTP/HTTPS GET.
- Timeout default 5 seconds.
- Expected status range default 200–399.
- Capture TLS certificate `NotAfter` on HTTPS.
- Record up/down, HTTP status, latency, certificate expiry, error, timestamp.
- Latest result in SQLite (or memory in GitOps mode without `--data`).
- Latest result only; Prometheus owns long-term history.
- No per-check SQLite history in 0.1; Prometheus owns long-term history.
- SSE events for changes; polling fallback every 15 seconds.
- Private items are excluded from anonymous status and SSE.
- Metrics use opaque item keys, not private names.

## 11.1 Widgets and integrations

- Optional widgets are defined under the top-level `widgets` config key.
- Built-ins: Pi-hole, Proxmox, Kubernetes, Prometheus, and iCal.
- Each widget has `enabled`, `public`, `title`, `refreshSeconds`, and optional
  TLS verification settings. All default to disabled.
- Secrets use `SecretRef` (`env` or `file`) and are resolved server-side. They
  are never included in widget responses.
- The widget manager fetches providers concurrently with a timeout, caches
  successful results for the configured refresh interval, and caches failures
  briefly to avoid hammering a broken service.
- `GET /api/v1/widgets` returns only public widgets to anonymous users and all
  widgets to authenticated administrators.
- Pi-hole supports v6 sessions with a v5 fallback.
- Proxmox uses API tokens and optionally aggregates all nodes.
- Kubernetes reuses the raw REST client and reports node/pod health.
- Prometheus queries cover NAS disk/SMART metrics through exporters.
- Calendar reads basic VEVENT entries; recurrence expansion is a follow-up.

## 12. Icons

- Source: `homarr-labs/dashboard-icons`, Apache-2.0.
- Pin a commit or tag for reproducibility; allow a custom base URL for offline mirrors.
- Embed a curated icon list at build time for the picker; arbitrary slugs are supported.
- Fetch only selected icons at runtime.
- Store bytes, content type, source URL, ETag, and fetch time in SQLite.
- Serve `GET /icons/{name}` with immutable cache headers and an ETag.
- Fallback: generated letter tile with a deterministic color.
- Custom uploads:
  - size limit (1 MiB);
  - allowed types: PNG, WebP, JPEG, sanitized SVG;
  - strip scripts, event handlers, external references, and embedded HTML from SVG;
  - serve with `X-Content-Type-Options: nosniff` and a restrictive CSP.
- Attribution for dashboard-icons in the README and About page.

## 13. Authentication and security

### 13.1 0.1 model

- Public read-only access.
- Single administrator/edit-mode password.
- No default password.
- Password source: first-run setup in UI mode; environment/secret in GitOps mode.
- Argon2id password hash.
- Session cookie: `HttpOnly`, `SameSite=Lax`, `Secure` when applicable.
- CSRF token or strict same-origin validation for mutations.
- Login rate limiting and lockout/backoff.
- Logout invalidates the session.

### 13.2 Private content

- Visibility is enforced in the data layer, not the UI.
- Anonymous bootstrap excludes private boards, sections, and items.
- Search, status, SSE, and metrics apply the same filter.
- Imported Flame `isPublic` values are preserved.

### 13.3 Headers and CSP

- `X-Content-Type-Options: nosniff`
- `Referrer-Policy: no-referrer`
- `Permissions-Policy` with unused features disabled.
- CSP with `default-src 'self'`; custom CSS served as a separate stylesheet.
- HSTS only when the external URL is HTTPS.
- Frame embedding disabled by default.

### 13.4 SSRF and secrets

- Status checks and future widgets can reach private addresses because homelab targets are private by nature.
- Block well-known cloud metadata endpoints by default.
- A general SSRF allowlist/policy is a later feature; homelab targets are private by design.
- Secrets are referenced from env/files; never returned to the browser.
- Optional encrypted secret storage is planned for widget credentials after 0.1.
- Logs redact secrets and credentials.

### 13.5 Future auth

- Forward-auth headers from trusted proxies (Authelia/Authentik) in 0.2.
- OIDC authorization code + PKCE and roles in 0.3.
- Multi-user RBAC is not part of 0.1.

## 14. Flame import

Source tables: `apps`, `categories`, and `bookmarks`.

| Flame | Blackflame |
|---|---|
| `apps` | Items of kind `app` on the default board |
| `categories` | Sections |
| `bookmarks` | Items of kind `bookmark`, linked to the mapped section |

Preserve `isPinned`, `orderId`, `isPublic`, `description`, and icon names. The import runs in a transaction, supports replace/merge, and is idempotent by URL/name where possible. Test fixtures cover Flame v2.x schema variations. Theme, search, and weather settings are configured after import; Flame's `config.json` is not part of the database import.

CLI paths:

- `blackflame import flame --db /path/db.sqlite --data /data`
- `blackflame import flame --db /path/db.sqlite --output /config/blackflame.yaml`

UI path: upload `db.sqlite`, choose replace/merge, import, and show a summary.

## 15. Deployment

### 15.1 Binary

- `blackflame serve --mode ui --data /data --listen :8080`
- Sets a 64 MiB soft Go heap limit by default; `GOMEMLIMIT` overrides it for larger deployments.
- `blackflame serve --mode gitops --config /config/blackflame.yaml --listen :8080`
- `blackflame validate --config /config/blackflame.yaml`
- `blackflame import ...`
- `blackflame export ...`
- `blackflame healthcheck`
- `blackflame version`

### 15.2 Container

- Multi-stage build: Go builder on `$BUILDPLATFORM`, Alpine runtime.
- `CGO_ENABLED=0`.
- Copy the CA bundle from the builder to avoid QEMU package installation.
- Non-root user; read-only root filesystem; writable `/data`.
- `EXPOSE 8080`.
- No Node.js in the runtime image.
- Target image size: 20–30 MB uncompressed.

### 15.3 Docker Compose

- Volume for `/data`.
- Optional socket proxy for Docker discovery.
- Health check via `blackflame healthcheck`.
- Reverse proxy examples for Caddy/Traefik/Nginx, including SSE buffering notes.

### 15.4 Kubernetes / Helm

- Deployment replicas: 1.
- Strategy: `Recreate` when SQLite is used.
- UI mode: PVC with `ReadWriteOnce`; avoid NFS for SQLite.
- GitOps mode: ConfigMap mounted at `/config`; optional `emptyDir` or PVC for runtime state.
- Security context: non-root, read-only root filesystem, drop capabilities, seccomp RuntimeDefault.
- Probes: `/healthz`, `/readyz`.
- Optional ServiceAccount/RBAC for discovery.
- Optional write RBAC only for future quick actions; never enabled by default.
- NetworkPolicy examples.
- ServiceMonitor/PodMonitor for metrics.
- Ingress/Gateway examples with SSE-friendly proxy settings.

### 15.5 systemd

- Hardened unit: `DynamicUser`, `StateDirectory`, `ConfigurationDirectory`, `ProtectSystem=strict`, `NoNewPrivileges`, `PrivateTmp`, `RestrictAddressFamilies`.
- Data directory under `/var/lib/blackflame`; config under `/etc/blackflame`.

## 16. Observability

- Structured JSON logs with request ID, method, path, status, duration, and errors.
- Human-readable log option for local development.
- `/healthz`: process alive.
- `/readyz`: config loaded, DB reachable, migrations complete.
- `/metrics`: Prometheus metrics.
- Optional pprof only on a localhost/debug listener, disabled by default.
- Build info: version, commit, build date, Go version.

Metric candidates:

- `blackflame_http_requests_total`
- `blackflame_item_up`
- `blackflame_item_latency_seconds`
- `blackflame_item_cert_expiry_timestamp`
- `blackflame_status_check_errors_total`
- `blackflame_discovery_last_success_timestamp`
- `blackflame_discovery_items`
- `blackflame_discovery_errors_total`
- `blackflame_build_info`

Private item names must not appear in anonymous metrics. Use opaque keys or exclude private items.

## 17. Testing

- Unit tests: config parsing/validation, visibility, ordering, status parsing, discovery JSON parsing, Flame import mapping, icon sanitization, auth/CSRF.
- Integration tests: `httptest` API tests with temporary SQLite; both config modes; fake Docker/Kubernetes API servers; status checks against `httptest` servers; SSE.
- Golden files: Flame database fixture, YAML configs, Docker containers JSON, Kubernetes Ingress/HTTPRoute JSON.
- E2E: Playwright against the built binary in UI and GitOps modes; setup, edit, drag/drop, import, search, keyboard, private visibility.
- Security: `govulncheck` in CI; container/image scanning with Trivy and dependency license checks are recommended before a public release.
- Load: 200 items, 50 checks, measure memory/CPU and response times.
- Multi-arch smoke test: run the built image on amd64 and arm64; build armv7.
- Accessibility: automated checks plus keyboard-only flows.

## 18. Dependency policy

- Latest stable, mutually compatible versions only.
- No prereleases, release candidates, `@next`, `main`-branch dependencies, or deprecated stacks.
- Pin `go.mod`/`go.sum`, `package-lock.json`, Docker tags, and action SHAs.
- Renovate/Dependabot updates deliberately; CI gates every bump.
- Publish `latest`, but deploy by semver or digest.
- Prefer the standard library and small, maintained modules.
- Avoid `client-go` and the Docker SDK.
- Current compatibility note: TypeScript 7.0.2 is npm `latest`, but `svelte-check` currently supports TypeScript 5/6. Use the newest compatible TypeScript 6.x until the Svelte toolchain supports 7.

Core candidates:

| Area | Choice |
|---|---|
| Go | 1.27.x |
| SQLite | `modernc.org/sqlite` |
| Migrations | `pressly/goose` |
| Config | `knadh/koanf` or stdlib + YAML |
| YAML | `gopkg.in/yaml.v3` |
| Metrics | Custom Prometheus text exposition (`client_golang` optional) |
| OIDC (later) | `coreos/go-oidc` |
| iCal | `arran4/golang-ical` |
| Frontend | Dependency-free ES modules (Svelte/React migration optional) |
| Drag/drop | Native HTML5 drag/drop |
| Base image | Alpine 3.24.x |

## 19. Risks and tradeoffs

| Risk | Mitigation |
|---|---|
| Scope creep | 0.1 is frozen to Flame parity + discovery/status; widgets/auth integrations follow |
| Discovery edge cases | Golden fixtures; explicit URL annotation; visible warnings; filters |
| Private data leakage | Central visibility filter; tests for API, SSE, metrics |
| Flame schema drift | Fixtures for v2.x; import summary; version detection |
| SQLite in Kubernetes | Single replica; `Recreate`; RWO PVC; no NFS |
| Docker socket security | Read-only socket proxy; direct socket documented as risky |
| Custom CSS/icon XSS | Separate stylesheet; SVG sanitization; strict headers |
| SSE behind proxies | Document buffering/timeouts; polling fallback |
| K8s RBAC surprises | Clear errors; namespace filters; Helm values |
| Alpine CVEs | Minimal packages; weekly rebuilds; Trivy scans |
| Frontend ecosystem gaps | Native HTML5 drag/drop; few dependencies; Svelte/React migration remains possible |
| Integration API drift | Curated widgets; version fallbacks (Pi-hole v5/v6); Prometheus generic path for long-tail services |
| TypeScript 7 incompatibility | Pin compatible TypeScript 6.x; revisit when tooling supports 7 |
| Binary size growth | No client-go/Docker SDK; build-size check in CI |

## 20. Architecture decision records

- [ADR 0001: Two configuration modes](docs/adr/0001-config-modes.md)
- [ADR 0002: Raw HTTP discovery](docs/adr/0002-raw-http-discovery.md)
