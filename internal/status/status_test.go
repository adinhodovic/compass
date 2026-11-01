package status

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/adinhodovic/compass/internal/config"
	"go.yaml.in/yaml/v3"
)

func newProvider(t *testing.T, cfg config.StatusConfig, body string, code int) *Provider {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); cfg.Headers["Authorization"] != "" &&
			got != cfg.Headers["Authorization"] {
			t.Errorf("Authorization header = %q", got)
		}
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	cfg.URL = srv.URL
	if cfg.Type == config.StatusTypeJSON || cfg.Type == "" {
		cfg.Endpoint = srv.URL
	}
	return New(defaulted(t, cfg), srv.Client())
}

// defaulted round-trips cfg through config.Load so tests exercise the
// same preset expansion and validation as the binary.
func defaulted(t *testing.T, cfg config.StatusConfig) config.StatusConfig {
	t.Helper()
	entry, err := yaml.Marshal([]config.StatusConfig{cfg})
	if err != nil {
		t.Fatal(err)
	}
	doc := "services:\n  sources:\n    - type: static\n      name: manual\n" +
		"status:\n" + indent(string(entry))
	path := filepath.Join(t.TempDir(), "compass.yaml")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return loaded.Status[0]
}

func indent(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := range lines {
		lines[i] = "  " + lines[i]
	}
	return strings.Join(lines, "\n") + "\n"
}

func TestFetchMapsChecks(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.StatusConfig
		body string
		want []Check
	}{
		{
			name: "prometheus preset",
			cfg:  config.StatusConfig{Type: config.StatusTypePrometheus},
			body: `{"status":"success","data":{"resultType":"vector","result":[
				{"metric":{"instance":"https://grafana.home"},"value":[1730000000,"1"]},
				{"metric":{"instance":"https://plex.home"},"value":[1730000000,"0"]},
				{"metric":{"job":"no-instance"},"value":[1730000000,"1"]}
			]}}`,
			want: []Check{
				{Match: "https://grafana.home", Up: true},
				{Match: "https://plex.home", Up: false},
			},
		},
		{
			name: "gatus preset reads newest result",
			cfg:  config.StatusConfig{Type: config.StatusTypeGatus},
			body: `[
				{"name":"grafana","results":[
					{"hostname":"grafana.home","success":false},
					{"hostname":"grafana.home","success":true}
				]},
				{"name":"empty","results":[]}
			]`,
			want: []Check{{Match: "grafana.home", Up: true}},
		},
		{
			name: "json booleans without up_value",
			cfg: config.StatusConfig{
				Mapping: config.StatusMappingConfig{ItemsPath: "checks", Match: "host", Up: "ok"},
			},
			body: `{"checks":[
				{"host":"a.home","ok":true},
				{"host":"b.home","ok":false},
				{"host":"c.home","ok":"yes"},
				{"host":"","ok":true},
				{"host":{"nested":1},"ok":true}
			]}`,
			want: []Check{{Match: "a.home", Up: true}, {Match: "b.home", Up: false}},
		},
		{
			name: "json up_value with items_mode values",
			cfg: config.StatusConfig{
				Mapping: config.StatusMappingConfig{
					ItemsMode: "values", Match: "url", Up: "state", UpValue: "UP",
				},
			},
			body: `{"x":{"url":"https://a.home","state":"UP"}}`,
			want: []Check{{Match: "https://a.home", Up: true}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newProvider(t, tt.cfg, tt.body, http.StatusOK)
			got, err := p.Fetch(context.Background())
			if err != nil {
				t.Fatalf("Fetch: %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestFetchSendsHeaders(t *testing.T) {
	p := newProvider(t, config.StatusConfig{
		Headers: map[string]string{"Authorization": "Bearer t"},
		Mapping: config.StatusMappingConfig{Match: "host", Up: "ok"},
	}, `[]`, http.StatusOK)
	if _, err := p.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
}

func TestFetchErrors(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		code    int
		wantErr string
	}{
		{
			name:    "non-2xx",
			body:    `[]`,
			code:    http.StatusBadGateway,
			wantErr: "status json: request: status 502",
		},
		{
			name:    "not an array",
			body:    `{"a":1}`,
			code:    http.StatusOK,
			wantErr: "status json: map response:",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newProvider(t, config.StatusConfig{
				Mapping: config.StatusMappingConfig{Match: "host", Up: "ok"},
			}, tt.body, tt.code)
			_, err := p.Fetch(context.Background())
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestFetchHonorsContextCancellation(t *testing.T) {
	p := newProvider(t, config.StatusConfig{
		Mapping: config.StatusMappingConfig{Match: "host", Up: "ok"},
	}, `[]`, http.StatusOK)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Fetch(ctx); err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("expected context canceled, got %v", err)
	}
}
