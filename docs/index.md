# Compass

A landing page for your services, dashboards, and documents, discovered automatically from sources such as Docker, Kubernetes, and Tailscale.

![Compass services dashboard](assets/images/services-landing.png)


## Why Compass

Add a source and Compass renders what is exposed in a server-driven UI with
source status, tags, metadata, and operator context. Search, filter, debug,
and jump into services without maintaining a pile of hand-written cards.

- Docker, Kubernetes, Tailscale, Headscale, static config, and JSON API
  sources.
- Source health, refresh status, service metadata, panels, tags, and debug
  views.
- Server-rendered HTML with HTMX, Alpine.js, Tailwind CSS, and daisyUI. No
  SPA runtime.

## Quick start

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

Open `http://localhost:8080/`. For Helm, other sources, and a config
walk-through, see [Getting Started](getting-started.md). The
[Demo](demo.md) has more screenshots of pages, embedded panels, and the
debug view.

[dashboardicons]: https://dashboardicons.com/
[selfhst]: https://selfh.st/icons/
[iconify]: https://iconify.design/

## Where to go next

- [Getting Started](getting-started.md): install and run.
- [Demo](demo.md): screenshots of the dashboard, pages, and debug view.
- [Configuration](configuration.md): every `compass.yaml` field.
- [Sources](sources.md): integrations and the shared service model.
- [Catalog](catalog.md): default icons, descriptions, and tags.
- [Pages](pages.md): markdown content with live service shortcodes.
- [Operations](operations.md): endpoints, metrics, refresh, logs, and troubleshooting.
- [Development](development.md): local setup and asset pipeline.

## Acknowledgments

- [Dashboard Icons][dashboardicons] powers most catalog logos and aggregates
  upstream sets including [selfh.st/icons][selfhst].
- [Iconify][iconify] covers the rest, including `simple-icons:*` and
  `lucide:*`.
- [daisyUI](https://daisyui.com/) and [Tailwind CSS](https://tailwindcss.com/)
  for the UI layer.
- [HTMX](https://htmx.org/), [Alpine.js](https://alpinejs.dev/), and
  [Fuse.js](https://fusejs.io/) for client-side behavior.
- [goldmark](https://github.com/yuin/goldmark) for markdown.
- [Zensical](https://zensical.org/) builds these docs.
