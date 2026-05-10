---
title: On-call Runbook
order: 2
tags: [ops, on-call]
---

# On-call runbook

A short example showing how an operational page looks rendered through
`prose`.

## Quick links

- [Grafana](http://localhost:3000) for dashboards
- [Caddy admin API](http://localhost:2019/config/) for live proxy state
- The Pi-hole admin UI at `/admin`

## Common incidents

### A service is missing from the homepage

1. Check the source it should come from (Docker / Kubernetes / Tailscale / API).
2. Confirm the source's discovery toggle (`auto_discover_all`, label selectors).
3. Check Compass logs for `Source load complete` with `services=N`.

### A logo is missing or wrong

1. Set `icon:` explicitly on the service or its catalog entry.
2. Prefer `dashboardicons:foo` for self-hosted apps (`selfhst:foo` still works).
3. Use Iconify names (`simple-icons:foo`, `lucide:bar`) or direct image URLs
   for everything else.

## Useful commands

```bash
make dev-up   # bring up Docker Compose deps
make dev      # run air + tailwindcss --watch
make assets   # rebuild CSS/JS once
```
