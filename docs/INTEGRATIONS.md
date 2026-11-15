# Optional integrations

All integrations are disabled by default. Configure them in **Settings →
Integrations** (UI mode) or under `widgets:` in YAML (GitOps mode). Secrets are
read from environment variables or files and are never sent to the browser.

Every widget also has:

- `enabled` — off by default;
- `public` — show the widget on the public, unauthenticated dashboard;
- `title` — optional card title;
- `refreshSeconds` — cache/fetch interval, minimum 10;
- `insecureSkipVerify` — optional TLS bypass for self-signed homelab services (not needed for Kubernetes, which uses a CA file).

## Pi-hole

```yaml
widgets:
  pihole:
    enabled: true
    public: true
    url: https://pihole.example.com
    secret:
      env: PIHOLE_PASSWORD
```

The widget tries the Pi-hole v6 API first, then falls back to the v5
`admin/api.php` endpoint. `secret` is the v6 password or v5 API token.

## Proxmox

```yaml
widgets:
  proxmox:
    enabled: true
    url: https://proxmox.example.com:8006
    tokenId: blackflame@pve!dashboard
    tokenSecret:
      file: /run/secrets/proxmox-token
    node: ""
    insecureSkipVerify: true
```

Shows node count, CPU, memory, and VM status. `node` is optional; leave it empty
to aggregate every node.

## Kubernetes

```yaml
widgets:
  kubernetes:
    enabled: true
    public: false
    api: ""
    token:
      file: /var/run/secrets/kubernetes.io/serviceaccount/token
    caFile: /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
    namespaces: []
```

Leave `api` empty when running inside the cluster. The service account needs
`get/list` on nodes and pods. The Helm chart includes those read-only permissions.

## Prometheus / NAS metrics

This is the recommended path for NAS disk, SMART, and other exporter data.

```yaml
widgets:
  prometheus:
    enabled: true
    url: http://prometheus:9090
    queries:
      - label: NAS root available
        query: node_filesystem_avail_bytes{mountpoint="/"}
        unit: bytes
      - label: NAS SMART temperature
        query: smartctl_device_temperature{temperature_type="current"}
        unit: "°C"
```

One line per query in the UI: `label|PromQL query|unit`.

## Calendar

```yaml
widgets:
  calendar:
    enabled: true
    url: https://calendar.example.com/feed.ics
    days: 7
    maxEvents: 5
```

Basic VEVENT entries are supported. Recurrence expansion is not yet implemented;
a recurring event may not appear unless the feed materializes the occurrence.
