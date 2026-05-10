<p align="center">
  <img src="docs/assets/images/compass-logo.svg" alt="Compass" width="560">
</p>

<p align="center">
  <strong>A landing page for your services, dashboards, and documents, discovered automatically from sources such as Docker, Kubernetes, and Tailscale.</strong>
</p>

<p align="center">
  <a href="https://adinhodovic.github.io/compass/getting-started/"><strong>Getting started</strong></a>
  ·
  <a href="https://adinhodovic.github.io/compass/"><strong>Docs</strong></a>
  ·
  <a href="https://adinhodovic.github.io/compass/demo/"><strong>Demo</strong></a>
</p>

<p align="center">
  <img src="docs/assets/images/services-landing.png" alt="Compass services dashboard" width="900">
</p>

## Why Compass

Add a source and Compass renders what is exposed in a server-driven UI with source status, tags, metadata, and operator context. Search, filter, debug, and jump into services without maintaining a pile of hand-written cards.

- Docker, Kubernetes, Tailscale, Headscale, static config, and JSON API sources.
- Source health, refresh status, service metadata, panels, tags, and debug views.
- Server-rendered HTML with HTMX, Alpine.js, Tailwind, and daisyUI. No SPA runtime.

## Quick start

Docker source `compass.yaml`:

```yaml
organization:
  name: Homelab

services:
  sources:
    - type: docker
      name: local
```

```bash
docker run --rm -p 8080:8080 \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  -v $PWD/compass.yaml:/etc/compass/compass.yaml:ro \
  adinhodovic/compass:latest \
  -c /etc/compass/compass.yaml
```

Kubernetes source `compass.yaml`:

```yaml
organization:
  name: Homelab

services:
  sources:
    - type: kubernetes
      name: cluster # Helm grants in-cluster RBAC for discovery
      kubernetes:
        namespaces: []
```

```bash
helm install compass oci://ghcr.io/adinhodovic/charts/compass -n compass --create-namespace --set-file config=compass.yaml
```

See [Getting started](https://adinhodovic.github.io/compass/getting-started/) for a minimal config and source examples.
