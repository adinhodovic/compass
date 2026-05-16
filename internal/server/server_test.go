package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adinhodovic/compass/internal/compass"
	"github.com/adinhodovic/compass/internal/config"
	"github.com/adinhodovic/compass/internal/pages"
	"github.com/adinhodovic/compass/internal/registry"
)

type staticProvider []compass.Service

func (s staticProvider) Services() []compass.Service             { return []compass.Service(s) }
func (s staticProvider) SourceStatuses() []registry.SourceStatus { return nil }
func (s staticProvider) DroppedServices() []registry.DroppedService {
	return nil
}

type debugProvider struct {
	services []compass.Service
	statuses []registry.SourceStatus
	dropped  []registry.DroppedService
}

func (p debugProvider) Services() []compass.Service                { return p.services }
func (p debugProvider) SourceStatuses() []registry.SourceStatus    { return p.statuses }
func (p debugProvider) DroppedServices() []registry.DroppedService { return p.dropped }

func TestServerRendersHomeAndDetail(t *testing.T) {
	provider := staticProvider{{
		ID:          "manual-grafana",
		Name:        "Grafana",
		URL:         "https://grafana.local",
		Source:      "manual",
		Tags:        []string{"monitoring"},
		Description: "Dashboards",
		Metadata:    map[string]any{"dashboard": "https://grafana.local"},
		Links: []compass.Link{{
			Label: "Health",
			URL:   "https://grafana.local/api/health",
			Icon:  "lucide:heart-pulse",
		}},
		Panels: []compass.Panel{{
			Title: "Cluster CPU",
			URL:   "https://grafana.local/d-solo/cluster/cpu?panelId=1",
		}},
	}}
	handler := New(config.Config{}, provider, slog.Default())

	for _, path := range []string{"/", "/services/manual-grafana"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		resp := httptest.NewRecorder()

		handler.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Fatalf("expected %s to render 200, got %d", path, resp.Code)
		}
		if path == "/services/manual-grafana" {
			body := resp.Body.String()
			for _, want := range []string{
				`title="Cluster CPU"`,
				`src="https://grafana.local/d-solo/cluster/cpu?panelId=1"`,
				`href="https://grafana.local/api/health" target="_blank" rel="noopener"`,
				`Health`,
				`aria-label="Copy Dashboard value"`,
				`<meta property="og:title" content="Grafana">`,
				`<meta property="og:description" content="Dashboards">`,
				`<meta property="og:url" content="http://example.com/services/manual-grafana">`,
				`<link rel="canonical" href="http://example.com/services/manual-grafana">`,
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("expected detail page to contain %q, got %q", want, body)
				}
			}
		}
	}
}

func TestHomeRendersPinnedAndRecentEmptyStates(t *testing.T) {
	handler := New(config.Config{}, staticProvider{}, discardLogger())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected home to render 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	for _, want := range []string{"Pinned", "No pinned services.", "Recent", "No recent services."} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected home to contain %q, got %q", want, body)
		}
	}
}

func TestHomeRendersSpecificActionLabels(t *testing.T) {
	handler := New(config.Config{}, staticProvider{{
		ID:         "manual-grafana",
		Name:       "Grafana",
		URL:        "https://grafana.local",
		Source:     "manual",
		SourceType: compass.SourceTypeStatic,
		Tags:       []string{"monitoring"},
	}}, discardLogger())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected home to render 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	for _, want := range []string{
		`aria-label="Toggle Monitoring group"`,
		`data-service-name="Grafana"`,
		`? 'Unpin ' : 'Pin '`,
		`aria-label="Copy Grafana URL"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected home to contain %q, got %q", want, body)
		}
	}
}

func TestServerHealthBypassesAuth(t *testing.T) {
	handler := New(
		config.Config{Auth: config.AuthConfig{UserHeader: "X-Forwarded-User", Required: true}},
		staticProvider{},
		discardLogger(),
	)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected /health to render 200, got %d", resp.Code)
	}
	if got := strings.TrimSpace(resp.Body.String()); got != `{"status":"ok"}` {
		t.Fatalf("unexpected health body: %q", got)
	}
	if got := resp.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("unexpected content type: %q", got)
	}
}

func TestDebugRouteDisabledReturns404(t *testing.T) {
	off := false
	handler := New(
		config.Config{Debug: config.DebugConfig{Enabled: &off}},
		staticProvider{},
		discardLogger(),
	)
	req := httptest.NewRequest(http.MethodGet, "/debug", nil)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected /debug to 404 when disabled, got %d", resp.Code)
	}
}

func TestDebugRouteEnabledByDefault(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	handler := New(config.Config{}, staticProvider{}, logger)
	req := httptest.NewRequest(http.MethodGet, "/debug", nil)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected /debug to render 200 by default, got %d\nbody: %q\nlogs: %s",
			resp.Code, resp.Body.String(), logs.String())
	}
}

func TestDebugRendersDroppedServices(t *testing.T) {
	handler := New(config.Config{}, debugProvider{
		statuses: []registry.SourceStatus{{Name: "manual", Type: compass.SourceTypeStatic}},
		dropped: []registry.DroppedService{{
			Name:       "Wildcard",
			URL:        "https://*.example.com",
			Source:     "manual",
			SourceType: compass.SourceTypeStatic,
			Reason:     "wildcard host excluded",
		}},
	}, discardLogger())
	req := httptest.NewRequest(http.MethodGet, "/debug", nil)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected /debug to render 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	for _, want := range []string{"Dropped", "Wildcard", "wildcard host excluded", "Returned by this source"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected debug page to contain %q, got %q", want, body)
		}
	}
}

func TestGroupLabelUsesTagWhenGroupedByTags(t *testing.T) {
	services := []compass.Service{{Source: "cluster", SourceType: compass.SourceTypeKubernetes}}

	if got := groupLabel("monitoring", compass.GroupByTags, services); got != "Monitoring" {
		t.Fatalf("expected tag label, got %q", got)
	}
	if got := groupLabel(
		"cluster",
		compass.GroupBySource,
		services,
	); got != "Kubernetes · Cluster" {
		t.Fatalf("expected source label, got %q", got)
	}
}

func TestPageListExcludesConfiguredHomePage(t *testing.T) {
	dir := t.TempDir()
	writePage(t, dir, "home.md", "Home")
	writePage(t, dir, "runbook.md", "Runbook")

	server := Server{
		cfg: config.Config{
			Pages: config.PagesConfig{Dir: dir},
			Home:  config.HomeConfig{Page: "home"},
		},
		pages:  pages.NewLoader(dir),
		logger: slog.Default(),
	}

	sections := server.pageList()
	if len(sections) != 1 {
		t.Fatalf("expected one non-empty section, got %d", len(sections))
	}
	if len(sections[0].Pages) != 1 || sections[0].Pages[0].Slug != "runbook" {
		t.Fatalf("expected only runbook page, got %+v", sections[0].Pages)
	}
}

func TestWithoutHomePageDoesNotMutateInput(t *testing.T) {
	server := Server{cfg: config.Config{Home: config.HomeConfig{Page: "home"}}}
	sections := []pages.Section{{
		Title: "Pages",
		Pages: []pages.Page{
			{Slug: "home", Title: "Home"},
			{Slug: "runbook", Title: "Runbook"},
		},
	}}

	filtered := server.withoutHomePage(sections)
	if len(filtered) != 1 || len(filtered[0].Pages) != 1 || filtered[0].Pages[0].Slug != "runbook" {
		t.Fatalf("unexpected filtered pages: %+v", filtered)
	}
	if len(sections[0].Pages) != 2 || sections[0].Pages[0].Slug != "home" ||
		sections[0].Pages[1].Slug != "runbook" {
		t.Fatalf("withoutHomePage mutated input: %+v", sections)
	}
}

func TestPageListExcludesConfiguredNestedHomePage(t *testing.T) {
	dir := t.TempDir()
	sectionDir := filepath.Join(dir, "01-admin")
	if err := os.MkdirAll(sectionDir, 0o700); err != nil {
		t.Fatalf("mkdir section: %v", err)
	}
	writePage(t, sectionDir, "home.md", "Admin Home")
	writePage(t, sectionDir, "billing.md", "Billing")

	server := Server{
		cfg: config.Config{
			Pages: config.PagesConfig{Dir: dir},
			Home:  config.HomeConfig{Section: "admin", Page: "home"},
		},
		pages:  pages.NewLoader(dir),
		logger: slog.Default(),
	}

	sections := server.pageList()
	if len(sections) != 1 {
		t.Fatalf("expected one section, got %d", len(sections))
	}
	if len(sections[0].Pages) != 1 || sections[0].Pages[0].Slug != "billing" {
		t.Fatalf("expected only billing page, got %+v", sections[0].Pages)
	}
}

func TestCommandIndexDoesNotIncludeConfiguredHomePage(t *testing.T) {
	dir := t.TempDir()
	writePage(t, dir, "home.md", "Welcome")
	writePage(t, dir, "runbook.md", "Runbook")

	server := Server{
		cfg: config.Config{
			Pages: config.PagesConfig{Dir: dir},
			Home:  config.HomeConfig{Page: "home"},
		},
		pages:  pages.NewLoader(dir),
		logger: slog.Default(),
	}

	var items []struct {
		Type  string `json:"type"`
		Value string `json:"value"`
		Label string `json:"label"`
	}
	if err := json.Unmarshal(
		[]byte(commandIndex(Base{Pages: server.pageList()})),
		&items,
	); err != nil {
		t.Fatalf("unmarshal command index: %v", err)
	}

	for _, item := range items {
		if item.Value == "/pages/home" || item.Label == "Welcome" {
			t.Fatalf("did not expect configured home page in command index: %+v", items)
		}
	}
	if len(items) != 1 || items[0].Value != "/pages/runbook" {
		t.Fatalf("expected only runbook page in command index, got %+v", items)
	}
}

func TestServicesTemplateIncludesFilterHooks(t *testing.T) {
	provider := staticProvider{
		{
			ID:         "grafana",
			Name:       "Grafana",
			URL:        "https://grafana.local",
			Source:     "manual",
			SourceType: compass.SourceTypeStatic,
			Tags:       []string{"monitoring"},
		},
		{
			ID:         "wiki",
			Name:       "Wiki",
			URL:        "https://wiki.local",
			Source:     "manual",
			SourceType: compass.SourceTypeStatic,
			Tags:       []string{"docs"},
		},
	}
	handler := New(config.Config{}, provider, slog.Default())
	req := httptest.NewRequest(http.MethodGet, "/services", nil)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected /services to render 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	for _, want := range []string{
		"visibleCount() + ' of '",
		"No matching services",
		"filteredGroupIDs(",
		"serviceIDs",
	} {
		if want == "serviceIDs" {
			if strings.Contains(body, want) {
				t.Fatalf("template helper name leaked into rendered body")
			}
			continue
		}
		if !strings.Contains(body, want) {
			t.Fatalf("expected rendered services template to contain %q", want)
		}
	}
}

func TestNavRendersNestedHeaderLinks(t *testing.T) {
	handler := New(
		config.Config{HeaderLinks: []config.LinkConfig{{
			Label: "Tools",
			Icon:  "lucide:wrench",
			Links: []config.LinkConfig{
				{Label: "Grafana", URL: "https://grafana.local"},
				{Label: "Runbook", URL: "/pages/runbook"},
			},
		}}},
		staticProvider{},
		slog.Default(),
	)
	req := httptest.NewRequest(http.MethodGet, "/services", nil)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected /services to render 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	for _, want := range []string{"Tools", "https://grafana.local", "/pages/runbook"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected nested header link output to contain %q", want)
		}
	}
}

func TestFullLogoReplacesHeaderAndFooterIconBrand(t *testing.T) {
	handler := New(
		config.Config{Organization: config.OrganizationConfig{
			Name:     "Compass Labs",
			Logo:     "/compact.svg",
			FullLogo: "https://example.com/compass-wordmark.svg",
		}},
		staticProvider{},
		slog.Default(),
	)
	req := httptest.NewRequest(http.MethodGet, "/services", nil)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected /services to render 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	if got := strings.Count(body, `src="https://example.com/compass-wordmark.svg"`); got != 2 {
		t.Fatalf("expected full logo in header and footer, got %d occurrences", got)
	}
	bodyStart := strings.Index(body, "<body")
	if bodyStart < 0 {
		t.Fatalf("expected rendered body")
	}
	if strings.Contains(body[bodyStart:], `src="/compact.svg"`) {
		t.Fatalf("compact logo should not render in header or footer when full_logo is set")
	}
}

func TestServiceIDsRendersJSONArray(t *testing.T) {
	got := string(serviceIDs([]compass.Service{{ID: "a"}, {ID: "b"}}))
	if got != `["a","b"]` {
		t.Fatalf("unexpected service IDs JSON: %s", got)
	}
}

func TestServicesJSONIncludesResolvedSourceAndIcon(t *testing.T) {
	data := servicesJSON([]compass.Service{{
		ID:          "grafana",
		Name:        "Grafana",
		URL:         "https://grafana.local",
		Source:      "manual",
		SourceType:  compass.SourceTypeStatic,
		PrimaryTag:  "monitoring",
		Tags:        []string{"monitoring"},
		Description: "Dashboards",
	}})

	var items []serviceJSON
	if err := json.Unmarshal([]byte(data), &items); err != nil {
		t.Fatalf("unmarshal services JSON: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one service JSON item, got %+v", items)
	}
	if items[0].SourceLabel != "Static · Manual" {
		t.Fatalf("unexpected source label: %q", items[0].SourceLabel)
	}
	if items[0].PrimaryTag != "monitoring" {
		t.Fatalf("unexpected primary tag: %q", items[0].PrimaryTag)
	}
	if items[0].IconText == "" && items[0].IconURL == "" {
		t.Fatalf("expected pre-resolved icon fields, got %+v", items[0])
	}
}

func TestCommandIndexIncludesServiceKeywords(t *testing.T) {
	data := commandIndex(Base{Services: []compass.Service{{
		ID:         "grafana",
		Name:       "Grafana",
		Source:     "manual",
		SourceType: compass.SourceTypeStatic,
		Tags:       []string{"monitoring", "core"},
	}}})

	var items []struct {
		Type     string `json:"type"`
		Value    string `json:"value"`
		Section  string `json:"section"`
		Keywords string `json:"keywords"`
	}
	if err := json.Unmarshal([]byte(data), &items); err != nil {
		t.Fatalf("unmarshal command index: %v", err)
	}
	if len(items) != 1 || items[0].Type != "service" || items[0].Value != "/services/grafana" {
		t.Fatalf("unexpected command service item: %+v", items)
	}
	for _, want := range []string{"manual", compass.SourceTypeStatic, "monitoring", "core"} {
		if !strings.Contains(items[0].Keywords, want) {
			t.Fatalf("expected command keywords to contain %q, got %q", want, items[0].Keywords)
		}
	}
}

func writePage(t *testing.T, dir string, name string, title string) {
	t.Helper()
	body := strings.Join([]string{"---", "title: " + title, "---", "", "# " + title, ""}, "\n")
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatalf("write page %s: %v", name, err)
	}
}

func TestPageHandlerRejectsUnsafeSegments(t *testing.T) {
	handler := New(
		config.Config{Pages: config.PagesConfig{Dir: t.TempDir()}},
		staticProvider{},
		slog.Default(),
	)

	// http.ServeMux normalizes leading-dot paths via 301/307 redirects
	// before the handler runs, so requests like /pages/.. never reach
	// s.page. The cases below survive mux normalization and must still
	// be rejected by the handler's own isSafeSegment check + nesting cap.
	for _, path := range []string{
		"/pages/with..dots",
		"/pages/section/with..dots",
		"/pages/a/b/c", // too deep — only one level of nesting is allowed
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, req)
		if resp.Code != http.StatusNotFound {
			t.Errorf("expected 404 for %s, got %d", path, resp.Code)
		}
	}
}

func TestDetailHandlerRedirectsBareSlash(t *testing.T) {
	handler := New(config.Config{}, staticProvider{}, slog.Default())
	req := httptest.NewRequest(http.MethodGet, "/services/", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", resp.Code)
	}
	if resp.Header().Get("Location") != "/services" {
		t.Fatalf("expected redirect to /services, got %q", resp.Header().Get("Location"))
	}
}

func TestDetailHandlerNotFoundForUnknownID(t *testing.T) {
	handler := New(config.Config{}, staticProvider{}, slog.Default())
	req := httptest.NewRequest(http.MethodGet, "/services/does-not-exist", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.Code)
	}
}
