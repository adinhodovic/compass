---
title: Welcome
order: 0
---

# Welcome to the dev Compass

This is a custom landing page rendered from `deploy/dev/pages/home.md`,
configured via `home.page: home` in `deploy/dev/compass.yaml`. The actual
service catalog moved to **Services** in the navbar.

## Quick links

- [Services](/services) — auto-discovered services from Docker, Caddy,
  Kubernetes, Tailscale, and Consul.
- The **Pages** dropdowns above for runbooks and admin docs.
- `Ctrl+K` opens the command palette anywhere on the site.

## Live monitoring stack

The shortcode `{{</* services tag=monitoring */>}}` expands server-side into the
card grid below — every service tagged `monitoring`, regardless of source:

{{< services tag=monitoring >}}

## Headscale tailnet

`{{</* services source=headscale */>}}` — the headscale source is first-class
(gRPC), bootstrapped on `make dev-up` with a demo user and two nodes:

{{< services source=headscale >}}

## What's here

| Source     | What it shows                                         |
| ---------- | ----------------------------------------------------- |
| Docker     | Local Compose services with `compass.adinhodovic.com/*` labels |
| Caddy      | Routes from Caddy's admin API                         |
| Consul     | Services registered in Consul (dev compose)           |
| Headscale  | Tailnet nodes via gRPC (`headscale-init` bootstrap)   |
| Kubernetes | Gateway API HTTPRoutes (when `kubectl` works)         |
| Tailscale  | Tailscale Services (when OAuth credentials are set)   |

The static source under `manual` adds a couple of curated entries (Grafana,
Internal Wiki, Pi-hole) on top.
