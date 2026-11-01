package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/adinhodovic/compass/internal/compass"
	"github.com/adinhodovic/compass/internal/config"
	"github.com/adinhodovic/compass/internal/registry"
)

func renderPath(t *testing.T, handler http.Handler, path string) string {
	t.Helper()
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, path, nil))
	if resp.Code != http.StatusOK {
		t.Fatalf("expected %s to render 200, got %d", path, resp.Code)
	}
	return resp.Body.String()
}

func TestServerRendersServiceStatus(t *testing.T) {
	checked := time.Now().Add(-time.Minute)
	services := []compass.Service{
		{
			ID: "grafana", Name: "Grafana", URL: "https://grafana.home", Source: "manual",
			Status: &compass.Status{Up: true, Provider: "blackbox", CheckedAt: checked},
		},
		{
			ID: "plex", Name: "Plex", URL: "https://plex.home", Source: "manual",
			Status: &compass.Status{Up: false, Provider: "blackbox", CheckedAt: checked},
		},
		{ID: "docs", Name: "Docs", URL: "https://docs.home", Source: "manual"},
	}
	handler := New(config.Config{}, debugProvider{
		services: services,
		providers: []registry.StatusProviderStatus{
			{Name: "blackbox", Type: "prometheus", LastChecked: checked, Checks: 4, Matched: 2},
			{
				Name:        "gatus",
				Type:        "gatus",
				LastChecked: checked,
				Error:       "status gatus: request: status 502",
			},
		},
	}, discardLogger())

	home := renderPath(t, handler, "/services")
	for _, want := range []string{
		`<span class="status size-2 shrink-0 status-success" role="img" aria-label="Up · blackbox · checked 1m ago"></span>`,
		`<span class="status size-2 shrink-0 status-error ring-4 ring-error/25" role="img" aria-label="Down · blackbox · checked 1m ago"></span>`,
		`aria-hidden="true"></span>1 up</span>`,
		`font-medium text-error"><span class="status size-2 status-error" aria-hidden="true"></span>1 down</span>`,
		`x-on:click="statusFilter = 'down'"`,
		`"status":"up"`,
		`"status":"down"`,
	} {
		if !strings.Contains(home, want) {
			t.Fatalf("expected home to contain %q", want)
		}
	}
	if got := strings.Count(home, `class="status size-2 shrink`); got != 2 {
		t.Fatalf("expected exactly 2 card status dots (Docs has none), got %d", got)
	}

	plex := renderPath(t, handler, "/services/plex")
	if !strings.Contains(plex, "Down · blackbox · checked 1m ago") ||
		!strings.Contains(plex, "badge-error") {
		t.Fatal("expected detail page to show Down badge from blackbox")
	}
	if docs := renderPath(t, handler, "/services/docs"); strings.Contains(docs, "· checked") {
		t.Fatal("expected no status badge for a service without status")
	}

	debug := renderPath(t, handler, "/debug")
	for _, want := range []string{
		`Status providers`,
		`4 checks · 2 matched services`,
		`status gatus: request: status 502`,
	} {
		if !strings.Contains(debug, want) {
			t.Fatalf("expected debug page to contain %q", want)
		}
	}
}

func TestHomeHidesStatusSummaryWithoutStatus(t *testing.T) {
	handler := New(config.Config{}, staticProvider{{
		ID: "grafana", Name: "Grafana", URL: "https://grafana.home", Source: "manual",
	}}, discardLogger())
	body := renderPath(t, handler, "/services")
	if strings.Contains(body, " down</span>") ||
		strings.Contains(body, `x-on:click="statusFilter = 'down'"`) {
		t.Fatal("expected no status summary or filter when no service has status")
	}
	if !strings.Contains(body, "<p>1 service across 1 tag</p>") {
		t.Fatal("expected the plain service count sentence")
	}
}

func TestDebugHidesStatusProvidersWhenNoneConfigured(t *testing.T) {
	handler := New(config.Config{}, debugProvider{}, discardLogger())
	if body := renderPath(t, handler, "/debug"); strings.Contains(body, "Status providers") {
		t.Fatal("expected no status providers section without providers")
	}
}
