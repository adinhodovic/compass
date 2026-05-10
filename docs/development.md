# Development

## Prerequisites

- Go 1.26+ (the version pinned in `go.mod`)
- Node.js 20+ (Tailwind / daisyUI compile, vendored JS deps from
  `node_modules`, and esbuild minify of `assets/app.js`)
- [`air`](https://github.com/air-verse/air) on `PATH` for the dev loop —
  `make dev` checks for it and prints the install hint when missing
  (`go install github.com/air-verse/air@latest`)
- Docker + Docker Compose (for local dev dependencies)

## Quick start

```bash
make dev-up   # bring up Docker Compose services declared in deploy/dev/
make dev      # `air` rebuilds Go + assets together
```

`make dev` runs `air`, which watches Go, HTML, CSS, JS, and YAML files
and rebuilds the binary in place. Asset rebuilds (Tailwind compile,
vendor copy, and `app.js` esbuild minify) run from air's `pre_cmd` via
`tools/build-assets.mjs`. A single change triggers one ordered rebuild,
which avoids racing the `//go:embed` against the asset writers.

Three QoL targets wrap `docker compose` so you don't have to remember
the `-f deploy/dev/docker-compose.yml` flag:

```bash
make dev-logs                    # tail every service in the dev stack
make dev-logs SERVICE=headscale  # tail one service
make dev-shell SERVICE=consul    # shell into a running container
make dev-restart SERVICE=caddy   # restart one service (omit for all)
```

`make dev-down` tears the stack down.

## The dev stack

`make dev-up` brings up a Compose stack that gives every first-class
source something real to talk to:

| Service | Purpose |
| ------- | ------- |
| `caddy` | Reverse proxy. Compass reads its admin API to discover routes. |
| `traefik` + `whoami` | Lets the Docker source exercise its Traefik label fallback. |
| `consul` | Two services pre-registered from `consul-services.json`; exercises the `api` source's `items_mode: values`. |
| `headscale` + `headscale-init` | Self-hosted tailnet. The init sidecar bootstraps a user, two demo nodes, and an API key on first run. |

`deploy/dev/compass.yaml` then wires up six sources (static, docker, caddy
via api, consul via api, headscale, plus optional kubernetes/tailscale if
their creds are available) against that stack. After `make dev`, useful
URLs:

- <http://localhost:8080/> — custom home page from `deploy/dev/pages/home.md`.
- <http://localhost:8080/services> — the discovered services dashboard.
- <http://localhost:8080/pages/architecture> — markdown page embedding live service cards via the `{{< services >}}` shortcode.
- <http://localhost:8080/debug> — per-source health and the raw discovery output for each one.

Skip a source by leaving its config block out or unsetting its env vars
(Tailscale and Kubernetes are skipped automatically when their creds
aren't set).

## Layout

```text
cmd/compass/             # CLI entrypoint
internal/
├── catalog/             # service metadata defaults + override loader
├── config/              # compass.yaml schema + Load
├── logo/                # icon/avatar resolution
├── pages/               # markdown page loader (goldmark)
├── compass/              # Service + Panel models
├── registry/            # normalize + sort + group
├── server/              # http handlers + html/template
└── source/              # discovery integrations (docker/kubernetes/...)
```

## Tests

```bash
make test                          # full run with coverage
go test ./internal/server          # template rendering
go test ./internal/source/...      # source mapping
```

The Kubernetes envtest will skip if the binaries aren't installed locally.
that's expected outside CI.

## Writing docs

These docs are rendered with [Zensical](https://zensical.org/). Run a local
preview with:

```bash
make docs            # live-reloading preview on :8001
make docs-build      # build the static site into site/
```

Both targets shell out to [`uvx`](https://docs.astral.sh/uv/) (`uv tool
run zensical ...`), so the only prerequisite is that `uv` is on your `PATH`.
No project virtualenv or global `pip install zensical` required. The
markdown files under `docs/` are picked up via the nav defined in
`zensical.toml`.
