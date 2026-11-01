package registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/adinhodovic/compass/internal/compass"
	"github.com/adinhodovic/compass/internal/config"
	"github.com/adinhodovic/compass/internal/source"
	"github.com/adinhodovic/compass/internal/status"
)

// statusServer serves body until fail is set, then returns 500.
type statusServer struct {
	body string
	fail atomic.Bool
}

func newStatusProvider(t *testing.T, name string, srv *statusServer) *status.Provider {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if srv.fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(srv.body))
	}))
	t.Cleanup(ts.Close)
	return status.New(config.StatusConfig{
		Type:     config.StatusTypeJSON,
		Name:     name,
		Endpoint: ts.URL,
		Interval: "1h",
		Service:  "key",
		Up:       "up",
	}, ts.Client())
}

func statusRegistry(t *testing.T, services []compass.Service, opts ...Option) *Registry {
	t.Helper()
	entries := []source.Entry{{Source: quickSource{name: "manual", services: services}}}
	reg := NewFromEntries(entries, nil, nil, append(opts, WithLoadTimeout(time.Second))...)
	if _, err := reg.Load(context.Background()); err != nil {
		t.Fatalf("load: %v", err)
	}
	return reg
}

func statusByName(services []compass.Service) map[string]*compass.Status {
	out := make(map[string]*compass.Status, len(services))
	for _, s := range services {
		out[s.Name] = s.Status
	}
	return out
}

func TestServicesMatchStatusByShape(t *testing.T) {
	services := []compass.Service{
		{Name: "Grafana", URL: "https://grafana.home/dashboards"},
		{Name: "Plex", URL: "https://plex.home"},
		{Name: "Docs", URL: "https://docs.home"},
		{Name: "App A", URL: "http://192.168.1.10:3000"},
		{Name: "App B", URL: "http://192.168.1.10:8080/"},
		{Name: "Wiki", URL: "https://home.lan/wiki"},
		{Name: "Notes", URL: "https://home.lan/notes"},
	}
	tests := []struct {
		name string
		body string
		want map[string]*bool
	}{
		{
			name: "host ignores scheme, path, case, and default ports",
			body: `[{"key":"https://GRAFANA.home","up":true},
				{"key":"plex.home:443","up":false}]`,
			want: map[string]*bool{"Grafana": new(true), "Plex": new(false), "Docs": nil},
		},
		{
			name: "host keeps non-default ports apart",
			body: `[{"key":"192.168.1.10:3000","up":true},
				{"key":"http://192.168.1.10:8080","up":false}]`,
			want: map[string]*bool{"App A": new(true), "App B": new(false), "Grafana": nil},
		},
		{
			name: "a path matches the full URL, keeping same-host services apart",
			body: `[{"key":"https://home.lan/wiki/","up":true},
				{"key":"home.lan/notes","up":false}]`,
			want: map[string]*bool{"Wiki": new(true), "Notes": new(false)},
		},
		{
			name: "a check with a path also covers its host",
			body: `[{"key":"https://grafana.home/api/health","up":false},
				{"key":"https://plex.home/health/","up":true}]`,
			want: map[string]*bool{"Grafana": new(false), "Plex": new(true), "Docs": nil},
		},
		{
			name: "a URL beats the host for the same service",
			body: `[{"key":"home.lan","up":false},{"key":"https://home.lan/wiki","up":true}]`,
			want: map[string]*bool{"Wiki": new(true), "Notes": new(false)},
		},
		{
			name: "a plain word matches the name, ignoring case",
			body: `[{"key":"plex","up":true},{"key":"DOCS","up":false}]`,
			want: map[string]*bool{"Plex": new(true), "Docs": new(false), "Grafana": nil},
		},
		{
			name: "duplicate keys resolve to down",
			body: `[{"key":"grafana.home","up":true},{"key":"https://grafana.home","up":false}]`,
			want: map[string]*bool{"Grafana": new(false), "Plex": nil},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newStatusProvider(t, "mon", &statusServer{body: tt.body})
			got := statusByName(statusRegistry(t, services, WithStatusProviders(p)).Services())
			for name, want := range tt.want {
				st := got[name]
				switch {
				case want == nil && st != nil:
					t.Errorf("%s: expected no status, got %+v", name, st)
				case want != nil && st == nil:
					t.Errorf("%s: expected status up=%v, got none", name, *want)
				case want != nil && (st.Up != *want || st.Provider != "mon" || st.CheckedAt.IsZero()):
					t.Errorf("%s: expected up=%v from mon, got %+v", name, *want, st)
				}
			}
		})
	}
}

func TestFirstMatchingStatusProviderWins(t *testing.T) {
	first := newStatusProvider(t, "first",
		&statusServer{body: `[{"key":"a.home","up":false}]`})
	second := newStatusProvider(t, "second",
		&statusServer{body: `[{"key":"a.home","up":true},{"key":"b.home","up":true}]`})
	reg := statusRegistry(t, []compass.Service{
		{Name: "A", URL: "https://a.home"},
		{Name: "B", URL: "https://b.home"},
	}, WithStatusProviders(first, second))

	got := statusByName(reg.Services())
	if got["A"].Provider != "first" || got["A"].Up {
		t.Fatalf("expected A down from first, got %+v", got["A"])
	}
	if got["B"].Provider != "second" || !got["B"].Up {
		t.Fatalf("expected B up from second, got %+v", got["B"])
	}
}

func TestFailedStatusRefreshKeepsLastSnapshot(t *testing.T) {
	srv := &statusServer{body: `[{"key":"a.home","up":true}]`}
	p := newStatusProvider(t, "mon", srv)
	reg := statusRegistry(t, []compass.Service{{Name: "A", URL: "https://a.home"}},
		WithStatusProviders(p))

	srv.fail.Store(true)
	if err := reg.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if st := statusByName(reg.Services())["A"]; st == nil || !st.Up {
		t.Fatalf("expected last good status to survive a failed fetch, got %+v", st)
	}
	providers := reg.StatusProviders()
	if len(providers) != 1 || providers[0].Error == "" || providers[0].Matched != 1 ||
		providers[0].Checks != 1 || providers[0].LastChecked.IsZero() {
		t.Fatalf("unexpected provider status: %+v", providers)
	}

	srv.fail.Store(false)
	if err := reg.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if providers := reg.StatusProviders(); providers[0].Error != "" {
		t.Fatalf("expected error cleared after success, got %+v", providers[0])
	}
}

func TestServicesStatusIsNotShared(t *testing.T) {
	p := newStatusProvider(t, "mon",
		&statusServer{body: `[{"key":"a.home","up":true}]`})
	reg := statusRegistry(t, []compass.Service{{Name: "A", URL: "https://a.home"}},
		WithStatusProviders(p))

	first := reg.Services()
	first[0].Status.Up = false
	if second := reg.Services(); !second[0].Status.Up {
		t.Fatal("mutating a returned Status leaked into the registry")
	}
}

func TestDiscoveryExplanationsDescribeStatusMatch(t *testing.T) {
	p := newStatusProvider(t, "mon",
		&statusServer{body: `[{"key":"a.home:8080","up":false}]`})
	reg := statusRegistry(t, []compass.Service{
		{ID: "a", Name: "A", URL: "http://a.home:8080"},
		{ID: "b", Name: "B", URL: "https://b.home"},
	}, WithStatusProviders(p))

	steps := map[string][]string{}
	for _, explanation := range reg.DiscoveryExplanations() {
		steps[explanation.ID] = explanation.Steps
	}
	want := map[string]string{
		"a": `Status: down, reported by mon (matched host "a.home:8080")`,
		"b": `No status: no check matched host "b.home", name "b"`,
	}
	for id, step := range want {
		got := steps[id]
		if len(got) == 0 || got[len(got)-1] != step {
			t.Errorf("%s: expected last step %q, got %q", id, step, got)
		}
	}
}

func TestRegistryWithoutStatusProviders(t *testing.T) {
	reg := statusRegistry(t, []compass.Service{{ID: "a", Name: "A", URL: "https://a.home"}})
	if st := reg.Services()[0].Status; st != nil {
		t.Fatalf("expected nil status without providers, got %+v", st)
	}
	if providers := reg.StatusProviders(); len(providers) != 0 {
		t.Fatalf("expected no providers, got %+v", providers)
	}
	for _, explanation := range reg.DiscoveryExplanations() {
		for _, step := range explanation.Steps {
			if strings.Contains(step, "tatus") {
				t.Fatalf("unexpected status step without providers: %q", step)
			}
		}
	}
}
