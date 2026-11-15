# Blackflame Helm chart

```sh
helm install blackflame deploy/helm/blackflame \
  --namespace blackflame --create-namespace \
  --set existingSecret=blackflame-auth
```

The secret must contain a `password` key. In UI mode, you can also omit the
secret and set the password through the first-run setup screen.

## Modes

### UI mode (default)

SQLite is authoritative. The chart creates a `ReadWriteOnce` PVC and mounts it at
`/data`.

```sh
helm install blackflame deploy/helm/blackflame \
  --set mode=ui \
  --set persistence.size=2Gi
```

### GitOps mode

YAML is authoritative and the UI is read-only. Provide the YAML inline or point
to an existing ConfigMap:

```sh
helm install blackflame deploy/helm/blackflame \
  --set mode=gitops \
  --set-file config=examples/blackflame.yaml
```

```sh
helm install blackflame deploy/helm/blackflame \
  --set mode=gitops \
  --set existingConfigMap=blackflame-config
```

The existing ConfigMap must contain a `blackflame.yaml` key.

## Kubernetes discovery

The chart installs a read-only ClusterRole by default (`rbac.create=true`,
`rbac.discovery=true`). After installation, enable Kubernetes discovery in
Settings → Discovery (UI mode) or in the YAML `discovery.kubernetes` block
(GitOps mode).

## Ingress and SSE

If you put the dashboard behind a reverse proxy, disable response buffering for
`/api/v1/events` so status updates stream correctly. For Traefik, add:

```yaml
traefik.ingress.kubernetes.io/router.middlewares: blackflame-no-buffering@kubernetescrd
```

or use your ingress controller's equivalent buffering setting.
