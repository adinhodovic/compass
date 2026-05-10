# Troubleshooting

Common failures and how to fix them. If something isn't here, open the
`/debug` page; it shows per-source status, last-load timestamps, and the
raw error string.

## "ui.primary_color contains unsafe CSS characters"

`ui.primary_color` is rendered into a server-side `<style>` block. The
config validator rejects characters that could break out of the
declaration: newlines, tabs, semicolons, braces, angle brackets, quotes.

**Fix:** use any well-formed CSS color (`#2563eb`, `oklch(70% 0.15
260)`, `rgb(37 99 235)`). Strip stray semicolons or quotes you may
have copy-pasted.

## "logging.level must be debug|info|warn|error"

The validator only accepts those four levels (case-insensitive). Common
mistakes: `verbose`, `trace`, `none`. Pick one of the four.

## "unsupported env interpolation \"${VAR:-default}\""

Compass's `${VAR}` interpolation is the strict braced form only — no
shell-style defaulting. The error fires on any `${...}` content that
doesn't match `[A-Za-z_][A-Za-z0-9_]*`.

**Fix:** drop the default and put the fallback elsewhere (e.g. a YAML
default below the field). If the secret really is optional, leave the
field blank in YAML and rely on the source's env-var fallback (Docker,
Tailscale, Headscale all read well-known env vars when their config
fields are blank).

## A source shows up in /debug with a stale `last loaded` time

The source's first load failed (or never ran). The `Error` column
shows the underlying message. Pair with the
`compass_source_last_success_timestamp_seconds` metric:

```
time() - compass_source_last_success_timestamp_seconds{source="kubernetes/my-cluster"} > 600
```

is a safe alert.

## "kubernetes: …" — credential resolution

Kubernetes credential resolution falls through this order:

1. Inline fields (`cluster_url` + `bearer_token`/`bearer_token_file`)
2. Explicit `kubeconfig` path
3. `KUBECONFIG` env var
4. `~/.kube/config`
5. In-cluster service account

When the binary runs at log level `debug`, it logs which path won:

```
DEBUG kubernetes credentials resolved source=cluster from=inline
```

If you're not sure which path is being taken, drop the log level to
`debug` for a single boot and check.

## RefreshInterval `""` vs `"0s"`

| Value      | Meaning                                          |
|------------|--------------------------------------------------|
| `""` / unset | Use the registry's global default (5 minutes). |
| `"0"` / `"0s"` | Disable periodic refresh. Source loads once at boot. |
| `"30s"`, `"5m"`, `"1h"` | Refresh on this interval.            |

If a source isn't refreshing and you didn't mean to disable it, double-
check that you wrote `""` and not `"0s"`.

## Static services don't appear

Likely causes:

- The URL doesn't parse. Set `logging.level: debug` in `compass.yaml`;
  static services with bad URLs log a warning at startup.
- The URL has no scheme. Compass prefixes `https://` when none is
  given, but it still has to parse cleanly.
- The service `name` is empty. Registry normalization drops nameless
  services.

## Templates render but no services appear

Open `/debug`. Either every source returned zero services, or every
service was filtered out. Common filter culprits:

- `services.filters.exclude_url_patterns` is too aggressive.
- `services.filters.exclude_wildcard_hosts` (default `true`) drops
  `*.example.com`.
- A Kubernetes route with no matching hostname yields zero endpoints.

## Docker socket permission denied

The container running Compass needs `r/w` on the Docker socket.
Either run as a user in the `docker` group, or mount the socket
read-only and accept that container labels are still visible.

## "address already in use" on `make dev`

`deploy/dev/docker-compose.yml` exposes ports for the test sources
(Headscale, Docker registry, etc.). Conflicts usually trace to a stale
compose stack from a previous session — `make dev-down` resolves it.
