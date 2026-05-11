# Getting started

Run Compass with one source, see it on the dashboard, then layer on more
sources, custom catalog entries, and pages as you go. This page covers the
install paths, the shape of `compass.yaml`, and the tweaks most new
deployments want.

## Install

### Docker

`compass.yaml`:

```yaml
organization:
  name: Homelab

services:
  sources:
    - type: docker
      name: local
```

```bash
# Add the host's docker GID so the non-root container user can read the socket. Alternatively use: tecnativa/docker-socket-proxy
docker run --rm -p 8080:8080 \
  --group-add $(getent group docker | cut -d: -f3) \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  -v $PWD/compass.yaml:/etc/compass/compass.yaml:ro \
  adinhodovic/compass:latest \
  -c /etc/compass/compass.yaml
```

Multi-arch images for `linux/amd64` and `linux/arm64` ship on every release.

### Helm

`compass.yaml`:

```yaml
organization:
  name: Homelab

services:
  sources:
    - type: kubernetes
      name: cluster
      kubernetes:
        namespaces: []
```

```bash
helm install compass oci://ghcr.io/adinhodovic/charts/compass \
  -n compass --create-namespace \
  --set-file config=compass.yaml
```

The chart is distributed as an OCI artifact on GHCR; no `helm repo add` step
is needed. Helm 3.8 or newer is required.

Every chart knob lives in `deploy/charts/compass/values.yaml`.

## The `compass.yaml`

Every config has two top-level pieces:

```yaml
organization:
  name: Homelab        # display name in the navbar and PWA manifest

services:
  sources:             # one or more discovery sources
    - type: docker
      name: local
```

`organization` controls branding and metadata. `services.sources` is the
list of where Compass should look. Each source has a `type`, a unique
`name`, and per-type fields documented in [Sources](sources.md).

You can run with no real infrastructure by using the `static` source,
which is useful for testing the UI on a laptop:

```yaml
services:
  sources:
    - type: static
      name: manual
      services:
        - name: Grafana
          url: https://grafana.local
          tags: [observability]
```

Known software like Grafana picks up an icon, description, and default
tags from the embedded [Catalog](catalog.md), so two fields (`name`,
`url`) are enough.

## Open the dashboard

Browse to `http://localhost:8080/`.

- Search from the dashboard or press `Ctrl+K` for the command palette.
- Pin a service with the star button.
- Open `/debug` to see source health and the raw service data.
- Open `/metrics` to scrape Compass with Prometheus.

Filter, sort, and group state lives in the URL, so any view can be shared
as a link.

## Add more sources

Multiple sources run side-by-side; the registry merges and deduplicates.

| Want services from… | Source       | Read                                          |
| ------------------- | ------------ | --------------------------------------------- |
| Docker / Compose    | `docker`     | [Sources → Docker](sources.md#docker)         |
| Kubernetes          | `kubernetes` | [Sources → Kubernetes](sources.md#kubernetes) |
| Tailscale           | `tailscale`  | [Sources → Tailscale](sources.md#tailscale)   |
| Headscale           | `headscale`  | [Sources → Headscale](sources.md#headscale)   |
| Anything HTTP+JSON  | `api`        | [Sources → API source](sources.md#api-source) |
| Hand-curated        | `static`     | [Sources → Static](sources.md#static)         |

## Common tweaks

| Want to…                                    | Read                                                |
| ------------------------------------------- | --------------------------------------------------- |
| Gate Compass behind basic or forward auth   | [Configuration → Auth](configuration.md#auth)       |
| Change the default group-by, sort, or theme | [Configuration → UI](configuration.md#ui)           |
| Override icons, descriptions, or tags       | [Catalog](catalog.md)                               |
| Add markdown runbooks or docs to the navbar | [Pages](pages.md)                                   |
| Configure log format or level               | [Configuration → Logging](configuration.md#logging) |

## Updating Compass

**Docker:**

```bash
docker pull adinhodovic/compass:latest
```

Then recreate the container.

**Helm:**

```bash
helm upgrade compass oci://ghcr.io/adinhodovic/charts/compass \
  -n compass \
  --set-file config=compass.yaml
```

Releases follow semver. Breaking changes are called out in the changelog
before a major bump.

## When something looks wrong

- A source shows `error` on `/debug` — the message there is the failure
  reason. Most often it is credentials or network reachability.
- The dashboard is empty — confirm the source's service count on
  `/debug`, then check tags and filters in the URL.
- Compass will not start — run with `-c /path/to/compass.yaml` and watch
  the startup logs; config errors print the offending field.

For more, see [Troubleshooting](troubleshooting.md).

## Next

- [Demo](demo.md): walkthrough of dashboard, pages, and debug views.
- [Sources](sources.md): every integration's setup and field reference.
- [Configuration](configuration.md): the full `compass.yaml` field list.
- [Operations](operations.md): refresh, metrics, SIGHUP, logs.
- [Development](development.md): run the dev stack with every source live.
