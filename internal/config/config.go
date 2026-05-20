package config

import (
	"bytes"
	"fmt"
	"net"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/adinhodovic/compass/internal/compass"
	"go.yaml.in/yaml/v3"
)

type Config struct {
	Organization OrganizationConfig `yaml:"organization"`
	UI           UIConfig           `yaml:"ui"`
	Logging      LoggingConfig      `yaml:"logging"`
	Catalog      CatalogConfig      `yaml:"catalog"`
	Assets       AssetsConfig       `yaml:"assets"`
	Pages        PagesConfig        `yaml:"pages"`
	Home         HomeConfig         `yaml:"home"`
	Auth         AuthConfig         `yaml:"auth"`
	Debug        DebugConfig        `yaml:"debug"`
	HeaderLinks  []LinkConfig       `yaml:"header_links"`
	FooterLinks  []LinkConfig       `yaml:"footer_links"`
	Services     ServicesConfig     `yaml:"services"`
}

// DebugConfig toggles operator-only routes. Default-on so existing
// installs keep their /debug dashboard; set `enabled: false` to suppress
// the route entirely (any request returns 404).
type DebugConfig struct {
	Enabled        *bool    `yaml:"enabled"`
	RequiredGroups []string `yaml:"required_groups"`
}

// IsEnabled reports whether the debug surface is on. Defaults to true
// when unset, so callers that bypass setDefaults (tests, programmatic
// Config{} construction) keep the historical behavior.
func (d DebugConfig) IsEnabled() bool {
	return d.Enabled == nil || *d.Enabled
}

// LoggingConfig configures the slog handler used by the binary.
type LoggingConfig struct {
	// Format is "text" (default; key=value text handler) or "json" (slog
	// JSONHandler — useful when shipping to Loki / Datadog / a log
	// aggregator that parses JSON).
	Format string `yaml:"format"`
	// Level is "debug", "info" (default), "warn", or "error". Case
	// insensitive. Anything else returns a config error at load time.
	Level string `yaml:"level"`
}

// LinkConfig is one entry in header_links or footer_links. Renders as a
// hyperlink in the navbar (header) or footer.
type LinkConfig struct {
	// Label is the link text.
	Label string `yaml:"label"`
	// URL is where the link points. Internal (`/services`) or external
	// (`https://…`) — both work.
	URL string `yaml:"url"`
	// Icon is an optional logo / iconify spec (see docs/sources.md icon
	// resolution). Header links render with an inline icon; footer links
	// ignore it.
	Icon string `yaml:"icon"`
	// NewTab opens the link in a new tab (sets target=_blank, rel=noopener).
	// Defaults to true for absolute URLs, false for site-internal paths.
	// Set explicitly to override.
	NewTab *bool `yaml:"new_tab"`
	// Links are nested header links. When present, the parent renders as a
	// navbar dropdown and URL/NewTab on the parent are ignored.
	Links []LinkConfig `yaml:"links"`
}

var (
	bracedEnvRe    = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)
	anyBracedEnvRe = regexp.MustCompile(`\$\{[^}]*\}`)
)

// expandBracedEnv expands ${VAR} references against the process environment.
// Only the strict form (an ASCII identifier inside the braces) is supported;
// any other ${...} content (e.g. ${VAR:-default}, ${VAR-x}) is rejected so
// users are not silently surprised when shell-style defaulting does nothing.
//
// Unset variables fail closed: a missing ${VAR} is an error rather than a
// silent empty substitution, so a typo in TAILSCALE_OAUTH_CLIENT_SECRET
// can't quietly disable an upstream API call.
func expandBracedEnv(s string) (string, error) {
	for _, match := range anyBracedEnvRe.FindAllString(s, -1) {
		if !bracedEnvRe.MatchString(match) {
			return "", fmt.Errorf(
				"unsupported env interpolation %q: only ${VAR} is supported",
				match,
			)
		}
	}
	var missing []string
	expanded := bracedEnvRe.ReplaceAllStringFunc(s, func(match string) string {
		name := match[2 : len(match)-1]
		value, ok := os.LookupEnv(name)
		if !ok {
			missing = append(missing, name)
		}
		return value
	})
	if len(missing) > 0 {
		return "", fmt.Errorf(
			"unset environment variables referenced in config: %s",
			strings.Join(dedupe(missing), ", "),
		)
	}
	return expanded, nil
}

// dedupe returns names with duplicates collapsed, preserving first-seen
// order. Used so the missing-env error lists each variable once even
// when the config references it from multiple fields.
func dedupe(names []string) []string {
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

// OpensInNewTab returns the resolved new-tab behavior: explicit `new_tab`
// when set, otherwise true for absolute URLs and false for relative ones.
func (l LinkConfig) OpensInNewTab() bool {
	if l.NewTab != nil {
		return *l.NewTab
	}
	return strings.Contains(l.URL, "://")
}

// ServicesConfig groups everything that contributes to the rendered service
// list: discovery sources, plus post-aggregation filters that narrow the
// combined output.
type ServicesConfig struct {
	Filters FiltersConfig  `yaml:"filters"`
	Sources []SourceConfig `yaml:"sources"`
}

// FiltersConfig narrows the set of services rendered after all sources
// have loaded. Applied at the registry layer so all sources are filtered
// uniformly.
type FiltersConfig struct {
	// ExcludeURLPatterns: glob patterns matched against each service's URL
	// host (e.g. "*.findwork.dev"). Uses path.Match semantics — `*`
	// matches any sequence of non-`/` characters, so `*.findwork.dev`
	// matches `foo.findwork.dev` and `foo.bar.findwork.dev`.
	ExcludeURLPatterns []string `yaml:"exclude_url_patterns"`
	// DedupeWWW drops a service whose URL host is a `www.X` variant when
	// another service exists with host `X`. Default: true. Set false to
	// keep both.
	DedupeWWW *bool `yaml:"dedupe_www"`
	// ExcludeWildcardHosts drops services whose URL host contains a literal
	// `*` (typically Kubernetes HTTPRoute resources with wildcard
	// `spec.hostnames`). Default: true.
	ExcludeWildcardHosts *bool `yaml:"exclude_wildcard_hosts"`
}

// HomeConfig optionally points "/" at a markdown page from pages.dir
// instead of the services dashboard. When unset, "/" continues to render
// the services dashboard. The services dashboard is always reachable at
// /services regardless of this setting.
type HomeConfig struct {
	// Page is the slug of the markdown file to render. For example, with
	// pages.dir = "pages" and home.page = "welcome", the file
	// pages/welcome.md is rendered at /.
	Page string `yaml:"page"`
	// Section is the optional sub-directory under pages.dir.
	Section string `yaml:"section"`
}

// AuthConfig configures Compass's authentication surface. Four modes:
//
//   - **Open** (default): no enforcement; user headers, if present, are
//     read for personalization only.
//   - **Optional basic auth** (`basic.users` non-empty, `required: false`):
//     Compass stays public, shows a login link, and accepts basic-auth
//     credentials for users who want private source visibility.
//   - **Required basic auth** (`basic.users` non-empty, `required: true`):
//     Compass challenges every non-exempt request.
//   - **Forward auth** (`required: true`, no `basic.users`): Compass trusts an upstream
//     proxy (oauth2-proxy, authelia, traefik forwardauth, Caddy
//     forward_auth, etc.) to populate `user_header`. Requests without the
//     header get HTTP 401. `trusted_proxies` optionally limits which
//     upstream IPs are honored.
type AuthConfig struct {
	// UserHeader is the request header containing the username. Defaults to
	// "X-Forwarded-User". Read in all three modes.
	UserHeader string `yaml:"user_header"`
	// EmailHeader is the request header containing the email address.
	// Defaults to "X-Forwarded-Email". Read in all three modes.
	EmailHeader string `yaml:"email_header"`
	// GroupsHeader is the request header containing the user's groups.
	// Defaults to "X-Forwarded-Groups". The value is split on commas,
	// semicolons, or pipes; surrounding whitespace is trimmed. Read in
	// all three modes.
	GroupsHeader string `yaml:"groups_header"`
	// Required enforces auth. With basic.users, requests without valid Basic
	// credentials get HTTP 401. Without basic.users, requests without the
	// forwarded user header get HTTP 401.
	Required bool `yaml:"required"`
	// RequiredGroups, when non-empty, gates required auth: requests whose
	// groups don't intersect this set get HTTP 403. Ignored unless
	// `required: true`.
	RequiredGroups []string `yaml:"required_groups"`
	// TrustedProxies optionally restricts header trust to requests coming
	// from these IPs/CIDRs (e.g. ["10.0.0.0/8", "192.168.1.5"]). Empty =
	// trust headers from any caller (which is fine when Compass is only
	// reachable through the upstream proxy). When set with `required:
	// true`, requests from outside the allowlist get 403.
	TrustedProxies []string `yaml:"trusted_proxies"`
	// Basic enables HTTP basic auth.
	Basic BasicAuthConfig `yaml:"basic"`
}

// BasicAuthConfig holds the user list for HTTP basic auth.
type BasicAuthConfig struct {
	// Users is the list of accepted credentials. An empty list disables
	// basic auth.
	Users []BasicAuthUser `yaml:"users"`
}

// BasicAuthUser is one credential pair. Password is bcrypt-hashed —
// generate with `htpasswd -BnC 10 USER` (drop the `USER:` prefix) or any
// bcrypt tool. Groups is optional and lets local/basic-auth sessions
// exercise the same group plumbing as forward-auth headers.
type BasicAuthUser struct {
	Name         string   `yaml:"name"`
	PasswordHash string   `yaml:"password_hash"`
	Groups       []string `yaml:"groups"`
}

type OrganizationConfig struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Logo        string `yaml:"logo"`
	FullLogo    string `yaml:"full_logo"`
}

type UIConfig struct {
	DefaultGroupBy string `yaml:"default_group_by"`
	// PrimaryColor sets daisyUI's --color-primary. Accepts any
	// CSS-parseable color (hex, rgb, hsl, oklch, etc.). Dark-theme
	// variant is auto-derived (same hue, lighter, slightly less
	// chroma).
	PrimaryColor string `yaml:"primary_color"`
}

type CatalogConfig struct {
	Path string `yaml:"path"`
}

type AssetsConfig struct {
	// Dir is an optional read-only directory of operator-managed files served
	// under /assets. Useful for local service icons mounted into the container.
	Dir string `yaml:"dir"`
}

type PagesConfig struct {
	// Dir is a directory of *.md files; each becomes /pages/{slug}.
	Dir string `yaml:"dir"`
}

type SourceConfig struct {
	Type     string             `yaml:"type"`
	Name     string             `yaml:"name"`
	Access   SourceAccessConfig `yaml:"access"`
	Services []compass.Service  `yaml:"services"`
	// Tags applied to every service discovered from this source. Per-service
	// tags (from upstream metadata) are appended after these.
	Tags []string `yaml:"tags"`
	// RefreshInterval controls how often the source is re-loaded after the
	// initial boot-time load. Empty/unset uses the global default (5m).
	// "0" or "0s" disables periodic refresh for this source.
	RefreshInterval string            `yaml:"refresh_interval"`
	Endpoint        string            `yaml:"endpoint"`
	Headers         map[string]string `yaml:"headers"`
	Mapping         MappingConfig     `yaml:"mapping"`
	DNSSD           DNSSDConfig       `yaml:"dns_sd"`
	Kubernetes      KubernetesConfig  `yaml:"kubernetes"`
	Tailscale       TailscaleConfig   `yaml:"tailscale"`
	Headscale       HeadscaleConfig   `yaml:"headscale"`
	Docker          DockerConfig      `yaml:"docker"`
}

type SourceAccessConfig struct {
	// RequiredGroups limits this source to users whose auth groups intersect
	// the list. Empty means the source is visible to everyone.
	RequiredGroups []string `yaml:"required_groups"`
}

type DNSSDConfig struct {
	// Names are Prometheus-style DNS-SD names, such as
	// _http._tcp.example.lan. Only SRV records are supported because they carry
	// both host and port, which Compass needs to build dashboard URLs.
	Names []string `yaml:"names"`
	Type  string   `yaml:"type"`
	// Nameservers optionally overrides the system resolver. Entries are host:port
	// endpoints, for example 127.0.0.1:5353.
	Nameservers []string `yaml:"nameservers"`
	// URLScheme overrides the scheme inferred from the service name. When empty,
	// _https._tcp uses https and everything else uses http.
	URLScheme string `yaml:"url_scheme"`
}

type DockerConfig struct {
	// Host is the Docker Engine endpoint. Defaults to /var/run/docker.sock when
	// blank. Accepts unix:///path, tcp://host:port, or a bare path. Falls back
	// to the DOCKER_HOST env var when blank.
	Host string `yaml:"host"`
	// AutoDiscoverAll lists all containers when no compass.adinhodovic.com/enabled
	// label is set. Defaults to true; set to false to require the enabled label.
	AutoDiscoverAll *bool `yaml:"auto_discover_all"`
	// IncludeStopped lists non-running containers too. Defaults to false.
	IncludeStopped bool `yaml:"include_stopped"`
	// Tags applied to every discovered container service.
	Tags []string `yaml:"tags"`
	// URLScheme is the scheme used when deriving URLs from Traefik
	// `Host(...)` rules in container labels. Defaults to https.
	URLScheme string `yaml:"url_scheme"`
}

type KubernetesConfig struct {
	// Inline cluster credentials. When any of ClusterURL / BearerToken /
	// BearerTokenFile is set, a rest.Config is built directly from these
	// fields and Kubeconfig / KUBECONFIG / ~/.kube/config / in-cluster
	// fallbacks are bypassed. See docs/sources.md for the recommended
	// recipe for minting a long-lived ServiceAccount token.
	ClusterURL         string `yaml:"cluster_url"`
	ClusterCA          string `yaml:"cluster_ca"`      // inline PEM
	ClusterCAFile      string `yaml:"cluster_ca_file"` // path to PEM
	BearerToken        string `yaml:"bearer_token"`
	BearerTokenFile    string `yaml:"bearer_token_file"`
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify"`

	// Kubeconfig is the explicit-file escape hatch (multi-context kubeconfigs,
	// exec plugins like `aws eks get-token`, mTLS client certs, etc.). When
	// empty the loader falls back to KUBECONFIG, then ~/.kube/config, then
	// in-cluster service account credentials.
	Kubeconfig      string   `yaml:"kubeconfig"`
	Namespaces      []string `yaml:"namespaces"`
	AutoDiscoverAll *bool    `yaml:"auto_discover_all"`
}

type TailscaleConfig struct {
	TailnetID         string   `yaml:"tailnet_id"`
	TailnetName       string   `yaml:"tailnet_name"`
	OAuthClientID     string   `yaml:"oauth_client_id"`
	OAuthClientSecret string   `yaml:"oauth_client_secret"`
	OAuthScopes       []string `yaml:"oauth_scopes"`
	URLScheme         string   `yaml:"url_scheme"`
	Tags              []string `yaml:"tags"`
	// IncludeServices toggles VIP service discovery via Services().List().
	// Defaults to false because Tailscale Services are still in alpha and
	// not yet covered by API stability guarantees; opt in once the surface
	// settles.
	IncludeServices *bool `yaml:"include_services"`
	// IncludeDevices toggles tailnet device (node) discovery via
	// Devices().List(). Defaults to true. Requires the OAuth client to
	// carry `devices:core:read` (or broader).
	IncludeDevices *bool `yaml:"include_devices"`
	// ServiceTags are appended to every discovered VIP service, on top of Tags.
	ServiceTags []string `yaml:"service_tags"`
	// DeviceTags are appended to every discovered device service, on top of
	// Tags. Useful to distinguish devices from services (e.g. ["node"]).
	DeviceTags []string `yaml:"device_tags"`
	// ExcludeUnauthorized drops devices with Authorized=false. Default false.
	ExcludeUnauthorized bool `yaml:"exclude_unauthorized"`
	// ExcludeExternal drops devices shared into the tailnet (IsExternal=true).
	// Default false.
	ExcludeExternal bool `yaml:"exclude_external"`
	// ExcludeStaleAfter drops devices whose LastSeen is older than this
	// duration. Accepts Go duration strings ("720h") plus the convenience
	// suffixes "d" (days) and "w" (weeks). Empty/unset disables the filter.
	ExcludeStaleAfter string `yaml:"exclude_stale_after"`
}

type HeadscaleConfig struct {
	// Address is the Headscale gRPC endpoint (e.g. headscale.example.com:50443).
	Address string `yaml:"address"`
	// APIKey is the bearer API key minted via `headscale apikeys create`.
	APIKey string `yaml:"api_key"`
	// Insecure allows plaintext gRPC (no TLS). Dev-only; defaults to false.
	Insecure *bool `yaml:"insecure"`
	// URLScheme is prefixed onto each node's IP/hostname when deriving its
	// service URL. Defaults to http (Headscale tailnets often serve plain
	// HTTP for self-hosted services on the magic prefix).
	URLScheme string `yaml:"url_scheme"`
	// Tags applied to every discovered node service.
	Tags []string `yaml:"tags"`
	// IncludeDevices toggles node discovery. Defaults to true.
	IncludeDevices *bool `yaml:"include_devices"`
	// DeviceTags are appended on top of Tags for every node, useful to
	// distinguish them from regular services (e.g. ["node"]).
	DeviceTags []string `yaml:"device_tags"`
}

type MappingConfig struct {
	ItemsPath string `yaml:"items_path"`
	// ItemsMode is "" / "array" (the default; items_path must resolve to an
	// array) or "values" (items_path must resolve to a map whose values are
	// the items; useful for Consul's /v1/agent/services and similar
	// map-of-objects endpoints).
	ItemsMode string            `yaml:"items_mode"`
	URLScheme string            `yaml:"url_scheme"`
	Fields    map[string]string `yaml:"fields"`
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	// Expand ${VAR} references against the process environment so
	// secrets and bootstrap-time values (e.g. dev compose's HEADSCALE_API_KEY)
	// can be sourced from env without templating. Only the braced form is
	// supported so literal dollar-containing values like bcrypt hashes survive.
	expanded, err := expandBracedEnv(string(data))
	if err != nil {
		return Config{}, err
	}

	var cfg Config
	decoder := yaml.NewDecoder(bytes.NewReader([]byte(expanded)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, err
	}
	setDefaults(&cfg)
	if err := validate(&cfg); err != nil {
		return Config{}, err
	}

	resolvePathsRelativeToConfig(&cfg, filepath.Dir(path))

	if err := validateReferencedFiles(&cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// setDefaults applies every default field on cfg in one place so callers can
// see the entire defaulting story without scrolling through Load().
func setDefaults(cfg *Config) {
	if cfg.UI.DefaultGroupBy == "" {
		cfg.UI.DefaultGroupBy = compass.GroupBySource
	}
	if cfg.UI.PrimaryColor == "" {
		// Tailwind blue-600 — a recognizable, accessible default. Any
		// CSS-parseable color works here (hex, rgb(), hsl(), oklch()...);
		// daisyUI's `--color-primary` is just plumbed through.
		cfg.UI.PrimaryColor = "#2563eb"
	}
	cfg.Logging.Format = strings.ToLower(strings.TrimSpace(cfg.Logging.Format))
	if cfg.Logging.Format == "" {
		cfg.Logging.Format = "text"
	}
	cfg.Logging.Level = strings.ToLower(strings.TrimSpace(cfg.Logging.Level))
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
	if cfg.Auth.UserHeader == "" {
		cfg.Auth.UserHeader = "X-Forwarded-User"
	}
	if cfg.Auth.EmailHeader == "" {
		cfg.Auth.EmailHeader = "X-Forwarded-Email"
	}
	if cfg.Auth.GroupsHeader == "" {
		cfg.Auth.GroupsHeader = "X-Forwarded-Groups"
	}
	if cfg.Debug.Enabled == nil {
		on := true
		cfg.Debug.Enabled = &on
	}
	cfg.Assets.Dir = strings.TrimSpace(cfg.Assets.Dir)
}

// validate runs every defaults-independent semantic check. setDefaults must
// have run before validate so the value-set checks ("must be one of …") see
// the resolved values.
func validate(cfg *Config) error {
	if cfg.UI.DefaultGroupBy != compass.GroupByTags &&
		cfg.UI.DefaultGroupBy != compass.GroupBySource {
		return fmt.Errorf("ui.default_group_by must be tags or source")
	}
	if !safeCSSDeclarationValue(cfg.UI.PrimaryColor) {
		return fmt.Errorf("ui.primary_color contains unsafe CSS characters")
	}
	if cfg.Logging.Format != "text" && cfg.Logging.Format != "json" {
		return fmt.Errorf("logging.format must be text or json, got %q", cfg.Logging.Format)
	}
	switch cfg.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf(
			"logging.level must be debug|info|warn|error, got %q",
			cfg.Logging.Level,
		)
	}
	if len(cfg.Auth.RequiredGroups) > 0 && !cfg.Auth.Required {
		return fmt.Errorf(
			"auth.required_groups has no effect unless auth.required is true",
		)
	}
	if err := normalizeGroups("auth.required_groups", cfg.Auth.RequiredGroups); err != nil {
		return err
	}
	for i, user := range cfg.Auth.Basic.Users {
		if strings.TrimSpace(user.Name) == "" {
			return fmt.Errorf("auth.basic.users[%d]: name is required", i)
		}
		if strings.TrimSpace(user.PasswordHash) == "" {
			return fmt.Errorf(
				"auth.basic.users[%d] (%s): password_hash is required",
				i,
				user.Name,
			)
		}
	}
	if err := normalizeGroups("debug.required_groups", cfg.Debug.RequiredGroups); err != nil {
		return err
	}
	if err := validateTrustedProxies(cfg.Auth.TrustedProxies); err != nil {
		return err
	}
	if len(cfg.Services.Sources) == 0 {
		return fmt.Errorf("at least one services.source is required")
	}
	seenSources := map[string]struct{}{}
	for i, source := range cfg.Services.Sources {
		typeName := strings.TrimSpace(source.Type)
		name := strings.TrimSpace(source.Name)
		if name == "" {
			return fmt.Errorf("services.sources[%d]: name is required", i)
		}
		identity := typeName + "/" + name
		if _, ok := seenSources[identity]; ok {
			return fmt.Errorf("services.sources[%d]: duplicate source identity %q", i, identity)
		}
		seenSources[identity] = struct{}{}
		if err := normalizeGroups(
			fmt.Sprintf("services.sources[%d].access.required_groups", i),
			source.Access.RequiredGroups,
		); err != nil {
			return err
		}
		if typeName == compass.SourceTypeDNSSD {
			if err := validateDNSSDSource(i, source.DNSSD); err != nil {
				return err
			}
		}
	}
	for _, pattern := range cfg.Services.Filters.ExcludeURLPatterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" || !strings.ContainsAny(pattern, "*?[") {
			continue
		}
		if _, err := pathpkg.Match(pattern, "example.com"); err != nil {
			return fmt.Errorf(
				"invalid services.filters.exclude_url_patterns value %q: %w",
				pattern,
				err,
			)
		}
	}
	return nil
}

func validateDNSSDSource(i int, cfg DNSSDConfig) error {
	field := fmt.Sprintf("services.sources[%d].dns_sd", i)
	if len(cfg.Names) == 0 {
		return fmt.Errorf("%s.names must contain at least one name", field)
	}
	for j, name := range cfg.Names {
		name = strings.TrimSpace(name)
		if name == "" {
			return fmt.Errorf("%s.names[%d]: must be non-empty", field, j)
		}
		if _, _, _, ok := ParseDNSSDName(name); !ok {
			return fmt.Errorf(
				"%s.names[%d]: must be a service name like _http._tcp.example.lan",
				field,
				j,
			)
		}
	}
	for j, nameserver := range cfg.Nameservers {
		nameserver = strings.TrimSpace(nameserver)
		if nameserver == "" {
			return fmt.Errorf("%s.nameservers[%d]: must be non-empty", field, j)
		}
		if _, _, err := net.SplitHostPort(nameserver); err != nil {
			return fmt.Errorf("%s.nameservers[%d]: must be host:port", field, j)
		}
	}
	switch strings.ToUpper(strings.TrimSpace(cfg.Type)) {
	case "", "SRV":
		return nil
	default:
		return fmt.Errorf("%s.type must be SRV", field)
	}
}

// ParseDNSSDName splits a Prometheus-style DNS-SD name into LookupSRV parts.
func ParseDNSSDName(raw string) (service, proto, name string, ok bool) {
	parts := strings.Split(strings.Trim(strings.TrimSpace(raw), "."), ".")
	if len(parts) < 3 {
		return "", "", "", false
	}
	if !strings.HasPrefix(parts[0], "_") || !strings.HasPrefix(parts[1], "_") {
		return "", "", "", false
	}
	service = strings.TrimPrefix(parts[0], "_")
	proto = strings.TrimPrefix(parts[1], "_")
	name = strings.Join(parts[2:], ".")
	if service == "" || proto == "" || name == "" {
		return "", "", "", false
	}
	return service, proto, name, true
}

func normalizeGroups(field string, groups []string) error {
	for i, group := range groups {
		group = strings.TrimSpace(group)
		if group == "" {
			return fmt.Errorf("%s[%d]: must be non-empty", field, i)
		}
		groups[i] = group
	}
	return nil
}

// validateReferencedFiles fails fast when a config field points at a file or
// directory that doesn't exist. Runs after path resolution so relative paths
// are anchored against the config directory.
func validateReferencedFiles(cfg *Config) error {
	type entry struct {
		field string
		path  string
		isDir bool
	}
	mustExist := []entry{
		{"catalog.path", cfg.Catalog.Path, true},
		{"assets.dir", cfg.Assets.Dir, true},
		{"pages.dir", cfg.Pages.Dir, true},
	}
	for _, src := range cfg.Services.Sources {
		mustExist = append(mustExist,
			entry{"kubernetes.kubeconfig", src.Kubernetes.Kubeconfig, false},
			entry{"kubernetes.cluster_ca_file", src.Kubernetes.ClusterCAFile, false},
			entry{"kubernetes.bearer_token_file", src.Kubernetes.BearerTokenFile, false},
		)
	}
	for _, m := range mustExist {
		if m.path == "" {
			continue
		}
		info, err := os.Stat(m.path)
		if err != nil {
			return fmt.Errorf("%s: %w", m.field, err)
		}
		if m.isDir && !info.IsDir() {
			return fmt.Errorf("%s: %s must be a directory", m.field, m.path)
		}
		if !m.isDir && info.IsDir() {
			return fmt.Errorf("%s: %s must be a file", m.field, m.path)
		}
	}
	return nil
}

// safeCSSDeclarationValue rejects characters that can break out of the
// `--compass-primary: <value>;` declaration rendered into base.html.
func safeCSSDeclarationValue(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	return !strings.ContainsAny(value, "\n\r\t;{}<>\"'`\\")
}

func validateTrustedProxies(entries []string) error {
	for _, raw := range entries {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			if _, _, err := net.ParseCIDR(entry); err != nil {
				return fmt.Errorf("auth.trusted_proxies contains invalid CIDR %q: %w", entry, err)
			}
			continue
		}
		if ip := net.ParseIP(entry); ip == nil {
			return fmt.Errorf("auth.trusted_proxies contains invalid IP %q", entry)
		}
	}
	return nil
}

// resolvePathsRelativeToConfig rewrites every relative file/directory path
// in the config to be absolute, anchored at the config file's directory.
// Lets `go run ./cmd/compass -c deploy/dev/compass.yaml` work the same from
// project root, deploy/dev, or anywhere else, without users having to
// hand-write absolute paths in YAML.
func resolvePathsRelativeToConfig(cfg *Config, configDir string) {
	resolve := func(p string) string {
		if p == "" || filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(configDir, p)
	}
	cfg.Catalog.Path = resolve(cfg.Catalog.Path)
	cfg.Assets.Dir = resolve(cfg.Assets.Dir)
	cfg.Pages.Dir = resolve(cfg.Pages.Dir)
	for i := range cfg.Services.Sources {
		s := &cfg.Services.Sources[i]
		s.Kubernetes.Kubeconfig = resolve(s.Kubernetes.Kubeconfig)
		s.Kubernetes.ClusterCAFile = resolve(s.Kubernetes.ClusterCAFile)
		s.Kubernetes.BearerTokenFile = resolve(s.Kubernetes.BearerTokenFile)
	}
}
