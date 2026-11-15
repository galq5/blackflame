# Blackflame — Product Requirements

## 1. Summary

Blackflame is a self-hosted homelab dashboard and startpage. It is designed as a practical replacement for [Flame](https://github.com/pawelmalak/flame) with a much smaller operational footprint and first-class Kubernetes/Docker discovery.

It ships as:

- a single static binary for Linux, macOS, and Windows;
- a multi-arch Alpine container image;
- a Docker Compose deployment;
- a Helm chart for Kubernetes.

The project is intended to be open source; the license is still to be decided.

## 2. Problem

Flame provides a friendly GUI for apps, bookmarks, categories, search, themes, and simple integrations. In practice, homelab users still hit these limits:

- The Node.js runtime and dependencies dominate image size and idle memory.
- Configuration is tied to a SQLite database and GUI editing; GitOps and Kubernetes users want declarative YAML.
- Discovery is label/annotation-driven but limited to a single source and requires manual duplication.
- There is no built-in view of whether services are actually reachable.
- Icons depend on external lookups unless individually managed.
- A single startpage does not fit separate contexts such as home, work lab, and family.

Blackflame addresses those limits without becoming a full monitoring or automation platform.

## 3. Target users

Primary:

- Homelab operators running Docker Compose or a small Kubernetes/k3s cluster.
- Existing Flame users who want a low-risk migration path.
- Users who prefer a single binary and a small container image over a Node.js stack.

Secondary:

- Kubernetes users who want a read-only dashboard driven by a ConfigMap.
- Users who want a clean startpage with lightweight service status.

## 4. Positioning

> Flame-compatible startpage, Kubernetes/Docker-native discovery, live status, and a single small binary.

The differentiators, in priority order:

1. **Flame migration and compatibility.** Import `db.sqlite`; keep Flame label and annotation conventions working.
2. **Zero-config discovery.** Docker labels and Kubernetes Ingress/HTTPRoute annotations populate the dashboard automatically.
3. **Small footprint.** Static Go binary, Alpine image, low idle memory, multi-arch.
4. **Two configuration modes.** GUI-managed SQLite for everyday use; read-only YAML for GitOps.
5. **Live status.** Server-side checks, latency, status dots, and certificate expiry without CORS workarounds.
6. **Local-first assets.** Icons are cached and served locally; no CDN dependency at page load.

Blackflame is not trying to replace Homepage, Glance, Homarr, Grafana, or Uptime Kuma feature-for-feature. It is a focused startpage with enough operational awareness to be useful.

## 5. Principles

- **Small by default.** Avoid heavy SDKs and frameworks when a narrow HTTP client is enough.
- **No telemetry.** The dashboard does not phone home.
- **Local-first.** Icons, themes, and status are served by the instance.
- **Safe defaults.** No default password. Private data is filtered server-side. Discovery is read-only.
- **One source of truth per mode.** Never bidirectionally sync YAML and SQLite.
- **Boring operations.** Single binary, explicit flags, standard health endpoints, graceful shutdown.
- **Flame compatibility where it is cheap.** Reuse existing labels, annotations, icon names, and database concepts.

## 6. Release 0.1 scope

### In scope

| Area | Requirements |
|---|---|
| Startpage | Apps, bookmarks, categories/sections, pinning, ordering, public/private visibility |
| Boards | Multiple boards, board switcher, per-board visibility, `/b/{slug}` routes |
| Search | `/g`, `/yt`, `/gh`, `/w` prefixes, custom providers, local filtering, keyboard navigation |
| Themes | CSS-variable presets, custom CSS, dark/light presets, per-board override in YAML mode |
| Layout | Toggle the large board header, search bar, clock, and weather |
| Branding | Change the dashboard name, description, and logo |
| Integrations | Optional Pi-hole, Proxmox, Kubernetes, Prometheus/NAS, and iCal cards |
| Auth | Public read-only view; password-protected edit mode; sessions; CSRF; rate limiting |
| Import | One-click import of a Flame `db.sqlite`, plus a CLI import path |
| Icons | dashboard-icons cache, icon picker, custom uploads, local serving, letter-tile fallback |
| Status | HTTP/HTTPS checks, latency, status dots, TLS certificate expiry, SSE updates |
| Discovery | Docker labels, Kubernetes Ingress and HTTPRoute annotations, Flame annotation compatibility |
| Config | UI/SQLite mode and read-only GitOps/YAML mode from day one |
| Weather | Open-Meteo current conditions, since Flame has a weather widget |
| Operations | `/healthz`, `/readyz`, `/metrics`, structured logs, graceful shutdown |
| Packaging | Single binary, Alpine multi-arch image, Compose, Helm, systemd unit |

### Explicitly out of scope for 0.1

- Additional widgets beyond the built-in optional integrations.
- Collecting SMART or disk data directly from a NAS host; use Prometheus exporters instead.
- OIDC and forward-auth integration.
- Quick actions such as Wake-on-LAN, webhooks, and Kubernetes restarts.
- PWA/offline application shell.
- NAS disk and SMART integration.
- Plugin system or user-installable widgets.
- Multi-user accounts and RBAC.
- High availability or multiple replicas.
- HTML bookmarks import (Flame database import is the priority).
- Write access to Docker or Kubernetes.

## 7. User stories

### Startpage

- As a user, I can add an app with a name, URL, icon, description, and category.
- As a user, I can pin items so they appear in a prominent row.
- As a user, I can reorder items and sections by dragging.
- As a user, I can mark an item or board private so anonymous visitors cannot see it.
- As a user, I can create separate boards for home, work, and family with their own URLs.

### Search and keyboard

- As a user, I can type `/g query` to search Google in a new tab.
- As a user, I can type a name and see matching apps and bookmarks immediately.
- As a user, I can press `/` to focus search, use arrow keys to move, Enter to open, and Esc to clear.
- As a user, I can press a number key to open one of the first pinned items.

### Editing and authentication

- As an administrator, I unlock edit mode with a password.
- As an administrator, I can create, edit, hide, pin, reorder, and delete content.
- As an administrator, I can customize the theme and add custom CSS.
- As an anonymous visitor, I can view public content but cannot mutate anything.
- As an anonymous visitor, I cannot discover private item names through search, status, SSE, or metrics.

### Migration

- As an existing Flame user, I upload my `db.sqlite` and get my apps, categories, bookmarks, pins, ordering, visibility, descriptions, and icons.
- As an existing Flame user, my Docker labels and Kubernetes annotations continue to work.

### Discovery and status

- As a Docker user, containers with Blackflame or Flame labels appear automatically.
- As a Kubernetes user, annotated Ingresses/HTTPRoutes appear automatically.
- As a user, I can rename, re-icon, regroup, hide, or reorder discovered items without discovery overwriting my changes.
- As a user, I see a status dot, latency, and certificate-expiry warning for each checked app.
- As a user, I can disable checks per item; the check interval is configured globally.

### Configuration and operations

- As a GUI user, I configure everything in the browser and the data lives in SQLite.
- As a GitOps user, I mount a YAML file, the UI is read-only, and invalid changes do not take down the dashboard.
- As an operator, I can scrape `/metrics` for status and discovery health.
- As an operator, I can run the same binary in Docker, Kubernetes, or systemd.

## 8. Functional requirements

### Content model

- Boards contain sections; sections contain items.
- Item kinds in 0.1: `app`, `bookmark`, `weather`.
- Each board, section, and item has a visibility flag.
- Effective visibility is computed server-side: private ancestors make descendants private.
- Ordering uses explicit integer positions, rewritten transactionally after a move.
- Items support an optional stable ID. YAML items without an ID derive one from kind/name/URL; collisions are validation errors.

### Search

- A query beginning with a configured prefix and a space opens that provider in a new tab.
- Otherwise, the query filters local apps, bookmarks, and sections.
- Local results rank pinned items first, then name matches, then description/URL matches.
- Provider URLs must use `http` or `https`.
- Search providers are configurable in UI mode and YAML mode.

### Themes

- Themes are built from CSS custom properties.
- Presets include blackflame, light, nord, gruvbox, and catppuccin.
- The UI theme editor covers colors, surfaces, accent, radius, and custom CSS. YAML can set the full CSS-variable set, including font, shadows, tile size, and spacing.
- Custom CSS is served as a separate stylesheet, not injected inline.
- A board may override the global theme in GitOps/YAML mode; UI mode uses the global theme.

### Layout

- The large board header can be hidden.
- The search bar can be hidden independently.
- The clock and weather card can be hidden independently.
- When the large header is hidden, the clock and weather move into the top bar.
- These toggles live in Settings → Layout and are also available in YAML.

### Branding

- The dashboard name, description, and logo are editable in Settings → Branding.
- The default logo is the bundled Blackflame mark; users can choose a dashboard-icons slug or upload a custom logo.
- Branding is reflected in the top bar, page title, login screen, and footer.

### Integrations

- All integrations are optional and disabled by default.
- Pi-hole, Proxmox, Kubernetes, Prometheus, and iCal cards are supported.
- Widgets are fetched server-side, cached, and never expose secrets to the browser.
- Each widget can be public or authenticated-only.
- Prometheus is the supported path for NAS disk and SMART metrics via exporters.

### Authentication and visibility

- Public read-only access is the default.
- Edit mode requires a password; there is no default password.
- Passwords are stored as Argon2id hashes.
- Sessions use `HttpOnly`, `SameSite=Lax` cookies; `Secure` is set when the external URL is HTTPS.
- Mutations require a CSRF token or a validated same-origin request.
- Login attempts are rate-limited.
- Private content is excluded from public bootstrap responses, search, status, SSE, and metrics.

### Flame import

- Import supports Flame v2.x SQLite databases.
- Mapping: `apps` → app items, `categories` → sections, `bookmarks` → bookmark items.
- Theme, search, and weather settings are configured after import; Flame's `config.json` is not read by the one-click database import.
- Preserve `isPinned`, `orderId`, `isPublic`, `description`, and icon names.
- Icon names are preserved; custom icon files are not part of the database upload.
- The UI offers an upload flow with an import summary.
- The CLI supports import into UI mode or export to YAML.
- Import is transactional and idempotent under a replace/merge choice.

### Icons

- The default source is a pinned revision of `homarr-labs/dashboard-icons`.
- A curated icon list is embedded for the picker; arbitrary dashboard-icons slugs can be entered and are fetched on demand.
- Selected icons are fetched on demand, stored in SQLite, and served locally.
- Icons are served with long-lived cache headers and an ETag.
- Custom uploads are size-limited, content-type validated, and sanitized for SVG.
- Missing icons fall back to a generated letter tile.

### Status

- Checks run in the background with a global interval, per-item enable/disable, and jitter.
- 0.1 supports HTTP and HTTPS checks; TCP checks are planned for 0.2.
- Each check records up/down, HTTP status, latency, TLS certificate expiry, error, and timestamp.
- The latest result is stored; long-term history is delegated to Prometheus.
- Long-term history is delegated to Prometheus.
- SSE pushes updates; clients fall back to polling.
- Private status is not exposed to anonymous users.

### Discovery

- Docker discovery reads container labels over the Docker API.
- Kubernetes discovery reads Ingress and HTTPRoute objects over the Kubernetes API.
- Flame label and annotation conventions are supported.
- Discovered items can be edited in UI mode; edits become overrides. GitOps mode is read-only.
- Discovery never writes to Docker or Kubernetes.
- Discovery errors are visible in the UI and in metrics.

### Configuration modes

- `ui`: SQLite is the authoritative config store; the browser can mutate it.
- `gitops`: YAML is the authoritative config store; the browser is read-only.
- Runtime state is separate from content config in both modes.
- No bidirectional sync between YAML and SQLite.
- Invalid GitOps reloads keep the last good snapshot and surface an error.

### Operations

- `/healthz` reports process liveness without requiring dependencies.
- `/readyz` reports config loaded, database reachable, and migrations complete.
- `/metrics` exposes Prometheus metrics for status, discovery, HTTP requests, and build info.
- Logs are structured JSON by default, with a human-readable option.
- Shutdown is graceful: stop schedulers, finish in-flight requests, close the database.

## 9. Non-functional requirements

### Performance

- Idle RSS target: 20–40 MB.
- Container image target: 20–30 MB uncompressed.
- Startup target: under 100 ms on a modern x86-64 host.
- No Node.js runtime in production.
- Status checks and discovery must not block HTTP request handling.

### Portability

- Linux `amd64`, `arm64`, and `arm/v7` container images.
- Raw binaries for Linux, macOS, and Windows.
- No CGO requirement; pure-Go SQLite.
- Works behind a reverse proxy with a configurable base path and external URL.

### Security

- No default credentials.
- Private content filtered server-side.
- CSRF protection on mutations.
- Rate limiting on login.
- Secure headers and a restrictive CSP.
- SVG and uploaded icon sanitization.
- Read-only Docker/Kubernetes discovery.
- Secrets referenced from env or files, never returned to the browser.
- No arbitrary command execution.

### Accessibility

- Keyboard navigation for search, boards, and edit mode.
- Visible focus states.
- Sufficient color contrast in default themes.
- Reduced-motion support.
- Drag-and-drop has keyboard-accessible alternatives.

### Privacy

- No telemetry, analytics, or phone-home.
- Icons are fetched only for configured items and cached locally.
- Metrics labels must not leak private item names to unauthenticated scrapers.

## 10. Success metrics

For 0.1:

- A new user can go from `docker run` to a useful dashboard in under five minutes.
- A Flame user can import an existing database without retyping content.
- Discovery works on a real k3s cluster and a Docker host without custom glue.
- Idle memory stays below 50 MB with 100 items and 50 checks.
- The multi-arch image runs on amd64 and arm64, with armv7 built and smoke-tested.
- No private data appears in anonymous API, SSE, or metrics responses.

## 11. Non-goals

- Full uptime monitoring, alerting, or incident management.
- Container start/stop/delete or Kubernetes workload management.
- A general-purpose bookmark manager or read-later service.
- Multi-tenant SaaS, teams, or per-user dashboards.
- A plugin marketplace.
- Replacing Grafana, Uptime Kuma, Home Assistant, or *arr dashboards.
- Supporting arbitrary shell commands or scripts as actions.

## 12. Open decisions

- Frontend: 0.1 uses a dependency-free ES module SPA so the build stays Node-free. A future Svelte 5 + Vite or React 19 + Vite migration remains possible; the API is frontend-agnostic.
- License: MIT versus Apache-2.0.
- Project name and domain availability.
- Metrics are exposed on the main port in 0.1; a separate metrics listener/token is a later option.
- Standalone Kubernetes discovery: explicit API/token/CA settings fields first; kubeconfig parsing later.
- Whether to add HTML bookmarks import after 0.1.
