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
)

// newProvider serves body from a test server and builds a provider from a
// one-line YAML status entry, where URL is replaced with the server's
// address. Going through config.Load exercises the real preset expansion.
func newProvider(t *testing.T, entry, body string, code int) (*Provider, *http.Header) {
	t.Helper()
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	doc := "services: {sources: [{type: static, name: manual}]}\n" +
		"status: [" + strings.ReplaceAll(entry, "URL", srv.URL) + "]\n"
	path := filepath.Join(t.TempDir(), "compass.yaml")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return New(cfg.Status[0], srv.Client()), &got
}

const jsonEntry = "{endpoint: URL, mapping: {match: host, up: ok}}"

func TestFetchMapsChecks(t *testing.T) {
	tests := []struct {
		name  string
		entry string
		body  string
		want  []Check
	}{
		{
			name:  "prometheus preset",
			entry: "{type: prometheus, url: URL}",
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
			name:  "gatus preset reads newest result",
			entry: "{type: gatus, url: URL}",
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
			name:  "json booleans without up_value",
			entry: "{endpoint: URL, mapping: {items_path: checks, match: host, up: ok}}",
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
			name:  "json up_value with items_mode values",
			entry: "{endpoint: URL, mapping: {items_mode: values, match: url, up: state, up_value: UP}}",
			body:  `{"x":{"url":"https://a.home","state":"UP"}}`,
			want:  []Check{{Match: "https://a.home", Up: true}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, _ := newProvider(t, tt.entry, tt.body, http.StatusOK)
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
	p, got := newProvider(t,
		"{endpoint: URL, headers: {Authorization: Bearer t}, mapping: {match: host, up: ok}}",
		`[]`, http.StatusOK)
	if _, err := p.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if auth := got.Get("Authorization"); auth != "Bearer t" {
		t.Fatalf("Authorization header = %q", auth)
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
			p, _ := newProvider(t, jsonEntry, tt.body, tt.code)
			_, err := p.Fetch(context.Background())
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestFetchHonorsContextCancellation(t *testing.T) {
	p, _ := newProvider(t, jsonEntry, `[]`, http.StatusOK)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Fetch(ctx); err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("expected context canceled, got %v", err)
	}
}
