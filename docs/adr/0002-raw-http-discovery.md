# ADR 0002: Raw HTTP for Docker and Kubernetes discovery

- **Status:** Accepted
- **Date:** 2026-11-13
- **Deciders:** Blackflame maintainers
- **Related:** [Architecture](../../ARCHITECTURE.md), [Product](../../PRODUCT.md)

## Context

Blackflame needs to discover applications from two sources:

- Docker containers with labels.
- Kubernetes Ingress and HTTPRoute objects with annotations.

The obvious Go implementations are:

- `github.com/docker/docker/client` for Docker.
- `k8s.io/client-go` for Kubernetes.

Both are mature, but both are large dependency trees. `client-go` in particular pulls in API machinery, authentication plugins, informers, caching, and code generation support. The result is typically a binary tens of megabytes larger than necessary for a dashboard that only performs read-only list operations.

Blackflame's value proposition includes a small static binary and a small image. Discovery is a core feature, but it does not need:

- watches or informers;
- writes to Docker or Kubernetes;
- exec, logs, port forwarding, or pod operations;
- full API discovery;
- client-side rate limiting and leader election.

Polling every 30 seconds is sufficient for a homelab dashboard.

## Decision

Use **narrow raw HTTP clients** for Docker and Kubernetes discovery in the default build.

### Docker

- Connect over the Unix socket (default `/var/run/docker.sock`) or TCP.
- Use `net/http` with a custom `DialContext` for Unix sockets.
- Call `GET /containers/json` and read labels.
- Parse only the fields needed: ID, names, image, labels, state, ports.
- No Docker SDK.
- No write endpoints.
- Recommend a read-only socket proxy for production use.

### Kubernetes

- Use in-cluster service account credentials by default:
  - `KUBERNETES_SERVICE_HOST` / `KUBERNETES_SERVICE_PORT`;
  - `/var/run/secrets/kubernetes.io/serviceaccount/token`;
  - `/var/run/secrets/kubernetes.io/serviceaccount/ca.crt`;
  - namespace file.
- Allow explicit API URL, token file, and CA file in Settings → Discovery or the YAML `discovery.kubernetes` block for standalone use.
- Call the REST API directly:
  - `/apis/networking.k8s.io/v1/ingresses`
  - `/apis/gateway.networking.k8s.io/v1/httproutes`
- Parse minimal JSON structs for metadata, annotations, spec rules, TLS, and status.
- Handle pagination with `limit` and `continue`.
- Treat 404 for optional APIs as "not installed", not as a fatal error.
- Surface 401/403 with actionable RBAC messages.
- No `client-go`, no informers, no watches.
- No write operations.

### Shared behavior

- Poll every 30 seconds with jitter.
- Apply namespace, label-selector, and ingress-class filters where supported.
- Merge results with local overrides at query time.
- Never write discovery results back into content configuration.
- Expose last-success timestamps, item counts, and errors through metrics and the UI.
- Keep JSON parsing and URL derivation in small, unit-tested packages with golden fixtures.

## Consequences

### Positive

- Significantly smaller binary and image.
- Fewer transitive dependencies and CVEs.
- Easier multi-arch cross-compilation.
- No CGO requirements.
- Clear, auditable read-only behavior.
- The dashboard does not need cluster-admin or Docker write access.

### Negative

- More parsing code to maintain.
- Kubernetes API version changes must be handled manually.
- No watches; discovery latency is bounded by the poll interval.
- Kubeconfig parsing is deferred; standalone users need explicit token/CA flags initially.
- Docker TLS configuration is deferred beyond Unix socket/TCP.
- Some API edge cases (aggregated discovery, API groups, protobuf) are not covered.

### Neutral

- Polling is adequate for a homelab dashboard and avoids watch reconnect complexity.
- Discovery errors are expected to be visible and non-fatal.
- A future external agent could reuse the same interface if a source needs heavier client logic.

## Alternatives considered

### `client-go` and the Docker SDK

Rejected for the default build. The functionality used is a small fraction of what the SDKs provide, and the binary size cost conflicts with the product's core constraint. A separate "full" build could be considered later if a feature genuinely requires it, but it should not be the default.

### Shelling out to `kubectl` or `docker`

Rejected. It adds runtime dependencies, is hard to test, complicates containers, and weakens security and portability.

### External discovery agent

Rejected for 0.1. An agent adds deployment complexity and a second component to secure. It remains a possible future option for multi-cluster or restricted-network scenarios.

### Watches/informers

Rejected for 0.1. The added complexity and reconnect logic are not justified by a 30-second poll interval. Revisit only if discovery latency becomes a real user complaint.

## Implementation notes

- Use `http.Client` with explicit timeouts and a dedicated `Transport`.
- Set `Accept: application/json`.
- Limit response body sizes to avoid memory abuse from a compromised endpoint.
- Validate TLS by default; allow a custom CA; do not add an insecure-skip-verify default.
- Use `context.Context` for cancellation and shutdown.
- Back off on repeated errors; do not hammer a failing API.
- Log sanitized errors; never log tokens or container secrets.
- Add metrics for last success, item count, duration, and errors.
- Unit-test URL derivation from Ingress rules, TLS blocks, wildcard hosts, and HTTPRoute hostnames.
- Golden fixtures: Docker `/containers/json` responses and Kubernetes Ingress/HTTPRoute lists.
- Test 403/404 handling and pagination.
- Document the exact RBAC rules in the Helm chart and README.

## Follow-ups

- Add Traefik `IngressRoute` discovery after 0.1.
- Add kubeconfig parsing for standalone use.
- Consider Gateway API `Gateway`/`Listener` data for richer URL derivation.
- Consider Docker TLS for remote daemons.
- Re-evaluate watches only if polling proves insufficient.
