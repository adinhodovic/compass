package netbird

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/adinhodovic/compass/internal/compass"
	"github.com/adinhodovic/compass/internal/config"
)

const peersJSON = `[
  {
    "id": "chacbco6lnnbn6cg5s90",
    "name": "alice-laptop",
    "hostname": "alice-laptop.local",
    "ip": "100.92.0.5",
    "ipv6": "fd00:4e42::5",
    "connection_ip": "203.0.113.7",
    "dns_label": "alice-laptop.netbird.cloud",
    "extra_dns_labels": [],
    "connected": true,
    "last_seen": "2026-10-20T10:00:00Z",
    "created_at": "2026-01-02T03:04:05Z",
    "os": "Linux",
    "version": "0.81.3",
    "ssh_enabled": true,
    "serial_number": "SN-123",
    "city_name": "Berlin",
    "country_code": "DE",
    "groups": [{"id": "g1", "name": "All"}, {"id": "g2", "name": "servers"}]
  },
  {
    "id": "chacdk86lnnboviihd70",
    "name": "",
    "ip": "",
    "dns_label": "stale-box.netbird.cloud",
    "connected": false,
    "groups": [{"id": "g1", "name": "All"}]
  },
  {
    "id": "unreachable",
    "name": "no-address",
    "connected": true
  }
]`

func newTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func servePeers(t *testing.T, gotAuth *string) *httptest.Server {
	return newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/peers" {
			http.NotFound(w, r)
			return
		}
		*gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(peersJSON))
	})
}

func TestNetbirdLoadMapsPeers(t *testing.T) {
	var gotAuth string
	server := servePeers(t, &gotAuth)
	source := newWithClient(
		"mesh",
		config.NetbirdConfig{
			APIURL:     server.URL + "/",
			APIToken:   "nbp_secret",
			Tags:       []string{"homelab"},
			DeviceTags: []string{"node"},
		},
		server.Client(),
	)

	services, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("load peers: %v", err)
	}
	if gotAuth != "Token nbp_secret" {
		t.Fatalf("unexpected Authorization header: %q", gotAuth)
	}
	if len(services) != 2 {
		t.Fatalf("expected peers without an address to be skipped, got %d services", len(services))
	}

	got := services[0]
	if got.ID != "netbird/peer/chacbco6lnnbn6cg5s90" {
		t.Fatalf("unexpected ID: %q", got.ID)
	}
	if got.Name != "alice-laptop" {
		t.Fatalf("unexpected name: %q", got.Name)
	}
	if got.URL != "http://100.92.0.5" {
		t.Fatalf("unexpected URL: %q", got.URL)
	}
	if want := []string{"homelab", "node", "servers"}; !slices.Equal(got.Tags, want) {
		t.Fatalf("expected tags %#v (All group skipped), got %#v", want, got.Tags)
	}
	if got.Metadata["connected"] != true || got.Metadata["os"] != "Linux" ||
		got.Metadata["dns_label"] != "alice-laptop.netbird.cloud" {
		t.Fatalf("unexpected metadata: %#v", got.Metadata)
	}
	if groups, _ := got.Metadata["groups"].([]string); !slices.Equal(
		groups,
		[]string{"All", "servers"},
	) {
		t.Fatalf("unexpected groups metadata: %#v", got.Metadata["groups"])
	}
	for _, key := range []string{"connection_ip", "serial_number", "city_name", "country_code", "extra_dns_labels"} {
		if _, ok := got.Metadata[key]; ok {
			t.Fatalf("metadata should not contain %q: %#v", key, got.Metadata)
		}
	}
}

func TestNetbirdLoadFallsBackToDNSLabelAndTagsOffline(t *testing.T) {
	var gotAuth string
	server := servePeers(t, &gotAuth)
	source := newWithClient(
		"mesh",
		config.NetbirdConfig{APIURL: server.URL, URLScheme: "https"},
		server.Client(),
	)

	services, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("load peers: %v", err)
	}
	got := services[1]
	if got.Name != "stale-box" {
		t.Fatalf("expected name from DNS label, got %q", got.Name)
	}
	if got.URL != "https://stale-box.netbird.cloud" {
		t.Fatalf("expected URL from DNS label, got %q", got.URL)
	}
	if want := []string{compass.SourceTypeNetbird, "offline"}; !slices.Equal(got.Tags, want) {
		t.Fatalf("expected tags %#v, got %#v", want, got.Tags)
	}
}

func TestNetbirdLoadWrapsErrorsWithoutLeakingToken(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "unauthorized",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, `{"message":"token invalid"}`, http.StatusUnauthorized)
			},
		},
		{
			name: "malformed body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"not":"an array"}`))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newTestServer(t, tt.handler)
			source := newWithClient(
				"mesh",
				config.NetbirdConfig{APIURL: server.URL, APIToken: "nbp_secret"},
				server.Client(),
			)

			_, err := source.Load(context.Background())
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.HasPrefix(err.Error(), "source mesh: list peers: ") {
				t.Fatalf("unexpected error shape: %v", err)
			}
			if strings.Contains(err.Error(), "nbp_secret") {
				t.Fatalf("error leaks the API token: %v", err)
			}
		})
	}
}

func TestNetbirdLoadHonorsContextCancellation(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	source := newWithClient("mesh", config.NetbirdConfig{APIURL: server.URL}, server.Client())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := source.Load(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestNetbirdIncludeDevicesFalseSkipsCall(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("API should not be called when devices are disabled")
	})
	source := newWithClient(
		"mesh",
		config.NetbirdConfig{APIURL: server.URL, IncludeDevices: new(false)},
		server.Client(),
	)

	services, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("expected no error when devices disabled, got %v", err)
	}
	if len(services) != 0 {
		t.Fatalf("expected no services, got %#v", services)
	}
}

func TestNetbirdNew(t *testing.T) {
	tests := []struct {
		name       string
		envURL     string
		envToken   string
		cfg        config.NetbirdConfig
		wantURL    string
		wantErrMsg string
	}{
		{
			name:     "defaults to NetBird Cloud",
			envToken: "env-token",
			wantURL:  defaultAPIURL,
		},
		{
			name:     "fills empty fields from env",
			envURL:   "https://env.example.com",
			envToken: "env-token",
			wantURL:  "https://env.example.com",
		},
		{
			name:     "YAML wins over env",
			envURL:   "https://env.example.com",
			envToken: "env-token",
			cfg: config.NetbirdConfig{
				APIURL:   "https://yaml.example.com",
				APIToken: "yaml-token",
			},
			wantURL: "https://yaml.example.com",
		},
		{
			name:       "requires token",
			wantErrMsg: "netbird API token is required",
		},
		{
			name:       "rejects non-http URL",
			cfg:        config.NetbirdConfig{APIURL: "netbird.example.com", APIToken: "t"},
			wantErrMsg: "must be an http(s) URL",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(envNetbirdAPIURL, tt.envURL)
			t.Setenv(envNetbirdAPIToken, tt.envToken)

			src, err := New(
				config.SourceConfig{Type: compass.SourceTypeNetbird, Netbird: tt.cfg},
				nil,
			)
			if tt.wantErrMsg != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrMsg) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErrMsg, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("new netbird source: %v", err)
			}
			if src.apiURL != tt.wantURL {
				t.Fatalf("expected api URL %q, got %q", tt.wantURL, src.apiURL)
			}
			if src.Name() != compass.SourceTypeNetbird {
				t.Fatalf("expected default name, got %q", src.Name())
			}
		})
	}
}
