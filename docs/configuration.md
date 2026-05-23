# Configuration

`compass.yaml` is the runtime config. Use `-c` / `--config` to point at a
different file.

## Top-level shape

```yaml
organization:
  name: Compass
  description: Homelab services with operational context.
  logo: /static/brand/favicon.svg
  full_logo: /static/brand/compass-logo.svg

ui:
  default_group_by: source  # or "tags"
  primary_color: "#2563eb"  # any CSS color; defaults to Tailwind blue-600

catalog:
  path: catalog/            # directory of *.yaml / *.yml override files

assets:
  dir: /etc/compass/assets   # optional mounted static asset directory

pages:
  dir: pages                # optional markdown pages directory

home:
  page: welcome             # optional; renders pages/welcome.md at "/"

auth:
  user_header: X-Forwarded-User
  email_header: X-Forwarded-Email
  groups_header: X-Forwarded-Groups

header_links:                # optional; rendered in the navbar
  - { label: Docs, url: https://docs.example.com, icon: lucide:book-open }

footer_links:                # optional; rendered in the footer
  - { label: Status, url: https://status.example.com }

services:
  filters:
    exclude_url_patterns: []
    dedupe_www: true
    exclude_wildcard_hosts: true
  sources:
    - type: static
      ...
```

Everything that contributes to the rendered service list lives under
`services:`: discovery `sources:` plus post-aggregation `filters:` that
narrow the combined output.

The whole file supports `${VAR}` interpolation before YAML parsing. Use
this for secrets and bootstrap-time values (Headscale API keys, OAuth
client secrets, etc.) without templating tooling. Bare `$VAR` is left
literal so dollar-containing values such as bcrypt hashes work inline.
Only the strict `${IDENTIFIER}` form is supported. Shell-style defaults
(`${VAR:-default}`) are rejected at load time so a typo never silently
passes through.

## Organization

| Field         | Notes                                                     |
| ------------- | --------------------------------------------------------- |
| `name`        | Required. Shown in the navbar and as the page title.      |
| `description` | Optional. Shown under the page header.                    |
| `logo`        | Optional compact icon. Same `icon:` syntax as services, plus absolute `http(s)` URLs and root-relative paths such as `/static/brand/favicon.svg`. Used for favicon, manifest, metadata, and header/footer fallback. |
| `full_logo`   | Optional wide logo or wordmark. Supports the same image URL/path handling as `logo`. When set, the header and footer show it instead of compact icon + name. |

## UI

| Field              | Notes                                                                      |
| ------------------ | -------------------------------------------------------------------------- |
| `default_group_by` | Initial grouping on the homepage: `source` (default) or `tags`.            |
| `primary_color`    | Sets daisyUI's `--color-primary`. Any CSS-parseable color (hex, `rgb()`, `hsl()`, `oklch()`). The dark-theme variant is auto-derived (same hue, lighter, slightly less chroma) so you only configure one value. Defaults to Tailwind blue-600 (`#2563eb`). |

Compass ships only light and dark themes. Pick one primary color; the
rest of the palette adapts.

## Assets

```yaml
assets:
  dir: /etc/compass/assets
```

| Field   | Notes |
| ------- | ----- |
| `dir`   | Optional read-only directory of operator-managed files, served under `/assets/*`. Relative paths are resolved from the config file directory. |

Use this for mounted local icons:

```yaml
assets:
  dir: /etc/compass/assets

services:
  sources:
    - type: static
      name: manual
      services:
        - name: Internal App
          url: https://app.example.com
          icon: /assets/internal-app.png
```

Compass serves files under `/assets/*` without directory listings.

## Logging

```yaml
logging:
  format: text     # text (default) | json
  level: info      # debug | info (default) | warn | error
```

| Field    | Default | Notes                                                                          |
| -------- | ------- | ------------------------------------------------------------------------------ |
| `format` | `text`  | `text` is slog's `key=value` handler. Use `json` for Loki, Datadog, or anything that parses JSON. |
| `level`  | `info`  | Controls the root logger threshold. `debug` enables per-request HTTP lines for `/health`, `/static/*`, and `/metrics`; non-static requests log at `info` regardless. 5xx responses always log at `error`. |

Every HTTP request gets one line via the request-logging middleware:
method, path, status, byte count, duration, and remote address.

## Catalog

`catalog.path` is a directory of `*.yaml` / `*.yml` overrides, merged onto
the embedded base in lexical order, per field. See [Catalog](catalog.md)
for examples and merge behavior.

## Pages

`pages.dir` is an optional directory of `.md` files. Each top-level file
becomes `/pages/{slug}`; sub-directories become their own nav dropdowns
at `/pages/{section}/{slug}`. See [Pages](pages.md).

## Home

`home.page` (and optional `home.section`) promotes a single markdown
page in `pages.dir` to the site root. The services dashboard moves to
`/services`. When omitted, `/` is the services dashboard. See
[Pages → Custom home page](pages.md#custom-home-page).

## Auth

Compass supports four modes:

- Open (default): no enforcement; user headers, if present, are used
  for personalization (pinned services, recents, notes scoped to the
  username) and the user menu in the navbar.
- Forward auth: set `auth.required: true` to delegate identity to an
  upstream proxy (oauth2-proxy, Authelia, Traefik forward-auth, Caddy
  forward_auth, Cloudflare Access). Requests without the user header get
  HTTP 401.
- Optional basic auth: populate `auth.basic.users` and leave
  `auth.required: false`. Anonymous users can browse public sources; the
  Login button opens the browser's Basic auth prompt at `/login`. Can be
  combined with forwarded identity, which follows the same
  `auth.trusted_proxies` trust rules as forward-auth mode.
- Required basic auth: same as optional, but with `auth.required: true`
  so Compass challenges every non-exempt route.

```yaml
auth:
  user_header: X-Forwarded-User       # default
  email_header: X-Forwarded-Email     # default
  groups_header: X-Forwarded-Groups   # default

  # required=false keeps public browsing enabled. With basic.users, this
  # shows a Login button and accepts credentials at /login. Without
  # basic.users, forwarded user/group headers are optional.
  required: false
  trusted_proxies:                    # optional; warn-only when omitted
    - 10.0.0.0/8
    - 192.168.1.5
  required_groups:                    # optional; when set, members of at least one are required
    - admins
    - ops

  # Basic auth. Set required=true above to protect the whole UI, or leave
  # required=false for public browsing plus optional login.
  basic:
    users:
      - name: admin
        password_hash: ${COMPASS_ADMIN_HASH}
```

| Field             | Default              | Notes                                  |
| ----------------- | -------------------- | -------------------------------------- |
| `user_header`     | `X-Forwarded-User`   | Username used for personalization scope and the user menu. |
| `email_header`    | `X-Forwarded-Email`  | Email shown in the user menu.          |
| `groups_header`   | `X-Forwarded-Groups` | Comma/semicolon/pipe-separated group list. Surfaced in the user menu and available to templates as `.User.Groups`. |
| `required`        | `false`              | When true, auth is enforced globally: Basic auth challenges when `basic.users` is set, otherwise forwarded auth requires `user_header`. When false, the UI is public and identity is optional. |
| `required_groups` | `[]`                 | Optional. When non-empty (and `required: true`), requests whose `groups_header` does not contain at least one of these values get 403. |
| `trusted_proxies` | `[]`                 | Optional CIDR/IP allowlist. When set, forwarded identity headers are trusted only from these remotes. Empty means trust any caller, which is fine when Compass is only reachable through the proxy. |
| `basic.users`     | `[]`                 | Each entry is `{name, password_hash, groups?}`. Hashes are bcrypt; generate with `htpasswd -BnC 10 USER` and strip the `USER:` prefix. The optional `groups` list is injected into `groups_header` on successful login, so basic-auth sessions can exercise the same group plumbing as forward auth (useful for local testing). |

Anonymous users in fully open mode share one browser-local storage scope.
Favorites, recents, and service notes persist in that browser. In optional-auth
deployments, anonymous service notes are read-only until the user signs in,
and optional-auth kiosk browsers do not share an anonymous notes key.

`/health`, `/static/*`, `/assets/*`, `/metrics`, and `/manifest.webmanifest`
are exempt from auth so probes, assets, scrapes, and PWA installs work
regardless of mode.

For public read-only browsing with private sources, leave `auth.required`
false and configure source access rules under `services.sources[].access`.
Authenticated users can be recognized by optional Basic auth or by a
trusted forward-auth proxy that injects the configured user and groups
headers. If Compass is reachable directly, set `auth.trusted_proxies` so
only the proxy's identity headers are believed. The binary logs a startup
warning when `required: true` is paired with an empty list, since spoofed
`X-Forwarded-User` headers would otherwise be trusted.

Basic-auth logout is best-effort because browsers cache Basic credentials.
Compass exposes `/logout` and shows a Logout link for Basic-auth users, but
some browsers may keep sending credentials until the tab is closed or saved
credentials are cleared.

## Debug

```yaml
debug:
  enabled: false   # default true
  required_groups: [admins]
```

`/debug` is on by default. Set `debug.enabled: false` to suppress the
route. Requests return 404, and the bug icon and footer link are
hidden from the rendered navbar. Useful when Compass runs in `auth:
open` mode and you'd rather not expose the source inventory at all.

Set `debug.required_groups` to keep `/debug` enabled but visible only to
users in at least one listed group. When unset, any user who can access
Compass can access `/debug`. Users allowed onto `/debug` see the full
source inventory, including sources hidden from their normal dashboard
view by `services.sources[].access`.

## Header and footer links

Both lists take the same shape. Header links render as inline navbar
buttons (icon optional); footer links render as plain links next to the
built-in `Services` / `Debug` links.

```yaml
header_links:
  - label: Docs
    url: https://docs.example.com
    icon: lucide:book-open      # optional; same syntax as service icons
    new_tab: true               # optional; auto-true for absolute URLs
  - label: Tools                # header-only dropdown
    icon: lucide:wrench
    links:
      - label: Grafana
        url: https://grafana.example.com
      - label: Prometheus
        url: https://prometheus.example.com

footer_links:
  - label: Status
    url: https://status.example.com
  - label: GitHub
    url: https://github.com/example/repo
```

| Field     | Notes                                                                       |
| --------- | --------------------------------------------------------------------------- |
| `label`   | Required. The visible link text.                                            |
| `url`     | Required. Absolute URL or site-internal path (`/services`, `/pages/foo`).   |
| `icon`    | Optional. Header only; footer links ignore it. Uses the same `icon:` syntax as services. |
| `new_tab` | Optional. Defaults to `true` for absolute URLs, `false` for relative paths. Override either way. |
| `links`   | Optional nested header links. When set, the parent renders as a navbar dropdown and its own `url` is ignored. |

## Filters

Post-aggregation filters narrow the rendered service list. They run in
the registry after every source has loaded, so the same rules apply
uniformly across `kubernetes`, `docker`, `tailscale`, `api`, and
`static`.

```yaml
services:
  filters:
    exclude_url_patterns:
      - "*.internal.example.com"
      - "staging.example.com"
    dedupe_www: true
    exclude_wildcard_hosts: true
  sources:
    - ...
```

| Field                    | Default | Notes |
| ------------------------ | ------- | ----- |
| `exclude_url_patterns`   | `[]`    | List of `path.Match` globs tested against each service's URL host. `*` spans `.` (it only stops at `/`), so `*.example.com` excludes any subdomain depth. A pattern with no glob meta-characters is treated as a domain suffix; `example.com` matches `example.com` and any subdomain. |
| `dedupe_www`             | `true`  | Drops a service whose host is `www.X` when another service with host `X` already exists. `www.lonely.com` with no companion survives. |
| `exclude_wildcard_hosts` | `true`  | Drops services whose URL host literally contains `*`. Mostly useful for Kubernetes HTTPRoute / Gateway resources with wildcard `spec.hostnames` (e.g. `*.example.com`), which would otherwise surface as non-navigable URLs. |

Set the booleans to `false` explicitly to opt out of the defaults.

## Sources

Each entry under `services.sources:` selects a discovery backend via
`type`. Every source supports a few common fields alongside its
type-specific config:

| Field              | Notes                                                                |
| ------------------ | -------------------------------------------------------------------- |
| `type`             | Required. One of `static`, `docker`, `kubernetes`, `tailscale`, `headscale`, `api`. |
| `name`             | Required. Used as the source instance name (shown in the UI and `/debug`). |
| `access.required_groups` | Optional. When set, services, details, command search entries, page shortcodes, and debug rows from this source are visible only to users in at least one listed group. Omit it or leave it empty for a public source. |
| `tags`             | Applied to every service from this source. Per-service tags are appended after these. |
| `refresh_interval` | How often to re-load the source after the initial boot-time load. Empty / unset uses the global default (`5m`). `"0"` or `"0s"` disables periodic refresh; the source loads once at boot and is not retried. Accepts any Go duration (`30s`, `2m`, `1h`). |

Example: public static links plus a Kubernetes source limited to `admins`
or `ops`:

```yaml
auth:
  required: false
  user_header: X-Forwarded-User
  groups_header: X-Forwarded-Groups
  trusted_proxies:
    - 10.0.0.0/8

services:
  sources:
    - type: static
      name: public
      services:
        - name: Status
          url: https://status.example.com

    - type: kubernetes
      name: cluster
      access:
        required_groups: [admins, ops]
      kubernetes:
        namespaces: []
```

See [Sources](sources.md) for the full per-type catalog and second-class
recipes.

## CLI flags and environment

The binary takes two flags. Anything else lives in `compass.yaml`.

| Flag                       | Env var                  | Default       | Notes                                  |
| -------------------------- | ------------------------ | ------------- | -------------------------------------- |
| `-l`, `--listen-address`   | `COMPASS_LISTEN_ADDRESS` | `:8080`       | HTTP listen address.                   |
| `-c`, `--config`           | `COMPASS_CONFIG`         | `compass.yaml` | Path to the config file. Relative or absolute. |

Everything else, including provider credentials, Tailscale OAuth,
Headscale API keys, Docker host, and Kubernetes bearer tokens, is set in
`compass.yaml`, optionally with `${VAR}` interpolation so secrets come from
the process environment without templating tooling:

```yaml
services:
  sources:
    - type: tailscale
      name: tailnet
      tailscale:
        tailnet_id: ${TAILSCALE_TAILNET_ID}
        oauth_scopes: [devices:core:read]
    - type: headscale
      name: headscale
      headscale:
        address: headscale.example.com:50443
        api_key: ${HEADSCALE_API_KEY}
```

The Kubernetes source has its own credential chain (in-cluster service
account, explicit `kubernetes.kubeconfig`, `KUBECONFIG`, `~/.kube/config`).
See [Sources → Kubernetes → Authentication](sources.md#authentication)
for the full resolution order and remote-cluster setup.

For runtime checks such as `/debug`, `/metrics`, and logs, see
[Operations](operations.md).
