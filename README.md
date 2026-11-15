![Blackflame](assets/black-flame-128.png)

# Blackflame

A self-hosted homelab dashboard: a Flame-compatible startpage with Kubernetes and Docker discovery, live service status, and a single static binary.

> **Status:** 0.1 is implemented and usable. The repository contains the Go server, the embedded SPA, tests, container packaging, a Helm chart, and release automation. The license is still to be decided.

## Why Blackflame

Flame is a great startpage, but it carries a Node.js runtime, a React/Redux frontend, and a relatively large container image. Blackflame keeps the parts that make Flame pleasant to use and targets the parts that matter in a homelab:

- One static Go binary, no Node.js at runtime.
- Alpine-based multi-arch images for `amd64`, `arm64`, and `arm/v7`.
- A small idle footprint: target 20–40 MB RSS and a 20–30 MB image. The server applies a 64 MiB soft Go heap limit by default; override it with `GOMEMLIMIT` if needed.
- Two explicit configuration modes: UI-managed SQLite or read-only GitOps YAML.
- Docker label and Kubernetes Ingress/HTTPRoute discovery, including Flame annotations.
- Server-side status checks, latency, and TLS certificate expiry.
- One-click import of an existing Flame `db.sqlite`.
- Local icon cache from a pinned [dashboard-icons](https://github.com/homarr-labs/dashboard-icons) revision, so icons do not depend on a CDN at page load.
- A dependency-free ES module frontend embedded into the binary; no Node toolchain is required to build or run it.

## Quick start

### From source

```sh
go run ./cmd/blackflame serve --mode ui --data ./data
```

Open <http://127.0.0.1:8080>. On first run, set an edit password. If the instance will be reachable by others before you finish setup, set the password ahead of time:

```sh
BLACKFLAME_PASSWORD='change-me' go run ./cmd/blackflame serve --mode ui --data ./data
```

### Docker

The container runs as UID/GID `1000`. Create the data directory with matching
ownership first:

```sh
mkdir -p data
chown 1000:1000 data
docker build -t blackflame .
docker run --rm -p 8080:8080 -v "$PWD/data:/data" blackflame
```

Or use Compose:

```sh
mkdir -p data
chown 1000:1000 data
BLACKFLAME_PASSWORD='change-me' docker compose up -d --build
```

On rootless Docker or Podman, use your user's subordinate UID mapping or run the
container with `--user "$(id -u):$(id -g)"` and a data directory owned by that
user.

### GitOps mode

```sh
blackflame validate --config ./blackflame.yaml
BLACKFLAME_PASSWORD='change-me' blackflame serve --mode gitops --config ./blackflame.yaml --data= --listen :8080
```

GitOps mode treats YAML as the authoritative source and makes the UI read-only. The UI mode stores content in SQLite and can export YAML.

## Features

- Apps, bookmarks, sections, pinning, ordering, public/private visibility.
- Multiple boards with `/b/{slug}` routes.
- Search prefixes (`/g`, `/yt`, `/gh`, `/w`), custom providers, local filtering, keyboard navigation.
- CSS-variable themes, presets, custom CSS, per-board theme overrides in YAML mode.
- Layout toggles for the large board header, search bar, clock, and weather.
- Custom dashboard name, description, and logo.
- Optional integrations: Pi-hole, Proxmox, Kubernetes, Prometheus/NAS metrics, and iCal. All are off by default.
- Password-protected edit mode with Argon2id hashes, sessions, CSRF protection, and rate limiting.
- Flame `db.sqlite` import (apps, categories, bookmarks, pins, ordering, visibility, descriptions, icons).
- dashboard-icons cache with local serving and custom icon uploads.
- HTTP/HTTPS status checks, latency, status dots, TLS certificate expiry, SSE updates.
- Docker discovery by labels and Kubernetes Ingress/HTTPRoute discovery by annotations.
- Flame label/annotation compatibility.
- Prometheus metrics, health/readiness endpoints, structured logs.
- Weather via Open-Meteo.
- Single binary, Alpine image, Compose, Helm, systemd.

## Commands

```sh
blackflame serve --mode ui --data ./data --listen :8080
blackflame serve --mode gitops --config ./blackflame.yaml
blackflame validate --config ./blackflame.yaml
blackflame import flame --db ./db.sqlite --data ./data [--replace]
blackflame import flame --db ./db.sqlite --output ./blackflame.yaml
blackflame export --data ./data --output ./blackflame.yaml
blackflame migrate --data ./data
blackflame backup --data ./data --output ./blackflame-backup.db
blackflame healthcheck --url http://127.0.0.1:8080/healthz
blackflame version
```

## Development

```sh
make test      # go test ./...
make race      # go test -race ./...
make vet       # go vet ./...
make build     # build bin/blackflame
make docker    # build a local image
```

The frontend is plain HTML/CSS/ES modules under `internal/web/dist`, embedded with `go:embed`. It deliberately avoids a Node build step in 0.1. The API is frontend-agnostic, so a future Svelte or React migration can replace only that directory.

## Documentation

- [Product requirements](PRODUCT.md)
- [Architecture](ARCHITECTURE.md)
- [Roadmap](ROADMAP.md)
- [Discovery labels and annotations](docs/DISCOVERY.md)
- [Optional integrations](docs/INTEGRATIONS.md)
- [Helm chart](deploy/helm/blackflame/README.md)
- [Example GitOps config](examples/blackflame.yaml)
- [ADR 0001: Two configuration modes](docs/adr/0001-config-modes.md)
- [ADR 0002: Raw HTTP discovery](docs/adr/0002-raw-http-discovery.md)

## License

TBD. MIT and Apache-2.0 are the candidates. No license has been chosen yet.
