# Sources

Sources are discovery integrations. Each one returns a list of
`compass.Service` values; the registry then normalizes them, fills in catalog
defaults, and sorts them.

All source entries live under `services.sources:` in `compass.yaml`. The
inline snippets below show only the per-source shape; wrap them in
`services: { sources: [ ... ] }` in your real config. See
[Configuration → Filters](configuration.md#filters) for the sibling
`services.filters:` block that narrows the combined output.

## Unified fields

Every source produces the same set of fields. Only the key names
differ per source.

| Concept          | Effect                                          | Annotation/label key                 | API mapping key | Static / catalog YAML key |
| ---------------- | ----------------------------------------------- | ------------------------------------ | --------------- | ------------------------- |
| `name`           | Display name                                    | `compass.adinhodovic.com/name`            | `name`          | `name`                    |
| `url`            | Primary URL (CSV `[Title=]URL,...` for k8s)     | `compass.adinhodovic.com/urls`            | `url`           | `url`                     |
| `description`    | Short blurb                                     | `compass.adinhodovic.com/description`     | `description`   | `description`             |
| `icon`           | Logo                                            | `compass.adinhodovic.com/icon`            | `icon`          | `icon`                    |
| `primary_tag`    | Main tag for grouping and card emphasis         | `compass.adinhodovic.com/primary-tag`     | `primary_tag`   | `primary_tag`             |
| `tags`           | Free-form tags                                  | `compass.adinhodovic.com/tags` (csv)      | `tags`          | `tags`                    |
| `links`          | Extra service actions (health, repo, runbook)   | `compass.adinhodovic.com/links`           | n/a             | `links:`                  |
| `enabled`        | Discovery gate (`true`/`false`)                 | `compass.adinhodovic.com/enabled` (label) | n/a             | n/a                       |
| `grafana-panels` | Grafana iframes; `Title=URL,Title=URL`          | `compass.adinhodovic.com/grafana-panels`  | n/a             | use `panels:` field       |

Where each source's keys live:

| Source       | Where the keys live                               |
| ------------ | ------------------------------------------------- |
| `docker`     | Container labels                                  |
| `kubernetes` | Resource annotations (`enabled` is a label)       |
| `tailscale`  | Tailscale Service annotations                     |
| `headscale`  | Native gRPC node fields; no annotation surface    |
| `api`        | `mapping.fields` map keys (values are `gjson` paths) |
| `static`     | Top-level YAML keys per service                   |

`Name` and `URL` are required. `Description`, `Icon`, and `Tags` are
catalog-backfilled when the source omits them.

### Field semantics

#### Icon resolution

`icon` accepts one of these forms:

| Form                    | Example                                | Use for                                       |
| ----------------------- | -------------------------------------- | --------------------------------------------- |
| `dashboardicons:<name>` | `dashboardicons:argo-cd`               | self-hosted software logos (preferred)        |
| `selfhst:<name>`        | `selfhst:argo-cd`                      | back-compat alias for selfh.st CDN            |
| Iconify spec            | `simple-icons:grafana`, `lucide:flask` | brands the icon packs lack, generic concepts  |
| Absolute URL            | `https://example.com/logo.svg`         | custom hosted images                          |
| Root-relative path      | `/assets/internal-app.png`             | files served from `assets.dir`, bundled images, or reverse-proxy-served images |

[dashboardicons]: https://dashboardicons.com/
[selfhst]: https://selfh.st/icons/

Use `dashboardicons:<name>` first for self-hosted apps; the
[Dashboard Icons][dashboardicons] project aggregates [selfh.st][selfhst]
plus a wider set of homelab/dashboard logos. The `selfhst:<name>` prefix
still works and points at selfh.st's CDN directly. Use `lucide:<name>` for
generic/internal tools, `simple-icons:<name>` for other brands, and URLs or
root-relative paths for custom images. For local mounted files, configure
`assets.dir` and point icons at `/assets/...`. Browse Dashboard Icons at
<https://dashboardicons.com/>, selfh.st at <https://selfh.st/icons/>, and
Iconify at <https://icon-sets.iconify.design/>.

If `icon` is empty after catalog backfill, the fallback is a source-type
icon, then a text avatar from the service name's initials. There is no
implicit favicon discovery.

#### Tags

Tags are deduplicated and order-preserving. They control grouping,
filtering, and search.

`primary_tag` is optional. When omitted, Compass uses the first tag. The
primary tag controls tag grouping and gets stronger emphasis on service cards.
When `primary_tag` is set but missing from `tags`, Compass prepends it.
Services with no tags land in `untagged`.

Source-side tag conventions:

- `static` top-level `tags:` prepends to every service's tags.
- `docker` appends the Compose project name when present.
- `tailscale` appends Tailscale's own tags after stripping the `tag:` prefix.
- `headscale` applies configured source/device tags, appends node tags after stripping `tag:`, and marks offline nodes with `offline`.

#### Metadata

`Metadata` is a free-form `map[string]any` rendered as a key/value table on
the detail page. URLs in the metadata table auto-render as links.

| Source       | Default metadata content                                             |
| ------------ | -------------------------------------------------------------------- |
| `docker`     | `compose_project`, `compose_service`                                 |
| `kubernetes` | `namespace`, `kind`, `labels`, route `hostnames` / `hostname`         |
| `tailscale`  | `name`, `tailnet`, `addresses`, `ports`, `tags`, device state fields |
| `headscale`  | `id`, `name`, `given_name`, `addresses`, `online`, `last_seen`, route maps |
| `api`        | none                                                                 |
| `static`     | Whatever the user wrote under `metadata:`                            |

Sensitive identifiers such as API keys, bearer tokens, Tailscale machine
keys, and Headscale node/machine keys are not rendered as metadata.

#### Links

Links are extra service actions shown on the service detail page. Use them
for health endpoints, metrics pages, source repositories, runbooks, or other
operator shortcuts. Compass only renders the links; it does not probe them.

Static services use the native `links:` field:

```yaml
services:
  sources:
    - type: static
      name: manual
      services:
        - name: Grafana
          url: https://grafana.example.com
          links:
            - label: Health
              url: https://grafana.example.com/api/health
              icon: lucide:heart-pulse
            - label: Repo
              url: https://github.com/grafana/grafana
              icon: simple-icons:github
            - label: Runbook
              url: /pages/on-call/grafana
              icon: lucide:book-open
              new_tab: false
```

Docker and Kubernetes use the `links` annotation/label:

```yaml
compass.adinhodovic.com/links: "lucide:heart-pulse|Health=https://grafana.example.com/api/health,lucide:book-open|Runbook=/pages/on-call/grafana"
```

Annotation entries are comma-separated and use this format:

| Form               | Example                                |
| ------------------ | -------------------------------------- |
| `Label=URL`        | `Runbook=/pages/on-call/grafana`       |
| `icon|Label=URL`   | `lucide:heart-pulse|Health=https://…`  |

Link URLs may be absolute `http(s)` URLs or root-relative site paths such
as `/pages/on-call/grafana`. Service links open in a new tab by default,
including root-relative links. Set `new_tab: false` on static links when
you want an in-app navigation. Annotation links always use the default.

#### Panels

Panels render as Grafana iframes on the service detail page and in
markdown page shortcodes. Use URLs from Grafana's **Share → Embed** flow,
usually `/d-solo/...` links.

Static sources use the native `panels:` field:

```yaml
services:
  sources:
    - type: static
      name: manual
      services:
        - name: Grafana
          url: https://grafana.example.com
          panels:
            - title: Cluster CPU
              url: https://grafana.example.com/d-solo/cluster/cpu?orgId=1&panelId=2
```

Docker and Kubernetes use the `grafana-panels` annotation/label:

```yaml
compass.adinhodovic.com/grafana-panels: "CPU=https://.../?panelId=1,Memory=https://.../?panelId=2"
```

Use one `Title=URL` entry for a single panel. Panel URLs that don't parse
as absolute URLs are skipped.

Panel URLs support a small set of service placeholders, useful for shared
Grafana dashboards with template variables:

| Placeholder        | Expands to            |
| ------------------ | --------------------- |
| `{{service.id}}`   | normalized service ID |
| `{{service.name}}` | service display name  |
| `{{service.type}}` | source type           |
| `{{service.url}}`  | service URL           |

Values are URL-escaped, so use them as Grafana query parameter values:

```yaml
compass.adinhodovic.com/grafana-panels: "Traffic=https://grafana.local/d-solo/services?panelId=2&var-service={{service.name}}"
```

Grafana and your ingress / SSO layer control embedding and authentication
behavior. For private embeds, Grafana usually needs
[`allow_embedding`](https://grafana.com/docs/grafana/latest/setup-grafana/configure-grafana/#allow_embedding):

```ini
[security]
allow_embedding = true
cookie_samesite = lax
```

#### Discovery gate

Per-source semantics for `compass.adinhodovic.com/enabled`:

- `docker`: when `auto_discover_all: true`, `enabled=false` excludes a container; when false, only `enabled=true` includes it.
- `kubernetes`: same pattern, applied per resource via the label.
- `tailscale`: not supported by the underlying API; whatever the OAuth scope can see is included.
- `api` / `static`: not applicable; filter at the endpoint or do not list the service.

#### Catalog defaults

The catalog, including built-in `internal/catalog/services.yaml` plus
optional overrides via `catalog.path`, is keyed by normalized service name
and provides defaults for source-omitted fields:

```yaml
service-slug:
  description: Short blurb.
  icon: selfhst:foo
  primary_tag: observability
  tags: [observability]
```

See [Catalog](catalog.md) for override semantics and per-field merge rules.

## First- and second-class support

Some sources have dedicated Go integrations. Others use the generic `api`
source with a mapping block. The split is mostly about behavior, not
importance.

### First-class

Compass has first-class support for sources with a dedicated Go
integration:

| Source       | Talks to                                                        |
| ------------ | --------------------------------------------------------------- |
| `static`     | Hand-written YAML in `compass.yaml`                              |
| `docker`     | Docker Engine socket (container labels)                         |
| `kubernetes` | Kubernetes API (HTTPRoute, GRPCRoute, Ingress)                  |
| `tailscale`  | Tailscale API (devices by default, Services opt-in)              |
| `headscale`  | Headscale gRPC API (self-hosted tailnet nodes)                  |

The split is intentional. First-class sources earn a dedicated Go client
when they need non-trivial logic: Kubernetes route expansion, Tailscale
OAuth refresh, Docker's Traefik-rule fallback, headscale's gRPC transport.
Anything that's just "GET a JSON endpoint" stays in `api`; adding a source
type per recipe would balloon the codebase without buying anything.

#### Static

Hand-curated YAML services. The full `compass.Service` shape is accepted:

```yaml
- type: static
  name: manual
  tags: [homelab]
  services:
    - name: Grafana
      url: https://grafana.local
      primary_tag: observability
      tags: [observability]
      metadata:
        environment: prod
      panels:
        - title: CPU
          url: https://grafana.local/d-solo/example?panelId=1
```

#### Docker

Discovers services from local Docker container labels. A container is
included when:

- it has a `compass.adinhodovic.com/urls` label, *or*
- it exposes a Traefik `Host(...)` router rule and `auto_discover_all` is
  true (see [Traefik support](#traefik-support) below).

```yaml
- type: docker
  name: local
  docker:
    host: /var/run/docker.sock     # or tcp://, or empty to use DOCKER_HOST
    auto_discover_all: true
    include_stopped: false
    url_scheme: https
```

Common service labels:

```yaml
compass.adinhodovic.com/urls: https://grafana.local
compass.adinhodovic.com/primary-tag: observability
compass.adinhodovic.com/tags: observability,core
```

Docker socket access is powerful. Compass only reads container metadata, but a
process with access to `/var/run/docker.sock` can usually ask the Docker daemon
to create containers, mount host paths, or otherwise control the host. Use the
direct socket mount for local or trusted deployments. For shared or exposed
deployments, put a restricted socket proxy in front of Docker and point Compass
at that instead.

Example with `tecnativa/docker-socket-proxy`:

```yaml
services:
  docker-socket-proxy:
    image: tecnativa/docker-socket-proxy:latest
    environment:
      CONTAINERS: 1
      INFO: 1
      VERSION: 1
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    restart: unless-stopped

  compass:
    image: adinhodovic/compass:latest
    ports:
      - "8080:8080"
    volumes:
      - ./compass.yaml:/etc/compass/compass.yaml:ro
    command: ["-c", "/etc/compass/compass.yaml"]
    depends_on:
      - docker-socket-proxy
    restart: unless-stopped
```

Then configure the Docker source to use the proxy:

```yaml
services:
  sources:
    - type: docker
      name: local
      docker:
        host: tcp://docker-socket-proxy:2375
```

##### Traefik support

There is no native Traefik source. Compass does not talk to the
Traefik API, read `traefik.yml`, or watch the file provider. What it
does is read Traefik-style labels off Docker containers via the docker
source, as a fallback when no `compass.adinhodovic.com/urls` label is set.

What is matched:

- HTTP routers only — labels of the form
  `traefik.http.routers.<name>.rule` whose value contains a
  `Host(\`...\`)` clause.
- Each hostname inside `Host(...)` clauses emits one service. Multi-host
  rules like `` Host(`a.example.com`, `b.example.com`) `` emit one service for
  `a.example.com` and one for `b.example.com`.
- Multiple HTTP routers on a single container are sorted by label name before
  hosts are extracted, so fan-out is deterministic.

Limits:

- TCP / UDP routers (`traefik.tcp.routers.*`, `HostSNI(...)` rules).
- Traefik file or KV providers. Only Docker-provider labels are read.
- Path matchers, middleware, router priority, and TLS settings. Compass only
  uses the host and `docker.url_scheme`.

The dev compose stack at `deploy/dev/docker-compose.yml` exercises
this fallback via the `whoami` service, which has no
`compass.adinhodovic.com/urls` label and only a Traefik rule.

#### Kubernetes

Discovers Gateway API `HTTPRoute` / `GRPCRoute` resources and Kubernetes
`Ingress` resources.

```yaml
- type: kubernetes
  name: cluster
  kubernetes:
    namespaces: []                  # empty = all namespaces
    auto_discover_all: true
```

`compass.adinhodovic.com/enabled=false` excludes a resource when
`auto_discover_all` is true; `compass.adinhodovic.com/enabled=true` includes it
when `auto_discover_all` is false.

Route behavior:

- `HTTPRoute` and `GRPCRoute` resources with multiple `spec.hostnames` emit one
  service per hostname.
- `Ingress` resources emit one service per `spec.rules[].host`.
- `compass.adinhodovic.com/urls` overrides route-derived hostnames. Use it when
  the route host is not the URL operators should open.

  ```yaml
  compass.adinhodovic.com/urls: "Docs=https://docs.example.com,Internal=https://app.internal"
  ```

  A single URL creates one card. Multiple comma-separated URLs create one card
  per URL. Optional `Title=` prefixes set the card name. The same syntax works
  on Docker containers via the `compass.adinhodovic.com/urls` label.

  ```yaml
  compass.adinhodovic.com/primary-tag: observability
  compass.adinhodovic.com/tags: observability,core
  ```

##### Authentication

Most installs use one of two modes.

###### Same cluster

If Compass runs inside Kubernetes, leave the auth fields unset. Compass uses
its Pod `ServiceAccount`.

Bind that account to a `Role` or `ClusterRole` that can `list` and `watch`:

- Gateway API `httproutes` and `grpcroutes`
- Kubernetes `ingresses`

###### Remote cluster

For a remote cluster, configure the API server and a read-only token:

```yaml
services:
  sources:
    - type: kubernetes
      name: prod-east
      kubernetes:
        cluster_url: https://prod-east.k8s.example.com:6443
        cluster_ca_file: /etc/compass/prod-east-ca.crt   # OR cluster_ca: |- inline PEM
        bearer_token: ${PROD_EAST_K8S_TOKEN}             # OR bearer_token_file: /var/run/.../token
        namespaces: []
        auto_discover_all: true
```

For multiple clusters, define one source per cluster.

Use `${VAR}` for `bearer_token` when your deployment injects secrets as
environment variables. Use `bearer_token_file` when secrets are mounted as
files or rotated by the platform.

###### Kubeconfig fallback

If you already manage kubeconfigs (multi-context kubeconfigs, exec
plugins like `aws eks get-token` / `gke-gcloud-auth-plugin`, mTLS
client certs, etc.), the source still accepts a kubeconfig path:

```yaml
- type: kubernetes
  name: staging
  kubernetes:
    kubeconfig: /etc/compass/kubeconfigs/staging.yaml
```

The kubeconfig's current context is used. When the inline
fields above are unset, the loader falls back to: explicit
`kubeconfig` → `KUBECONFIG` env var → `~/.kube/config` → in-cluster
service account.

#### Tailscale

Reads tailnet devices via the Tailscale API. Tailscale Services can also be
discovered, but are opt-in while that API is still alpha.

```yaml
- type: tailscale
  name: tailnet
  tailscale:
    tailnet_id: example.com         # or set TAILSCALE_TAILNET_ID
    oauth_scopes: ['devices:core:read']
    url_scheme: https
    tags: [tailscale]
```

Field reference:

| Field                 | Notes |
| --------------------- | ----- |
| `tailnet_id`          | Tailnet identifier. Falls back to `TAILSCALE_TAILNET_ID`. |
| `tailnet_name`        | Optional display name used in metadata. |
| `oauth_client_id`     | OAuth client ID. Falls back to `TAILSCALE_OAUTH_CLIENT_ID`. |
| `oauth_client_secret` | OAuth client secret. Falls back to `TAILSCALE_OAUTH_CLIENT_SECRET`. |
| `oauth_scopes`        | OAuth scopes. `devices:core:read` is added when `include_devices` is true; `services:read` is added when `include_services` is true. |
| `url_scheme`          | Scheme prefixed onto discovered hostnames. Defaults to `https`. |
| `tags`                | Tags applied to every discovered Tailscale service/device. |
| `include_services`    | Toggle Tailscale Services discovery. Defaults to `false`; the Services API is still in alpha. |
| `include_devices`     | Toggle tailnet device discovery. Defaults to `true`. |
| `exclude_unauthorized` | Drops unauthorized devices. Defaults to `false`. |
| `exclude_external`    | Drops external/shared-in devices. Defaults to `false`. |
| `exclude_stale_after` | Drops devices not seen within a Go duration such as `24h` or `168h`. Empty keeps stale devices. |
| `service_tags`        | Extra tags appended to Tailscale Services. |
| `device_tags`         | Extra tags appended to tailnet devices. |

To opt in once the API stabilizes:

```yaml
- type: tailscale
  name: tailnet
  tailscale:
    oauth_scopes: ['services:read', 'devices:core:read']
    include_services: true
    service_tags: [service]
```

#### Headscale

Reads nodes from a self-hosted [Headscale][headscale] coordinator over
gRPC. The dedicated client handles bearer-token auth, TLS, and the
device → service mapping.

```yaml
- type: headscale
  name: headscale
  tags: [vpn, headscale]
  headscale:
    address: headscale.example.com:50443   # gRPC endpoint
    api_key: ${HEADSCALE_API_KEY}
    url_scheme: http                        # prefixed onto each node's hostname
    include_devices: true                   # default true; nodes become services
    device_tags: [node]                     # appended to every node's tags
```

[headscale]: https://headscale.net

Field reference:

| Field             | Notes |
| ----------------- | ----- |
| `address`         | gRPC endpoint (`host:port`). Falls back to `HEADSCALE_ADDRESS` env. |
| `api_key`         | Bearer key from `headscale apikeys create`. Use `${VAR}` interpolation. |
| `insecure`        | Allow plaintext gRPC. Dev only. Defaults to `false`, i.e. TLS. |
| `url_scheme`      | Scheme prefixed onto each node's IP/hostname. Defaults to `http`. |
| `tags`            | Tags applied to every discovered node. |
| `include_devices` | Toggle node discovery. Defaults to `true`. |
| `device_tags`     | Extra tags appended on top of `tags` for every node. |

A working dev example lives in `deploy/dev/compass.yaml`. The
`headscale-init` sidecar in `deploy/dev/docker-compose.yml` bootstraps a
demo user, two nodes, and an API key on `make dev-up`.

### Second-class

Second-class integrations use the generic `api` source. They still produce
the same `compass.Service` values; they just do it through JSON field
mapping instead of a dedicated client.

#### API source

Generic JSON API mapping. Hits an HTTP endpoint and turns each item in the
response into a service via `gjson` path-based field extraction.

```yaml
- type: api
  name: caddy
  endpoint: http://localhost:2019/config/apps/http/servers/srv0/routes
  mapping:
    items_path: ""                  # default: root must be an array
    items_mode: array               # default; or "values" for map-of-objects
    url_scheme: http
    fields:
      name: "match.0.host.0"
      url: "match.0.host.0"
```

#### API recipes {#recipes-via-api}

Compass does not ship dedicated source types for these, but each is
"three lines of YAML against a JSON endpoint" away from working. They live
under the generic `api` source.

##### Consul (Agent Services)

Consul's `/v1/agent/services` returns a *map* keyed by service ID; each
value is the service object. Use `items_mode: values` to iterate the
map. Add a `Meta.url` on each registered service to give Compass a
canonical URL:

```yaml
- type: api
  name: consul
  endpoint: http://consul.local:8500/v1/agent/services
  mapping:
    items_mode: values
    fields:
      name: Service
      url: Meta.url      # set on the registering side
      tags: Tags
      primary_tag: Meta.primary_tag
```

There's a working dev example in `deploy/dev/docker-compose.yml` and
`deploy/dev/consul-services.json`. Consul auto-registers two services
from the JSON config on startup, no bootstrap sidecar needed.

##### Caddy (admin API)

Caddy's admin API exposes the live HTTP server config. Each route
under `/config/apps/http/servers/<server>/routes` includes its match
rules, so the route's first hostname becomes both the service name
and the URL:

```yaml
- type: api
  name: caddy
  endpoint: http://localhost:2019/config/apps/http/servers/srv0/routes
  mapping:
    url_scheme: http
    fields:
      name: "match.0.host.0"
      url: "match.0.host.0"
```

The dev stack at `deploy/dev/docker-compose.yml` runs Caddy with its
admin API exposed on `:2019` for exactly this recipe. Caddy's
admin API is unauthenticated by default — only expose it on a trusted
network.

##### Generic JSON API

Anything else: point at the endpoint and map fields. Paths use
[`gjson` syntax](https://github.com/tidwall/gjson/blob/master/SYNTAX.md), including array indexes and escaped dots in object
keys. The mapping language supports:

- Object paths (`spec.hostnames.0`, `match.0.host.0`).
- Escaped dots in object keys (`metadata.service\.name`).
- Top-level array (`items_path: ""`, `items_mode: array`, the default).
- Top-level map of objects (`items_mode: values`).
- A `url_scheme:` to prefix URLs returned without one.
- Headers via `headers:` for bearer tokens / API keys.

The pattern across all of these is the same: encode the canonical URL in
metadata at the source side, and let the API mapping read it. Compass
stays unaware of the registering system's specifics.
