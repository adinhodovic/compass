# Operations

Operator-facing surfaces of the running binary: debug page, metrics,
SIGHUP refresh, and the full HTTP endpoint table.

## Endpoints

| Path                      | Purpose                                                            |
| ------------------------- | ------------------------------------------------------------------ |
| `/`                       | Services dashboard, or the configured home page when `home.page` is set. |
| `/services`               | Services dashboard. Always reachable, regardless of `home`.        |
| `/services/{id}`          | Per-service detail page (metadata + Grafana panels).               |
| `/pages/{slug}`           | Top-level markdown pages.                                          |
| `/pages/{section}/{slug}` | Pages from sub-directories.                                        |
| `/debug`                  | Per-source health (see below).                                     |
| `/health`                 | Liveness endpoint. Exempt from auth.                               |
| `/metrics`                | Prometheus metrics (see below). Exempt from auth.                  |
| `/manifest.webmanifest`   | PWA manifest with the configured organization logo.                |
| `/static/*`               | Embedded CSS/JS assets. No CDN, no runtime fetch. Served with `Cache-Control: public, max-age=86400` — release artifacts change with the binary, so a 1-day TTL is safe. |

## `/debug`

A per-source health view. For every configured source it shows a
status badge (`ok` / `error` / `pending`), the service count and last
load time, the discovery output as a table, and the error message when
the last refresh failed.

The navbar bug icon shows a small red dot whenever any source's last
load reported an error, so you don't have to keep `/debug` open to know
something is unhealthy.

On by default. Set `debug.enabled: false` in `compass.yaml` to suppress
the route — any request to `/debug` then returns 404.

![Compass debug page showing source health and discovered services](assets/images/debug.png)

## `/health`

Liveness endpoint for load balancers and orchestrators. Returns HTTP 200
with `{"status":"ok"}` and is unauthenticated regardless of the configured
auth mode.

## `/metrics`

Prometheus exposition format. Default scrape config is fine; the
endpoint is unauthenticated regardless of the configured auth mode so
scrapers don't need credentials.

| Metric                                          | Type      | Labels                                |
| ----------------------------------------------- | --------- | ------------------------------------- |
| `compass_http_requests_total`                    | counter   | `route`, `method`, `status`           |
| `compass_http_request_duration_seconds`          | histogram | `route`                               |
| `compass_source_refresh_total`                   | counter   | `source`, `outcome` (success\|error)  |
| `compass_source_refresh_duration_seconds`        | histogram | `source`                              |
| `compass_source_services`                        | gauge     | `source`                              |
| `compass_source_last_success_timestamp_seconds`  | gauge     | `source`                              |

The `source` label uses Compass's canonical `<type>/<name>` source identity,
for example `kubernetes/cluster`.

`last_success_timestamp_seconds` is the Unix time of the most recent
successful refresh. Pair with `time()` for stale-source alerting:

```promql
time() - compass_source_last_success_timestamp_seconds > 600
```

A ServiceMonitor for prometheus-operator ships with the Helm chart;
enable it with `serviceMonitor.enabled: true`.

## `SIGHUP`

`kill -HUP <pid>` forces every source to re-load immediately, regardless
of `refresh_interval`. Useful as a Kubernetes preStop hook or when you
want a manual "pull again now" without waiting for the next tick.

## Logs

Every HTTP request emits one structured log line: method, path, status,
byte count, duration, and remote address. Source refresh outcomes log
here too. `/health`, `/static/*`, and `/metrics` log at `debug` to keep
probe and scrape traffic quiet; everything else logs at `info`. 5xx
responses always log at `error`.

Configure the format and level in `compass.yaml` under
[`logging:`](configuration.md#logging).
