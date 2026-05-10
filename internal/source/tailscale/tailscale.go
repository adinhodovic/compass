package tailscale

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/adinhodovic/compass/internal/compass"
	"github.com/adinhodovic/compass/internal/config"
	"github.com/adinhodovic/compass/internal/source/meta"
	"golang.org/x/oauth2/clientcredentials"
	tailscaleapi "tailscale.com/client/tailscale/v2"
)

// Well-known env vars consulted when the matching YAML field is empty. Lets
// single-tailnet homelab users get away with no compass.yaml secrets at all.
// Multi-tenant configs should use explicit ${VAR_HOME} / ${VAR_WORK}
// interpolation in YAML — the env fallback only fires when the YAML field
// is blank, so it never overrides an explicit value.
const (
	envTailnetID         = "TAILSCALE_TAILNET_ID"
	envOAuthClientID     = "TAILSCALE_OAUTH_CLIENT_ID"
	envOAuthClientSecret = "TAILSCALE_OAUTH_CLIENT_SECRET"
	envOAuthScopes       = "TAILSCALE_OAUTH_SCOPES"
)

// Auto-appended whenever the corresponding source is enabled so users don't
// get a 403 from leaving the scope out of OAuthScopes.
const (
	scopeServicesRead = "services:read"
	scopeDevicesRead  = "devices:core:read"
)

// Tag-convention prefixes that override per-device URL pieces. Tailscale
// devices have no annotations field, so ACL tags are the one knob users can
// twist per node from the Tailscale admin UI.
const (
	tagPrefixCompassPort   = "compass-port-"
	tagPrefixCompassScheme = "compass-scheme-"
)

const oauthTokenURL = "https://api.tailscale.com/api/v2/oauth/token"

type Source struct {
	name                string
	tailnetID           string
	tailnetName         string
	urlScheme           string
	tags                []string
	serviceTags         []string
	deviceTags          []string
	includeServices     bool
	includeDevices      bool
	excludeUnauthorized bool
	excludeExternal     bool
	excludeStaleAfter   time.Duration
	client              client
}

type client interface {
	Services() servicesAPI
	Devices() devicesAPI
}

type servicesAPI interface {
	List(ctx context.Context) ([]tailscaleapi.Service, error)
}

type devicesAPI interface {
	List(ctx context.Context) ([]tailscaleapi.Device, error)
}

type clientWrapper struct {
	client *tailscaleapi.Client
}

func (w clientWrapper) Services() servicesAPI {
	return w.client.Services()
}

func (w clientWrapper) Devices() devicesAPI {
	return devicesWrapper{resource: w.client.Devices()}
}

type devicesWrapper struct {
	resource *tailscaleapi.DevicesResource
}

func (w devicesWrapper) List(ctx context.Context) ([]tailscaleapi.Device, error) {
	// Pull all fields so ClientConnectivity (endpoints, DERP region),
	// advertised/enabled routes, and SSH/distro metadata are populated for
	// the detail page.
	return w.resource.List(ctx, tailscaleapi.WithFields(tailscaleapi.IncludeFieldsAll))
}

func New(cfg config.SourceConfig) (Source, error) {
	name := cfg.Name
	if name == "" {
		name = compass.SourceTypeTailscale
	}
	tailscaleConfig := cfg.Tailscale
	if tailscaleConfig.TailnetID == "" {
		tailscaleConfig.TailnetID = strings.TrimSpace(os.Getenv(envTailnetID))
	}
	if tailscaleConfig.OAuthClientID == "" {
		tailscaleConfig.OAuthClientID = strings.TrimSpace(os.Getenv(envOAuthClientID))
	}
	if tailscaleConfig.OAuthClientSecret == "" {
		tailscaleConfig.OAuthClientSecret = strings.TrimSpace(os.Getenv(envOAuthClientSecret))
	}
	if len(tailscaleConfig.OAuthScopes) == 0 {
		tailscaleConfig.OAuthScopes = parseScopes(os.Getenv(envOAuthScopes))
	}
	if tailscaleConfig.TailnetID == "" {
		return Source{}, errors.New("tailscale tailnet ID is required")
	}
	if tailscaleConfig.OAuthClientID == "" || tailscaleConfig.OAuthClientSecret == "" {
		return Source{}, errors.New("tailscale OAuth credentials are required")
	}

	oauthConfig := &clientcredentials.Config{
		ClientID:     tailscaleConfig.OAuthClientID,
		ClientSecret: tailscaleConfig.OAuthClientSecret,
		TokenURL:     oauthTokenURL,
		Scopes: oauthScopes(
			tailscaleConfig.OAuthScopes,
			meta.DefaultBool(tailscaleConfig.IncludeServices, false),
			meta.DefaultBool(tailscaleConfig.IncludeDevices, true),
		),
	}
	tailscaleClient := &tailscaleapi.Client{
		HTTP:    oauthConfig.Client(context.Background()),
		Tailnet: tailscaleConfig.TailnetID,
	}

	tailscaleConfig.Tags = meta.MergeTags(cfg.Tags, tailscaleConfig.Tags)

	if _, err := parseStaleDuration(tailscaleConfig.ExcludeStaleAfter); err != nil {
		return Source{}, err
	}

	return newWithClient(
		name,
		tailscaleConfig,
		clientWrapper{client: tailscaleClient},
	), nil
}

func newWithClient(name string, cfg config.TailscaleConfig, client client) Source {
	if cfg.URLScheme == "" {
		cfg.URLScheme = "https"
	}
	if len(cfg.Tags) == 0 {
		cfg.Tags = []string{compass.SourceTypeTailscale}
	}
	staleAfter, _ := parseStaleDuration(cfg.ExcludeStaleAfter)

	return Source{
		name:                name,
		tailnetID:           cfg.TailnetID,
		tailnetName:         cfg.TailnetName,
		urlScheme:           cfg.URLScheme,
		tags:                cfg.Tags,
		serviceTags:         cfg.ServiceTags,
		deviceTags:          cfg.DeviceTags,
		includeServices:     meta.DefaultBool(cfg.IncludeServices, false),
		includeDevices:      meta.DefaultBool(cfg.IncludeDevices, true),
		excludeUnauthorized: cfg.ExcludeUnauthorized,
		excludeExternal:     cfg.ExcludeExternal,
		excludeStaleAfter:   staleAfter,
		client:              client,
	}
}

// tailnetDisplay returns the human-friendly tailnet name when set,
// falling back to the tailnet ID for logs and metadata.
func (s Source) tailnetDisplay() string {
	if s.tailnetName != "" {
		return s.tailnetName
	}
	return s.tailnetID
}

func (s Source) Name() string {
	return s.name
}

func (s Source) Type() string {
	return compass.SourceTypeTailscale
}

func (s Source) LogAttributes() []slog.Attr {
	attrs := []slog.Attr{slog.String("tailnet", s.tailnetDisplay())}
	if s.tailnetName != "" && s.tailnetID != "" {
		attrs = append(attrs, slog.String("tailnet_id", s.tailnetID))
	}
	return attrs
}

func (s Source) Load(ctx context.Context) ([]compass.Service, error) {
	var services []compass.Service

	if s.includeServices {
		loaded, err := s.loadServices(ctx)
		if err != nil {
			return nil, err
		}
		services = append(services, loaded...)
	}

	if s.includeDevices {
		loaded, err := s.loadDevices(ctx)
		if err != nil {
			return nil, err
		}
		services = append(services, loaded...)
	}

	return services, nil
}

func (s Source) loadServices(ctx context.Context) ([]compass.Service, error) {
	tailscaleServices, err := s.client.Services().List(ctx)
	if err != nil {
		if tailscaleapi.IsNotFound(err) {
			slog.Warn(
				"tailscale services endpoint returned 404 - verify tailnet ID and OAuth client scopes",
				"tailnet",
				s.tailnetDisplay(),
				"tailnet_id",
				s.tailnetID,
			)
			return nil, nil
		}
		return nil, fmt.Errorf("source %s: list services: %w", s.name, err)
	}

	services := make([]compass.Service, 0, len(tailscaleServices))
	for _, tailscaleService := range tailscaleServices {
		serviceURL := s.serviceURL(tailscaleService)
		if serviceURL == "" {
			continue
		}
		services = append(services, compass.Service{
			ID:          "tailscale/" + tailscaleService.Name,
			Name:        serviceName(tailscaleService),
			URL:         serviceURL,
			Source:      s.name,
			Tags:        s.tagsForService(tailscaleService),
			Description: tailscaleService.Comment,
			Metadata: map[string]any{
				"name":      tailscaleService.Name,
				"tailnet":   s.tailnetDisplay(),
				"addresses": tailscaleService.Addrs,
				"ports":     tailscaleService.Ports,
				"tags":      tailscaleService.Tags,
			},
		})
	}

	return services, nil
}

func (s Source) loadDevices(ctx context.Context) ([]compass.Service, error) {
	tailscaleDevices, err := s.client.Devices().List(ctx)
	if err != nil {
		if tailscaleapi.IsNotFound(err) {
			slog.Warn(
				"tailscale devices endpoint returned 404 - verify tailnet ID and OAuth client scopes",
				"tailnet",
				s.tailnetDisplay(),
				"tailnet_id",
				s.tailnetID,
			)
			return nil, nil
		}
		return nil, fmt.Errorf("source %s: list devices: %w", s.name, err)
	}

	services := make([]compass.Service, 0, len(tailscaleDevices))
	now := time.Now()
	for _, device := range tailscaleDevices {
		if s.excludeUnauthorized && !device.Authorized {
			continue
		}
		if s.excludeExternal && device.IsExternal {
			continue
		}
		if s.excludeStaleAfter > 0 && deviceIsStale(device, s.excludeStaleAfter, now) {
			continue
		}
		deviceURL := s.deviceURL(device)
		if deviceURL == "" {
			continue
		}
		metadata := map[string]any{
			"name":           device.Name,
			"hostname":       device.Hostname,
			"tailnet":        s.tailnetDisplay(),
			"node_id":        device.NodeID,
			"id":             device.ID,
			"addresses":      device.Addresses,
			"os":             device.OS,
			"client_version": device.ClientVersion,
			"user":           device.User,
			"tags":           device.Tags,
			"authorized":     device.Authorized,
		}
		if device.LastSeen != nil && !device.LastSeen.IsZero() {
			metadata["last_seen"] = device.LastSeen.Time
		}
		if len(device.AdvertisedRoutes) > 0 {
			metadata["advertised_routes"] = device.AdvertisedRoutes
		}
		if len(device.EnabledRoutes) > 0 {
			metadata["enabled_routes"] = device.EnabledRoutes
		}
		if device.SSHEnabled {
			metadata["ssh_enabled"] = true
		}
		if device.UpdateAvailable {
			metadata["update_available"] = true
		}
		if device.IsExternal {
			metadata["is_external"] = true
		}
		if cc := device.ClientConnectivity; cc != nil {
			if len(cc.Endpoints) > 0 {
				metadata["endpoints"] = cc.Endpoints
			}
			if cc.DERP != "" {
				metadata["derp"] = cc.DERP
			}
		}
		if d := device.Distro; d != nil {
			distro := map[string]any{}
			if d.Name != "" {
				distro["name"] = d.Name
			}
			if d.Version != "" {
				distro["version"] = d.Version
			}
			if d.CodeName != "" {
				distro["code_name"] = d.CodeName
			}
			if len(distro) > 0 {
				metadata["distro"] = distro
			}
		}
		if pi := device.PostureIdentity; pi != nil {
			if len(pi.SerialNumbers) > 0 {
				metadata["serial_numbers"] = pi.SerialNumbers
			}
			if len(pi.HardwareAddresses) > 0 {
				metadata["hardware_addresses"] = pi.HardwareAddresses
			}
		}
		services = append(services, compass.Service{
			ID:       "tailscale/device/" + deviceID(device),
			Name:     deviceName(device),
			URL:      deviceURL,
			Source:   s.name,
			Tags:     s.tagsForDevice(device),
			Metadata: metadata,
		})
	}

	return services, nil
}

func (s Source) serviceURL(service tailscaleapi.Service) string {
	if service.Name != "" {
		return meta.WithScheme(strings.TrimPrefix(service.Name, "svc:"), s.urlScheme)
	}
	if len(service.Addrs) > 0 {
		return meta.WithScheme(service.Addrs[0], s.urlScheme)
	}

	return ""
}

func (s Source) deviceURL(device tailscaleapi.Device) string {
	scheme := s.urlScheme
	port := ""
	for _, raw := range device.Tags {
		bare := strings.TrimPrefix(raw, "tag:")
		switch {
		case strings.HasPrefix(bare, tagPrefixCompassScheme):
			if v := strings.TrimPrefix(bare, tagPrefixCompassScheme); v != "" {
				scheme = v
			}
		case strings.HasPrefix(bare, tagPrefixCompassPort):
			if v := strings.TrimPrefix(bare, tagPrefixCompassPort); isValidPort(v) {
				port = v
			}
		}
	}

	host := device.Name
	if host == "" && len(device.Addresses) > 0 {
		host = device.Addresses[0]
	}
	if host == "" {
		return ""
	}
	if port != "" {
		host = host + ":" + port
	}
	return meta.WithScheme(host, scheme)
}

func isValidPort(v string) bool {
	n, err := strconv.Atoi(v)
	return err == nil && n > 0 && n <= 65535
}

func (s Source) tagsForService(service tailscaleapi.Service) []string {
	var serviceTags []string
	for _, tag := range service.Tags {
		serviceTags = append(serviceTags, strings.TrimPrefix(tag, "tag:"))
	}

	return meta.MergeTags(meta.MergeTags(s.tags, s.serviceTags), serviceTags)
}

func (s Source) tagsForDevice(device tailscaleapi.Device) []string {
	var deviceTags []string
	for _, raw := range device.Tags {
		bare := strings.TrimPrefix(raw, "tag:")
		// Filter out tag-convention URL overrides; they are control plane,
		// not user-visible tags. Keep everything else.
		if strings.HasPrefix(bare, tagPrefixCompassPort) ||
			strings.HasPrefix(bare, tagPrefixCompassScheme) {
			continue
		}
		deviceTags = append(deviceTags, bare)
	}

	if !device.Authorized {
		deviceTags = append(deviceTags, "unauthorized")
	}
	if device.UpdateAvailable {
		deviceTags = append(deviceTags, "update-available")
	}
	if !device.ConnectedToControl {
		deviceTags = append(deviceTags, "offline")
	}

	return meta.MergeTags(meta.MergeTags(s.tags, s.deviceTags), deviceTags)
}

func deviceID(device tailscaleapi.Device) string {
	if device.NodeID != "" {
		return device.NodeID
	}
	return device.ID
}

func deviceName(device tailscaleapi.Device) string {
	// Prefer the Tailscale machine name (first label of the magicDNS FQDN)
	// over the raw OS hostname — the machine name is what users edit in the
	// admin console, while Hostname can be something like "MacBook-Pro".
	if name, _, ok := strings.Cut(device.Name, "."); ok && name != "" {
		return name
	}
	if device.Name != "" {
		return device.Name
	}
	if device.Hostname != "" {
		return device.Hostname
	}
	if len(device.Addresses) > 0 {
		return device.Addresses[0]
	}
	return device.NodeID
}

func serviceName(service tailscaleapi.Service) string {
	if service.Name == "" && len(service.Addrs) > 0 {
		return service.Addrs[0]
	}

	return strings.TrimPrefix(service.Name, "svc:")
}

func oauthScopes(scopes []string, includeServices, includeDevices bool) []string {
	resolved := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		if scope = strings.TrimSpace(scope); scope != "" {
			resolved = append(resolved, scope)
		}
	}
	if includeServices && !slices.Contains(resolved, scopeServicesRead) {
		resolved = append(resolved, scopeServicesRead)
	}
	if includeDevices && !slices.Contains(resolved, scopeDevicesRead) {
		resolved = append(resolved, scopeDevicesRead)
	}
	return resolved
}

// parseScopes splits comma- or whitespace-separated scope strings, trimming
// blanks. Used for the TAILSCALE_OAUTH_SCOPES env-var fallback.
func parseScopes(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	scopes := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			scopes = append(scopes, p)
		}
	}
	return scopes
}

// parseStaleDuration accepts standard Go duration strings ("720h", "30m")
// plus the convenience suffixes "d" (days) and "w" (weeks). Empty input
// returns 0 — meaning "filter disabled".
func parseStaleDuration(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	if mult, ok := dayWeekMultiplier(raw); ok {
		n, err := strconv.Atoi(raw[:len(raw)-1])
		if err != nil {
			return 0, fmt.Errorf("invalid exclude_stale_after %q: %w", raw, err)
		}
		if n < 0 {
			return 0, fmt.Errorf("invalid exclude_stale_after %q: must be non-negative", raw)
		}
		return time.Duration(n) * mult, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid exclude_stale_after %q: %w", raw, err)
	}
	if d < 0 {
		return 0, fmt.Errorf("invalid exclude_stale_after %q: must be non-negative", raw)
	}
	return d, nil
}

func dayWeekMultiplier(raw string) (time.Duration, bool) {
	if len(raw) < 2 {
		return 0, false
	}
	switch raw[len(raw)-1] {
	case 'd':
		return 24 * time.Hour, true
	case 'w':
		return 7 * 24 * time.Hour, true
	}
	return 0, false
}

// deviceIsStale returns true when the device has a known LastSeen older
// than the configured threshold. Devices currently connected to control
// (LastSeen == nil per the v2 API) are considered fresh.
func deviceIsStale(device tailscaleapi.Device, threshold time.Duration, now time.Time) bool {
	if device.LastSeen == nil || device.LastSeen.IsZero() {
		return false
	}
	return now.Sub(device.LastSeen.Time) > threshold
}
