# Discovery reference

Blackflame reads labels and annotations from Docker containers and Kubernetes
Ingress/HTTPRoute objects. Discovery is read-only.

## Docker labels

```yaml
labels:
  blackflame.enable: "true"
  blackflame.name: "Immich"
  blackflame.url: "https://photos.example.com"
  blackflame.icon: "immich"
  blackflame.group: "Media"
  blackflame.hidden: "false"
```

Multiple apps can share one container by separating values with `;`:

```yaml
labels:
  blackflame.enable: "true"
  blackflame.name: "Immich;Photos backup"
  blackflame.url: "https://photos.example.com;https://backup.example.com"
  blackflame.icon: "immich;immich"
  blackflame.group: "Media;Media"
```

Flame labels are also supported:

```yaml
labels:
  flame.type: "application"
  flame.name: "Immich"
  flame.url: "https://photos.example.com"
  flame.icon: "immich"
```

The dashboard connects to `/var/run/docker.sock` by default. A read-only socket
proxy is recommended for production:

```yaml
services:
  docker-socket-proxy:
    image: tecnativa/docker-socket-proxy:latest
    environment:
      CONTAINERS: "1"
      POST: "0"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
```

Then set the Docker socket to `tcp://docker-socket-proxy:2375` in
Settings → Discovery.

## Kubernetes annotations

```yaml
metadata:
  annotations:
    blackflame.dev/type: "application"
    blackflame.dev/name: "Immich"
    blackflame.dev/url: "https://photos.example.com"
    blackflame.dev/icon: "immich"
    blackflame.dev/group: "Media"
    blackflame.dev/hidden: "false"
```

Flame annotations are supported as-is:

```yaml
metadata:
  annotations:
    flame.pawelmalak/type: "application"
    flame.pawelmalak/name: "Immich"
    flame.pawelmalak/url: "https://photos.example.com"
    flame.pawelmalak/icon: "immich"
```

If the URL annotation is omitted, Blackflame derives it from the first Ingress
rule (host + path, HTTPS when TLS is configured) or the first HTTPRoute
hostname/path.

### RBAC

The Helm chart installs a read-only ClusterRole by default. For a manual
deployment, the service account needs:

```yaml
rules:
  - apiGroups: ["networking.k8s.io"]
    resources: ["ingresses"]
    verbs: ["get", "list"]
  - apiGroups: ["gateway.networking.k8s.io"]
    resources: ["httproutes"]
    verbs: ["get", "list"]
  - apiGroups: [""]
    resources: ["nodes", "pods"]
    verbs: ["get", "list"]
```

The nodes/pods permissions are only needed for the optional Kubernetes widget.

If HTTPRoute discovery returns a permission error, verify that the Gateway API
CRDs are installed and the service account has the rule above. A missing CRD is
treated as "not installed" and does not fail the dashboard.
