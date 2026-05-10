package tailscale

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/adinhodovic/compass/internal/compass"
	"github.com/adinhodovic/compass/internal/config"
	tailscaleapi "tailscale.com/client/tailscale/v2"
)

type MockServicesClient struct {
	services []tailscaleapi.Service
	err      error
	called   bool
}

func (m *MockServicesClient) List(ctx context.Context) ([]tailscaleapi.Service, error) {
	m.called = true
	if m.err != nil {
		return nil, m.err
	}

	return m.services, nil
}

type MockDevicesClient struct {
	devices []tailscaleapi.Device
	err     error
	called  bool
}

func (m *MockDevicesClient) List(ctx context.Context) ([]tailscaleapi.Device, error) {
	m.called = true
	if m.err != nil {
		return nil, m.err
	}

	return m.devices, nil
}

func TestTailscaleDefaultsIncludeDevicesOnly(t *testing.T) {
	servicesClient := &MockServicesClient{}
	devicesClient := &MockDevicesClient{}
	source := newWithClient(
		"tailnet",
		config.TailscaleConfig{},
		&MockTailscaleClient{servicesClient: servicesClient, devicesClient: devicesClient},
	)

	if _, err := source.Load(context.Background()); err != nil {
		t.Fatalf("load: %v", err)
	}
	if servicesClient.called {
		t.Fatal("expected default load to skip services endpoint (alpha)")
	}
	if !devicesClient.called {
		t.Fatal("expected default load to call devices endpoint")
	}
}

type MockTailscaleClient struct {
	servicesClient *MockServicesClient
	devicesClient  *MockDevicesClient
}

func (m *MockTailscaleClient) Services() servicesAPI {
	return m.servicesClient
}

func (m *MockTailscaleClient) Devices() devicesAPI {
	if m.devicesClient == nil {
		return &MockDevicesClient{}
	}
	return m.devicesClient
}

func TestTailscaleLoadMapsServices(t *testing.T) {
	source := newWithClient(
		"tailnet",
		config.TailscaleConfig{
			TailnetID:       "ts-id",
			TailnetName:     "example.ts.net",
			Tags:            []string{"remote"},
			URLScheme:       "https",
			IncludeServices: new(true),
		},
		&MockTailscaleClient{
			servicesClient: &MockServicesClient{services: []tailscaleapi.Service{{
				Name:    "svc:grafana",
				Addrs:   []string{"100.64.0.10"},
				Comment: "Dashboards",
				Ports:   []string{"443"},
				Tags:    []string{"tag:monitoring"},
			}}},
		},
	)

	services, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("load tailscale services: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("expected one service, got %d", len(services))
	}
	service := services[0]
	if service.Name != "grafana" || service.URL != "https://grafana" {
		t.Fatalf("unexpected service: %#v", service)
	}
	if service.Tags[0] != "remote" || service.Tags[1] != "monitoring" {
		t.Fatalf("unexpected tags: %#v", service.Tags)
	}
	if service.Metadata["tailnet"] != "example.ts.net" {
		t.Fatalf("unexpected metadata: %#v", service.Metadata)
	}
}

func TestTailscaleLoadReturnsClientError(t *testing.T) {
	source := newWithClient(
		"tailnet",
		config.TailscaleConfig{IncludeServices: new(true)},
		&MockTailscaleClient{servicesClient: &MockServicesClient{err: errors.New("boom")}},
	)

	_, err := source.Load(context.Background())
	if err == nil {
		t.Fatal("expected client error")
	}
}

func TestTailscaleLoadTreatsNotFoundAsNoServices(t *testing.T) {
	source := newWithClient(
		"tailnet",
		config.TailscaleConfig{IncludeServices: new(true)},
		&MockTailscaleClient{
			servicesClient: &MockServicesClient{
				err: tailscaleapi.APIError{Status: http.StatusNotFound, Message: "not found"},
			},
		},
	)

	services, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("expected no error for missing services endpoint, got %v", err)
	}
	if len(services) != 0 {
		t.Fatalf("expected no services, got %#v", services)
	}
}

func TestNewFillsEmptyFieldsFromEnv(t *testing.T) {
	t.Setenv(envTailnetID, "ts-id")
	t.Setenv(envOAuthClientID, "client-id")
	t.Setenv(envOAuthClientSecret, "client-secret")
	t.Setenv(envOAuthScopes, "services:write")

	source, err := New(config.SourceConfig{Type: compass.SourceTypeTailscale})
	if err != nil {
		t.Fatalf("new tailscale source: %v", err)
	}
	if source.tailnetID != "ts-id" {
		t.Fatalf("expected env tailnet ID, got %q", source.tailnetID)
	}
}

func TestNewYAMLWinsOverEnv(t *testing.T) {
	t.Setenv(envTailnetID, "env-id")
	t.Setenv(envOAuthClientID, "env-client")
	t.Setenv(envOAuthClientSecret, "env-secret")

	source, err := New(config.SourceConfig{
		Type: compass.SourceTypeTailscale,
		Tailscale: config.TailscaleConfig{
			TailnetID:         "yaml-id",
			OAuthClientID:     "yaml-client",
			OAuthClientSecret: "yaml-secret",
		},
	})
	if err != nil {
		t.Fatalf("new tailscale source: %v", err)
	}
	if source.tailnetID != "yaml-id" {
		t.Fatalf("expected YAML tailnet ID to win, got %q", source.tailnetID)
	}
}

func TestNewTailnetNameComesFromYAML(t *testing.T) {
	t.Setenv(envTailnetID, "ts-id")
	t.Setenv(envOAuthClientID, "client-id")
	t.Setenv(envOAuthClientSecret, "client-secret")

	source, err := New(config.SourceConfig{
		Type: compass.SourceTypeTailscale,
		Tailscale: config.TailscaleConfig{
			TailnetName: "example.com",
		},
	})
	if err != nil {
		t.Fatalf("new tailscale source: %v", err)
	}
	if source.tailnetName != "example.com" {
		t.Fatalf("expected tailnet name from YAML, got %q", source.tailnetName)
	}
}

func TestOAuthScopesDefault(t *testing.T) {
	if got := oauthScopes(nil, true, false); len(got) != 1 || got[0] != "services:read" {
		t.Fatalf("unexpected default scopes: %#v", got)
	}
}

func TestOAuthScopesAutoBumpsDevicesReadWhenIncludeDevices(t *testing.T) {
	got := oauthScopes(nil, true, true)
	if len(got) != 2 || got[0] != "services:read" || got[1] != "devices:core:read" {
		t.Fatalf("expected services:read + devices:core:read, got %#v", got)
	}
}

func TestOAuthScopesDoesNotDuplicateDevicesRead(t *testing.T) {
	got := oauthScopes([]string{"devices:core:read"}, false, true)
	if len(got) != 1 || got[0] != "devices:core:read" {
		t.Fatalf("expected single devices:core:read, got %#v", got)
	}
}

func TestNewRequiresCredentials(t *testing.T) {
	t.Setenv(envTailnetID, "")
	t.Setenv(envOAuthClientID, "")
	t.Setenv(envOAuthClientSecret, "")
	if _, err := New(config.SourceConfig{Type: compass.SourceTypeTailscale}); err == nil {
		t.Fatal("expected missing tailscale config error")
	}
}

func TestTailscaleLoadMapsDevices(t *testing.T) {
	source := newWithClient(
		"tailnet",
		config.TailscaleConfig{
			TailnetName:     "example.ts.net",
			Tags:            []string{"remote"},
			ServiceTags:     []string{"service"},
			DeviceTags:      []string{"node"},
			IncludeServices: new(false),
			IncludeDevices:  new(true),
			URLScheme:       "https",
		},
		&MockTailscaleClient{
			servicesClient: &MockServicesClient{},
			devicesClient: &MockDevicesClient{devices: []tailscaleapi.Device{{
				ID:                 "12345",
				NodeID:             "nodekey-12345",
				Name:               "alice-laptop.example.ts.net",
				Hostname:           "MacBook-Pro",
				Addresses:          []string{"100.64.0.20"},
				OS:                 "macOS",
				ClientVersion:      "1.74.0",
				User:               "alice@example.com",
				Tags:               []string{"tag:server"},
				Authorized:         true,
				ConnectedToControl: true,
			}}},
		},
	)

	services, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("load tailscale devices: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("expected one device service, got %d", len(services))
	}
	got := services[0]
	if got.ID != "tailscale/device/nodekey-12345" {
		t.Fatalf("unexpected device ID: %q", got.ID)
	}
	if got.Name != "alice-laptop" {
		t.Fatalf("expected machine name (first label of magicDNS), got %q", got.Name)
	}
	if got.URL != "https://alice-laptop.example.ts.net" {
		t.Fatalf("unexpected device URL: %q", got.URL)
	}
	if len(got.Tags) != 3 || got.Tags[0] != "remote" || got.Tags[1] != "node" ||
		got.Tags[2] != "server" {
		t.Fatalf("unexpected device tags: %#v", got.Tags)
	}
	if got.Metadata["node_id"] != "nodekey-12345" {
		t.Fatalf("unexpected metadata: %#v", got.Metadata)
	}
	if got.Metadata["os"] != "macOS" {
		t.Fatalf("unexpected os metadata: %#v", got.Metadata)
	}
	if got.Metadata["hostname"] != "MacBook-Pro" {
		t.Fatalf("expected raw OS hostname preserved in metadata, got %#v", got.Metadata)
	}
}

func TestTailscaleLoadDeviceMetadataIncludesConnectivity(t *testing.T) {
	source := newWithClient(
		"tailnet",
		config.TailscaleConfig{
			IncludeServices: new(false),
			IncludeDevices:  new(true),
		},
		&MockTailscaleClient{
			devicesClient: &MockDevicesClient{devices: []tailscaleapi.Device{{
				NodeID:           "nodekey-1",
				Name:             "router.example.ts.net",
				Hostname:         "router",
				Addresses:        []string{"100.64.0.30"},
				AdvertisedRoutes: []string{"10.0.0.0/24"},
				EnabledRoutes:    []string{"10.0.0.0/24"},
				SSHEnabled:       true,
				ClientConnectivity: &tailscaleapi.ClientConnectivity{
					Endpoints: []string{"1.2.3.4:41641", "[2001:db8::1]:41641"},
					DERP:      "Frankfurt",
				},
			}}},
		},
	)

	services, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("load tailscale devices: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("expected one device service, got %d", len(services))
	}
	md := services[0].Metadata
	endpoints, ok := md["endpoints"].([]string)
	if !ok || len(endpoints) != 2 || endpoints[0] != "1.2.3.4:41641" {
		t.Fatalf("expected connectivity endpoints in metadata, got %#v", md["endpoints"])
	}
	if md["derp"] != "Frankfurt" {
		t.Fatalf("expected DERP region metadata, got %#v", md["derp"])
	}
	if md["ssh_enabled"] != true {
		t.Fatalf("expected ssh_enabled metadata, got %#v", md["ssh_enabled"])
	}
	if routes, _ := md["advertised_routes"].([]string); len(routes) != 1 ||
		routes[0] != "10.0.0.0/24" {
		t.Fatalf("expected advertised_routes metadata, got %#v", md["advertised_routes"])
	}
}

func TestTailscaleLoadMergesServicesAndDevices(t *testing.T) {
	source := newWithClient(
		"tailnet",
		config.TailscaleConfig{
			IncludeServices: new(true),
			IncludeDevices:  new(true),
		},
		&MockTailscaleClient{
			servicesClient: &MockServicesClient{services: []tailscaleapi.Service{{
				Name:  "svc:grafana",
				Addrs: []string{"100.64.0.10"},
			}}},
			devicesClient: &MockDevicesClient{devices: []tailscaleapi.Device{{
				NodeID:    "nodekey-1",
				Name:      "myhost.tailnet.ts.net",
				Hostname:  "myhost",
				Addresses: []string{"100.64.0.20"},
			}}},
		},
	)

	services, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("load tailscale services+devices: %v", err)
	}
	if len(services) != 2 {
		t.Fatalf("expected 2 services (1 service + 1 device), got %d: %#v", len(services), services)
	}
	if services[0].ID != "tailscale/svc:grafana" {
		t.Fatalf("expected service first, got %q", services[0].ID)
	}
	if services[1].ID != "tailscale/device/nodekey-1" {
		t.Fatalf("expected device second, got %q", services[1].ID)
	}
}

func TestTailscaleLoadDevicesTreatsNotFoundAsNoServices(t *testing.T) {
	source := newWithClient(
		"tailnet",
		config.TailscaleConfig{
			IncludeServices: new(false),
			IncludeDevices:  new(true),
		},
		&MockTailscaleClient{
			devicesClient: &MockDevicesClient{
				err: tailscaleapi.APIError{Status: http.StatusNotFound, Message: "not found"},
			},
		},
	)

	services, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("expected no error for missing devices endpoint, got %v", err)
	}
	if len(services) != 0 {
		t.Fatalf("expected no services, got %#v", services)
	}
}

func TestTailscaleLoadDevicesReturnsClientError(t *testing.T) {
	source := newWithClient(
		"tailnet",
		config.TailscaleConfig{
			IncludeServices: new(false),
			IncludeDevices:  new(true),
		},
		&MockTailscaleClient{
			devicesClient: &MockDevicesClient{err: errors.New("boom")},
		},
	)

	if _, err := source.Load(context.Background()); err == nil {
		t.Fatal("expected device client error")
	}
}

func TestTailscaleIncludeServicesFalseSkipsServicesCall(t *testing.T) {
	servicesMock := &MockServicesClient{err: errors.New("should not be called")}
	source := newWithClient(
		"tailnet",
		config.TailscaleConfig{
			IncludeServices: new(false),
			IncludeDevices:  new(true),
		},
		&MockTailscaleClient{
			servicesClient: servicesMock,
			devicesClient:  &MockDevicesClient{},
		},
	)

	services, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("expected no error when services disabled, got %v", err)
	}
	if len(services) != 0 {
		t.Fatalf("expected no services, got %#v", services)
	}
}

func TestTailscaleStatusDerivedTags(t *testing.T) {
	source := newWithClient(
		"tailnet",
		config.TailscaleConfig{
			IncludeServices: new(false),
			IncludeDevices:  new(true),
		},
		&MockTailscaleClient{
			devicesClient: &MockDevicesClient{devices: []tailscaleapi.Device{{
				NodeID:             "n",
				Name:               "stale.example.ts.net",
				Addresses:          []string{"100.64.0.4"},
				Authorized:         false,
				ConnectedToControl: false,
				UpdateAvailable:    true,
			}}},
		},
	)

	services, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := map[string]bool{"unauthorized": false, "update-available": false, "offline": false}
	for _, tag := range services[0].Tags {
		if _, ok := want[tag]; ok {
			want[tag] = true
		}
	}
	for tag, found := range want {
		if !found {
			t.Fatalf("expected %q tag, got %#v", tag, services[0].Tags)
		}
	}
}

func TestTailscaleDeviceFilters(t *testing.T) {
	now := time.Now()
	stale := tailscaleapi.Time{Time: now.Add(-30 * 24 * time.Hour)}
	source := newWithClient(
		"tailnet",
		config.TailscaleConfig{
			IncludeServices:     new(false),
			IncludeDevices:      new(true),
			ExcludeUnauthorized: true,
			ExcludeExternal:     true,
			ExcludeStaleAfter:   "7d",
		},
		&MockTailscaleClient{
			devicesClient: &MockDevicesClient{devices: []tailscaleapi.Device{
				{
					NodeID: "ok", Name: "ok.example.ts.net",
					Addresses:  []string{"100.64.0.1"},
					Authorized: true, ConnectedToControl: true,
				},
				{
					NodeID: "unauth", Name: "u.example.ts.net",
					Addresses:  []string{"100.64.0.2"},
					Authorized: false, ConnectedToControl: true,
				},
				{
					NodeID: "shared", Name: "s.example.ts.net",
					Addresses:  []string{"100.64.0.3"},
					Authorized: true, ConnectedToControl: true, IsExternal: true,
				},
				{
					NodeID: "stale", Name: "z.example.ts.net",
					Addresses:  []string{"100.64.0.4"},
					Authorized: true, LastSeen: &stale,
				},
			}},
		},
	)

	services, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("expected exactly 1 surviving device, got %d: %#v", len(services), services)
	}
	if services[0].ID != "tailscale/device/ok" {
		t.Fatalf("expected the authorized fresh non-shared device, got %q", services[0].ID)
	}
}

func TestTailscaleParseStaleDuration(t *testing.T) {
	cases := map[string]time.Duration{
		"":     0,
		"30m":  30 * time.Minute,
		"720h": 720 * time.Hour,
		"30d":  30 * 24 * time.Hour,
		"2w":   2 * 7 * 24 * time.Hour,
	}
	for input, expected := range cases {
		got, err := parseStaleDuration(input)
		if err != nil {
			t.Fatalf("parseStaleDuration(%q): %v", input, err)
		}
		if got != expected {
			t.Fatalf("parseStaleDuration(%q) = %v, want %v", input, got, expected)
		}
	}
	if _, err := parseStaleDuration("nope"); err == nil {
		t.Fatal("expected error on bogus duration")
	}
	if _, err := parseStaleDuration("-3d"); err == nil {
		t.Fatal("expected error on negative duration")
	}
}

func TestTailscaleDeviceURLTagOverrides(t *testing.T) {
	source := newWithClient(
		"tailnet",
		config.TailscaleConfig{
			IncludeServices: new(false),
			IncludeDevices:  new(true),
			URLScheme:       "https",
		},
		&MockTailscaleClient{
			devicesClient: &MockDevicesClient{devices: []tailscaleapi.Device{{
				NodeID:             "router",
				Name:               "router.example.ts.net",
				Addresses:          []string{"100.64.0.50"},
				Authorized:         true,
				ConnectedToControl: true,
				Tags: []string{
					"tag:compass-port-8443",
					"tag:compass-scheme-http",
					"tag:server",
				},
			}}},
		},
	)

	services, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got := services[0]
	if got.URL != "http://router.example.ts.net:8443" {
		t.Fatalf("expected tag-driven URL override, got %q", got.URL)
	}
	for _, tag := range got.Tags {
		if strings.HasPrefix(tag, "compass-port-") || strings.HasPrefix(tag, "compass-scheme-") {
			t.Fatalf("expected compass-* control tags to be filtered, got %#v", got.Tags)
		}
	}
}

func TestTailscaleDevicePostureMetadata(t *testing.T) {
	source := newWithClient(
		"tailnet",
		config.TailscaleConfig{
			IncludeServices: new(false),
			IncludeDevices:  new(true),
		},
		&MockTailscaleClient{
			devicesClient: &MockDevicesClient{devices: []tailscaleapi.Device{{
				NodeID:             "n",
				Name:               "host.example.ts.net",
				Addresses:          []string{"100.64.0.1"},
				Authorized:         true,
				ConnectedToControl: true,
				MachineKey:         "mkey:abcdef0123456789",
				Distro: &tailscaleapi.Distro{
					Name: "ubuntu", Version: "24.04", CodeName: "noble",
				},
				PostureIdentity: &tailscaleapi.DevicePostureIdentity{
					SerialNumbers:     []string{"SN1234"},
					HardwareAddresses: []string{"aa:bb:cc:dd:ee:ff"},
				},
			}}},
		},
	)

	services, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	md := services[0].Metadata
	distro, ok := md["distro"].(map[string]any)
	if !ok || distro["name"] != "ubuntu" || distro["version"] != "24.04" {
		t.Fatalf("expected distro metadata, got %#v", md["distro"])
	}
	if serials, _ := md["serial_numbers"].([]string); len(serials) != 1 || serials[0] != "SN1234" {
		t.Fatalf("expected serial number metadata, got %#v", md["serial_numbers"])
	}
	if _, ok := md["machine_key"]; ok {
		t.Fatalf("machine_key metadata should be redacted, got %#v", md["machine_key"])
	}
}
