# compass

A Helm chart that runs [Compass](https://github.com/adinhodovic/compass) — a
service-first homelab catalog that discovers services from Kubernetes,
Docker, Tailscale, Headscale, and any JSON HTTP endpoint.

## Install

```bash
helm install compass oci://ghcr.io/adinhodovic/charts/compass \
  -n compass --create-namespace
```

The chart is published as an OCI artifact on GHCR; no `helm repo add` is
needed. Helm 3.8 or newer is required.

## Configuration

Compass needs a `compass.yaml` to start. Three ways to provide it:

1. Inline in values (default) — the chart's `config:` value is a YAML
   string the chart turns into a ConfigMap mounted at `/etc/compass/compass.yaml`.
2. External — set `existingConfigMap.name` to a ConfigMap you manage
   yourself (must contain a `compass.yaml` key).
3. Secret — set `existingSecret.name` to a Secret you manage yourself
   (must contain a `compass.yaml` key). Use this when `compass.yaml`
   contains inline credentials.

Inline example:

```yaml
config: |
  organization:
    name: My Homelab
    description: Services across the cluster.

  services:
    sources:
      - type: kubernetes
        name: cluster
        kubernetes:
          namespaces: []
          auto_discover_all: true
```

`rbac.create` defaults to `true`, so the chart provisions the
`get/list/watch` permissions the kubernetes source needs. Set it to
`false` if you only run non-Kubernetes sources or supply your own RBAC.

The `${VAR}` interpolation in Compass config picks up values from the
container's environment, so secrets like `HEADSCALE_API_KEY` /
`TAILSCALE_OAUTH_CLIENT_SECRET` go through `env:`:

```yaml
env:
  - name: HEADSCALE_API_KEY
    valueFrom:
      secretKeyRef:
        name: headscale-api-key
        key: token
```

If you cannot avoid inline secrets in `compass.yaml`, store the whole config
in a Secret instead of the inline `config:` value:

```bash
kubectl create secret generic compass-config \
  -n compass \
  --from-file=compass.yaml=./compass.yaml
```

```yaml
existingSecret:
  name: compass-config
```

## Pages and catalog overrides

Compass reads markdown pages from a directory (`pages.dir`) and catalog
overrides from another (`catalog.path`). The chart wires those mounts
for you — three modes per dir:

1. **Inline content** — small enough to live in values:
   ```yaml
   config: |
     organization:
       name: My Homelab
     pages:
       dir: /etc/compass/pages
     catalog:
       path: /etc/compass/catalog
     services:
       sources: []

   pages:
     enabled: true
     files:
       home.md: |
         ---
         title: Home
         ---
         Welcome.
       on-call/runbook.md: |
         # Runbook

   catalog:
     enabled: true
     files:
       overrides.yaml: |
         grafana:
           tags: [observability, dashboards]
   ```

2. **Existing ConfigMap** — when content is managed via GitOps,
   Kustomize, or External Secrets:
   ```yaml
   pages:
     enabled: true
     existingConfigMap:
       name: compass-pages
   catalog:
     enabled: true
     existingConfigMap:
       name: compass-catalog
   ```

3. **Generic volume** — for PVCs, projected volumes, CSI drivers, or
   anything else, leave `pages.enabled: false` / `catalog.enabled:
   false` and use the chart-wide `volumes:` / `volumeMounts:` escape
   hatches.

Whichever mode you pick, point `config:` at the same paths
(`/etc/compass/pages`, `/etc/compass/catalog`) so Compass binary
finds the mounted directories. The chart sets the mountPath via
`pages.mountPath` and `catalog.mountPath`; defaults match the example
above.

ConfigMaps cap at ~1 MiB per object, so for larger page sets prefer
mode (2) or (3).

## RBAC

On by default. Most Helm installs use the kubernetes source against the
host cluster, and the chart bakes in `get/list/watch` on Gateway API
`HTTPRoute` and `GRPCRoute`, plus Kubernetes `Ingress` — the
kubernetes source's first-class discovery surface. Set
`rbac.create: false` if you only run non-Kubernetes sources or supply
your own RBAC out-of-band:

```yaml
rbac:
  create: true                    # default; set to false to opt out
  scope: ClusterRole              # or "Role" if kubernetes.namespaces is fixed
```

## Metrics

`/health` and `/metrics` are unauthenticated by default. The chart's
liveness/readiness probes use `/health`; Prometheus scrapes `/metrics`
without credentials. Turn on the ServiceMonitor when running
prometheus-operator:

```yaml
serviceMonitor:
  enabled: true
  scrapeInterval: 30s
```

## Values

| Key                          | Type   | Default                    | Description |
| ---------------------------- | ------ | -------------------------- | ----------- |
| `replicaCount`               | int    | `1`                        | Pod replicas. |
| `image.repository`           | string | `adinhodovic/compass`       | Image repository. |
| `image.tag`                  | string | `""` (chart `appVersion`)  | Override the image tag. |
| `image.pullPolicy`           | string | `IfNotPresent`             | Pull policy. |
| `imagePullSecrets`           | list   | `[]`                       | Image-pull secrets. |
| `nameOverride` / `fullnameOverride` | string | `""`               | Standard chart-name overrides. |
| `config`                     | string | (Kubernetes-source default) | Inline `compass.yaml`. Ignored if `existingConfigMap.name` or `existingSecret.name` is set. |
| `existingConfigMap.name`     | string | `""`                       | External ConfigMap (must have a `compass.yaml` key). |
| `existingSecret.name`        | string | `""`                       | External Secret (must have a `compass.yaml` key). Mutually exclusive with `existingConfigMap.name`. |
| `pages.enabled`              | bool   | `false`                    | Mount markdown pages at `pages.mountPath`. Set `pages.dir` in `config:` to the same path. |
| `pages.mountPath`            | string | `/etc/compass/pages`        | Where the pages volume mounts inside the container. |
| `pages.files`                | object | `{}`                       | Inline path → markdown content; nested keys (`on-call/runbook.md`) become sub-directories via `items:` projection. Ignored when `existingConfigMap.name` is set. |
| `pages.existingConfigMap.name` | string | `""`                     | External ConfigMap mounted instead of generating one from `files`. |
| `catalog.enabled`            | bool   | `false`                    | Mount catalog overrides at `catalog.mountPath`. Set `catalog.path` in `config:` to the same path. |
| `catalog.mountPath`          | string | `/etc/compass/catalog`      | Where the catalog volume mounts inside the container. |
| `catalog.files`              | object | `{}`                       | Inline filename → YAML content. Files merge in lexical order; later files win on conflict. |
| `catalog.existingConfigMap.name` | string | `""`                   | External ConfigMap mounted instead of generating one from `files`. |
| `env`                        | list   | `[]`                       | Container env vars; consumed by `${VAR}` interpolation in `config`. |
| `extraArgs`                  | list   | `[]`                       | Extra CLI args appended after `-c /etc/compass/compass.yaml`. |
| `serviceAccount.create`      | bool   | `true`                     | Create a ServiceAccount. |
| `serviceAccount.name`        | string | `""`                       | SA name (defaults to fullname). |
| `serviceAccount.annotations` | object | `{}`                       | Annotations on the SA. |
| `serviceAccount.automount`   | bool   | `true`                     | Mount the SA token into the pod. |
| `rbac.create`                | bool   | `true`                     | Create a (Cluster)Role + binding with the kubernetes source's default rules (get/list/watch on Gateway API HTTPRoute / GRPCRoute and Ingress). |
| `rbac.scope`                 | string | `ClusterRole`              | `ClusterRole` (default) or `Role`. |
| `service.type`               | string | `ClusterIP`                | Service type. |
| `service.port`               | int    | `8080`                     | Service port (also the container port). |
| `httpRoute.enabled`          | bool   | `false`                    | Create a Gateway API `HTTPRoute` for exposing Compass UI. Separate from the source's ability to discover existing Ingress objects. |
| `httpRoute.parentRefs`       | list   | one example                | `parentRefs:` (Gateway + namespace + optional sectionName). |
| `httpRoute.hostnames`        | list   | `[compass.adinhodovic.com]`     | `hostnames:` to attach. |
| `httpRoute.matches`          | list   | `[ {path: PathPrefix /} ]` | `matches:` rules. |
| `httpRoute.annotations`      | object | `{}`                       | HTTPRoute annotations. |
| `resources`                  | object | `{}`                       | Resource requests/limits. |
| `livenessProbe`              | object | `httpGet /health port: http` | Liveness probe. |
| `readinessProbe`             | object | `httpGet /health port: http` | Readiness probe. |
| `volumes` / `volumeMounts`   | list   | `[]`                       | Extra volumes/mounts on top of the config volume. |
| `nodeSelector` / `tolerations` / `affinity` | object/list | `{}` / `[]` / `{}` | Standard scheduling fields. |
| `podAnnotations` / `podLabels` | object | `{}`                     | Pod metadata. |
| `podSecurityContext` / `securityContext` | object | hardened (see [values.yaml](values.yaml)) | Pod / container security context. Defaults satisfy PSA `restricted`: `runAsNonRoot`, `runAsUser: 1001`, `readOnlyRootFilesystem`, `capabilities.drop: [ALL]`, `seccompProfile: RuntimeDefault`. Override per-field if your install needs different ids/caps. |
| `serviceMonitor.enabled`     | bool   | `false`                    | Emit a Prometheus Operator ServiceMonitor. |
| `serviceMonitor.scrapeInterval` | string | `30s`                  | Prometheus scrape interval. |
| `serviceMonitor.additionalLabels` | object | `{}`                | Extra labels for selector matching. |
| `serviceMonitor.namespace`   | string | `""` (release namespace)   | Where to create the ServiceMonitor. |
| `serviceMonitor.namespaceSelector` | object | (release namespace) | Override the namespaceSelector. |
| `serviceMonitor.targetLabels` | list  | `[]`                       | targetLabels. |
| `serviceMonitor.relabelings` / `metricRelabelings` | list | `[]`     | (Metric)RelabelConfigs. |
