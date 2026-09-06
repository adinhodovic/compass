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
func (s staticProvider) DiscoveryExplanations() []registry.ServiceExplanation { return nil }

type debugProvider struct {
	services     []compass.Service
	statuses     []registry.SourceStatus
	dropped      []registry.DroppedService
	explanations []registry.ServiceExplanation
}

func (p debugProvider) Services() []compass.Service                          { return p.services }
func (p debugProvider) SourceStatuses() []registry.SourceStatus              { return p.statuses }
func (p debugProvider) DroppedServices() []registry.DroppedService           { return p.dropped }
func (p debugProvider) DiscoveryExplanations() []registry.ServiceExplanation { return p.explanations }

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

func TestServiceNotesEditingModes(t *testing.T) {
	provider := staticProvider{{
		ID:         "manual-grafana",
		Name:       "Grafana",
		URL:        "https://grafana.local",
		Source:     "manual",
		SourceType: compass.SourceTypeStatic,
	}}

	t.Run("open", func(t *testing.T) {
		handler := New(config.Config{}, provider, discardLogger())
		req := httptest.NewRequest(http.MethodGet, "/services/manual-grafana", nil)
		resp := httptest.NewRecorder()

		handler.ServeHTTP(resp, req)

		if resp.Code != http.StatusOK {
			t.Fatalf("expected detail to render 200, got %d", resp.Code)
		}
		body := resp.Body.String()
		for _, want := range []string{
			`data-user="open"`,
			"Stored in this browser only.",
			"service-notes",
			">Edit</button>",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("expected open detail to contain %q", want)
			}
		}
		for _, unwanted := range []string{"Sign in to keep private notes for this service.", `href="/login"`} {
			if strings.Contains(body, unwanted) {
				t.Fatalf("open detail should not contain %q", unwanted)
			}
		}
	})

	t.Run("optional basic", func(t *testing.T) {
		handler := New(config.Config{
			Auth: config.AuthConfig{
				UserHeader: "X-Forwarded-User",
				Basic: config.BasicAuthConfig{Users: []config.BasicAuthUser{{
					Name:         "admin",
					PasswordHash: "$2a$10$He3AU65PBOkfE3oeq0dRxuYvEbBkWECslj3JchYXRVAAqoA6FIaAu",
				}}},
			},
		}, provider, discardLogger())

		req := httptest.NewRequest(http.MethodGet, "/services/manual-grafana", nil)
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, req)
		body := resp.Body.String()
		if strings.Contains(body, "service-notes") {
			t.Fatalf("anonymous optional-basic detail should not contain notes textarea")
		}
		if !strings.Contains(body, `href="/login"`) ||
			!strings.Contains(body, "to add private notes for this service.") {
			t.Fatalf("expected anonymous optional-basic detail to explain private notes")
		}

		req = httptest.NewRequest(http.MethodGet, "/services/manual-grafana", nil)
		req.SetBasicAuth("admin", "admin")
		resp = httptest.NewRecorder()
		handler.ServeHTTP(resp, req)
		body = resp.Body.String()
		for _, want := range []string{`data-user="admin"`, "service-notes", ">Edit</button>"} {
			if !strings.Contains(body, want) {
				t.Fatalf("expected authenticated detail to contain %q", want)
			}
		}
	})
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

func TestServerServesConfiguredAssetsDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(dir, "internal-app.png"),
		[]byte("png"),
		0o600,
	); err != nil {
		t.Fatalf("write icon: %v", err)
	}
	handler := New(
		config.Config{Assets: config.AssetsConfig{Dir: dir}},
		staticProvider{},
		discardLogger(),
	)

	req := httptest.NewRequest(http.MethodGet, "/assets/internal-app.png", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected configured asset to render 200, got %d", resp.Code)
	}
	if resp.Body.String() != "png" {
		t.Fatalf("unexpected asset body: %q", resp.Body.String())
	}
}

func TestServerDoesNotListConfiguredAssetsDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(dir, "internal-app.png"),
		[]byte("png"),
		0o600,
	); err != nil {
		t.Fatalf("write icon: %v", err)
	}
	handler := New(
		config.Config{Assets: config.AssetsConfig{Dir: dir}},
		staticProvider{},
		discardLogger(),
	)

	req := httptest.NewRequest(http.MethodGet, "/assets/", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected configured asset directory to return 404, got %d", resp.Code)
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
		`aria-label="Copy dashboard view link"`,
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

func TestAPIServicesReturnsAccessFilteredRegistry(t *testing.T) {
	provider := staticProvider{
		{
			ID:         "public",
			Name:       "Public",
			URL:        "https://public.local",
			Source:     "manual",
			SourceType: compass.SourceTypeStatic,
		},
		{
			ID:         "private",
			Name:       "Private",
			URL:        "https://private.local",
			Source:     "private",
			SourceType: compass.SourceTypeStatic,
		},
	}
	handler := New(config.Config{
		Auth: config.AuthConfig{GroupsHeader: "X-Forwarded-Groups"},
		Services: config.ServicesConfig{Sources: []config.SourceConfig{{
			Type:   compass.SourceTypeStatic,
			Name:   "private",
			Access: config.SourceAccessConfig{RequiredGroups: []string{"admins"}},
		}}},
	}, provider, discardLogger())

	request := func(groups string) servicesAPIResponse {
		req := httptest.NewRequest(http.MethodGet, "/api/services", nil)
		if groups != "" {
			req.Header.Set("X-Forwarded-Groups", groups)
		}
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Fatalf("expected /api/services to render 200, got %d", resp.Code)
		}
		if got := resp.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("expected no-store cache control, got %q", got)
		}
		var body servicesAPIResponse
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode API response: %v", err)
		}
		return body
	}

	if body := request(""); len(body.Services) != 1 || body.Services[0].ID != "public" {
		t.Fatalf("expected only public service, got %#v", body.Services)
	}
	if body := request("admins"); len(body.Services) != 2 {
		t.Fatalf("expected both services for admins, got %#v", body.Services)
	}
}

func TestAPIServicesRequiresNormalAuth(t *testing.T) {
	handler := New(config.Config{Auth: config.AuthConfig{
		Required:   true,
		UserHeader: "X-Forwarded-User",
	}}, staticProvider{}, discardLogger())
	req := httptest.NewRequest(http.MethodGet, "/api/services", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected /api/services to require auth, got %d", resp.Code)
	}
}

func TestAPIServicesRejectsWriteMethods(t *testing.T) {
	handler := New(config.Config{}, staticProvider{}, discardLogger())
	req := httptest.NewRequest(http.MethodPost, "/api/services", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected /api/services POST to return 405, got %d", resp.Code)
	}
	if got := resp.Header().Get("Allow"); got != "GET, HEAD" {
		t.Fatalf("expected GET, HEAD Allow header, got %q", got)
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
		services: []compass.Service{{
			ID:         "grafana",
			Name:       "Grafana",
			URL:        "https://grafana.local",
			Source:     "manual",
			SourceType: compass.SourceTypeStatic,
		}},
		statuses: []registry.SourceStatus{{Name: "manual", Type: compass.SourceTypeStatic}},
		explanations: []registry.ServiceExplanation{{
			ID:    "grafana",
			Steps: []string{"Accepted and published", "ID generated from source and name"},
		}},
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
	for _, want := range []string{"Dropped", "Wildcard", "wildcard host excluded", "Returned by this source", "Why included", "ID generated from source and name"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected debug page to contain %q, got %q", want, body)
		}
	}
}

func TestDebugRouteRequiresConfiguredGroups(t *testing.T) {
	handler := New(config.Config{
		Auth:  config.AuthConfig{GroupsHeader: "X-Forwarded-Groups"},
		Debug: config.DebugConfig{RequiredGroups: []string{"admins"}},
	}, staticProvider{}, discardLogger())

	for _, tc := range []struct {
		name   string
		groups string
		want   int
	}{
		{name: "anonymous", want: http.StatusNotFound},
		{name: "wrong group", groups: "viewers", want: http.StatusNotFound},
		{name: "allowed group", groups: "admins", want: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/debug", nil)
			if tc.groups != "" {
				req.Header.Set("X-Forwarded-Groups", tc.groups)
			}
			resp := httptest.NewRecorder()
			handler.ServeHTTP(resp, req)
			if resp.Code != tc.want {
				t.Fatalf("expected status %d, got %d", tc.want, resp.Code)
			}
		})
	}
}

func TestDebugShowsRestrictedSourcesWhenDebugAllowed(t *testing.T) {
	handler := New(config.Config{
		Auth:  config.AuthConfig{GroupsHeader: "X-Forwarded-Groups"},
		Debug: config.DebugConfig{RequiredGroups: []string{"admins"}},
		Services: config.ServicesConfig{Sources: []config.SourceConfig{{
			Type: compass.SourceTypeStatic,
			Name: "private",
			Access: config.SourceAccessConfig{
				RequiredGroups: []string{"finance"},
			},
		}}},
	}, debugProvider{
		services: []compass.Service{{
			ID:         "payroll",
			Name:       "Payroll",
			URL:        "https://payroll.local",
			Source:     "private",
			SourceType: compass.SourceTypeStatic,
		}},
		statuses: []registry.SourceStatus{{Name: "private", Type: compass.SourceTypeStatic}},
	}, discardLogger())
	req := httptest.NewRequest(http.MethodGet, "/debug", nil)
	req.Header.Set("X-Forwarded-Groups", "admins")
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected /debug to render 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	for _, want := range []string{"Payroll", "Restricted source", "finance"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected debug page to contain %q", want)
		}
	}
}

func TestOptionalBasicTrustsForwardedGroupsWhenProxiesUnset(t *testing.T) {
	handler := New(config.Config{
		Auth: config.AuthConfig{
			UserHeader:   "X-Forwarded-User",
			GroupsHeader: "X-Forwarded-Groups",
			Basic: config.BasicAuthConfig{Users: []config.BasicAuthUser{{
				Name:         "admin",
				PasswordHash: "$2a$10$He3AU65PBOkfE3oeq0dRxuYvEbBkWECslj3JchYXRVAAqoA6FIaAu",
				Groups:       []string{"admins"},
			}}},
		},
		Debug: config.DebugConfig{RequiredGroups: []string{"admins"}},
	}, staticProvider{}, discardLogger())

	req := httptest.NewRequest(http.MethodGet, "/debug", nil)
	req.Header.Set("X-Forwarded-User", "admin")
	req.Header.Set("X-Forwarded-Groups", "admins")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected forwarded groups to be trusted, got %d", resp.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/debug", nil)
	req.SetBasicAuth("admin", "admin")
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected verified basic auth to access debug, got %d", resp.Code)
	}
}

func TestOptionalBasicAcceptsForwardedGroupsFromTrustedProxy(t *testing.T) {
	handler := New(config.Config{
		Auth: config.AuthConfig{
			UserHeader:     "X-Forwarded-User",
			GroupsHeader:   "X-Forwarded-Groups",
			TrustedProxies: []string{"10.0.0.0/8"},
			Basic: config.BasicAuthConfig{Users: []config.BasicAuthUser{{
				Name:         "admin",
				PasswordHash: "$2a$10$He3AU65PBOkfE3oeq0dRxuYvEbBkWECslj3JchYXRVAAqoA6FIaAu",
				Groups:       []string{"admins"},
			}}},
		},
		Debug: config.DebugConfig{RequiredGroups: []string{"admins"}},
	}, staticProvider{}, discardLogger())

	for _, tc := range []struct {
		name   string
		remote string
		want   int
	}{
		{name: "trusted proxy", remote: "10.1.2.3:1234", want: http.StatusOK},
		{name: "untrusted remote", remote: "203.0.113.10:1234", want: http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/debug", nil)
			req.RemoteAddr = tc.remote
			req.Header.Set("X-Forwarded-User", "admin")
			req.Header.Set("X-Forwarded-Groups", "admins")
			resp := httptest.NewRecorder()
			handler.ServeHTTP(resp, req)
			if resp.Code != tc.want {
				t.Fatalf("expected status %d, got %d", tc.want, resp.Code)
			}
		})
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
	if got := sourceLabel(compass.SourceTypeDNSSD, "lan"); got != "DNS SD · Lan" {
		t.Fatalf("expected dns_sd source label, got %q", got)
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

func TestSourceAccessFiltersServicesByGroups(t *testing.T) {
	provider := staticProvider{
		{
			ID:         "grafana",
			Name:       "Grafana",
			URL:        "https://grafana.local",
			Source:     "public",
			SourceType: compass.SourceTypeStatic,
		},
		{
			ID:         "prometheus",
			Name:       "Prometheus",
			URL:        "https://prometheus.local",
			Source:     "private",
			SourceType: compass.SourceTypeStatic,
		},
	}
	handler := New(config.Config{
		Auth: config.AuthConfig{GroupsHeader: "X-Forwarded-Groups"},
		Services: config.ServicesConfig{Sources: []config.SourceConfig{
			{Type: compass.SourceTypeStatic, Name: "public"},
			{
				Type: compass.SourceTypeStatic,
				Name: "private",
				Access: config.SourceAccessConfig{
					RequiredGroups: []string{"admins"},
				},
			},
		}},
	}, provider, discardLogger())

	for _, tc := range []struct {
		name             string
		groups           string
		wantPrometheus   bool
		wantDetailStatus int
	}{
		{name: "anonymous", wantDetailStatus: http.StatusNotFound},
		{name: "wrong group", groups: "viewers", wantDetailStatus: http.StatusNotFound},
		{name: "allowed group", groups: "ops, admins", wantPrometheus: true, wantDetailStatus: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/services", nil)
			if tc.groups != "" {
				req.Header.Set("X-Forwarded-Groups", tc.groups)
			}
			resp := httptest.NewRecorder()
			handler.ServeHTTP(resp, req)

			if resp.Code != http.StatusOK {
				t.Fatalf("expected /services to render 200, got %d", resp.Code)
			}
			body := resp.Body.String()
			if !strings.Contains(body, "Grafana") {
				t.Fatalf("expected public service to render")
			}
			if got := strings.Contains(body, "Prometheus"); got != tc.wantPrometheus {
				t.Fatalf("private service rendered=%v, want %v", got, tc.wantPrometheus)
			}

			req = httptest.NewRequest(http.MethodGet, "/services/prometheus", nil)
			if tc.groups != "" {
				req.Header.Set("X-Forwarded-Groups", tc.groups)
			}
			resp = httptest.NewRecorder()
			handler.ServeHTTP(resp, req)
			if resp.Code != tc.wantDetailStatus {
				t.Fatalf(
					"expected private detail status %d, got %d",
					tc.wantDetailStatus,
					resp.Code,
				)
			}
		})
	}
}

func TestDevAdminBasicAuthSeesRestrictedSource(t *testing.T) {
	handler := New(config.Config{
		Auth: config.AuthConfig{
			UserHeader:   "X-Forwarded-User",
			GroupsHeader: "X-Forwarded-Groups",
			Basic: config.BasicAuthConfig{Users: []config.BasicAuthUser{{
				Name:         "admin",
				PasswordHash: "$2a$10$He3AU65PBOkfE3oeq0dRxuYvEbBkWECslj3JchYXRVAAqoA6FIaAu",
				Groups:       []string{"admins", "ops"},
			}}},
		},
		Services: config.ServicesConfig{Sources: []config.SourceConfig{
			{Type: compass.SourceTypeStatic, Name: "manual"},
			{Type: compass.SourceTypeStatic, Name: "lab"},
			{
				Type: compass.SourceTypeStatic,
				Name: "private",
				Access: config.SourceAccessConfig{
					RequiredGroups: []string{"admins"},
				},
			},
		}},
	}, staticProvider{
		{
			ID:         "grafana",
			Name:       "Grafana",
			URL:        "https://grafana.local",
			Source:     "manual",
			SourceType: compass.SourceTypeStatic,
		},
		{
			ID:         "duplicate-demo-lab",
			Name:       "Duplicate Demo",
			URL:        "https://duplicate-demo.local",
			Source:     "lab",
			SourceType: compass.SourceTypeStatic,
		},
		{
			ID:         "admin-console",
			Name:       "Admin Console",
			URL:        "https://admin.local",
			Source:     "private",
			SourceType: compass.SourceTypeStatic,
		},
	}, discardLogger())

	unauthReq := httptest.NewRequest(http.MethodGet, "/services", nil)
	unauthResp := httptest.NewRecorder()
	handler.ServeHTTP(unauthResp, unauthReq)
	if unauthResp.Code != http.StatusOK {
		t.Fatalf(
			"expected unauthenticated request to render public services, got %d",
			unauthResp.Code,
		)
	}
	unauthBody := unauthResp.Body.String()
	if !strings.Contains(unauthBody, "Grafana") ||
		!strings.Contains(unauthBody, "Duplicate Demo") ||
		strings.Contains(unauthBody, "Admin Console") {
		t.Fatalf("expected anonymous user to see public but not restricted services")
	}
	if !strings.Contains(unauthBody, `href="/login"`) {
		t.Fatalf("expected anonymous page to include login link")
	}

	loginReq := httptest.NewRequest(http.MethodGet, "/login", nil)
	loginResp := httptest.NewRecorder()
	handler.ServeHTTP(loginResp, loginReq)
	if loginResp.Code != http.StatusUnauthorized {
		t.Fatalf("expected /login without credentials to challenge, got %d", loginResp.Code)
	}
	if got := loginResp.Header().Get("WWW-Authenticate"); got != `Basic realm="compass"` {
		t.Fatalf("unexpected basic challenge header: %q", got)
	}

	loginReq = httptest.NewRequest(http.MethodGet, "/login", nil)
	loginReq.SetBasicAuth("admin", "admin")
	loginResp = httptest.NewRecorder()
	handler.ServeHTTP(loginResp, loginReq)
	if loginResp.Code != http.StatusFound || loginResp.Header().Get("Location") != "/" {
		t.Fatalf(
			"expected successful login to redirect home, got %d %q",
			loginResp.Code,
			loginResp.Header().Get("Location"),
		)
	}

	logoutReq := httptest.NewRequest(http.MethodGet, "/logout", nil)
	logoutReq.SetBasicAuth("admin", "admin")
	logoutResp := httptest.NewRecorder()
	handler.ServeHTTP(logoutResp, logoutReq)
	if logoutResp.Code != http.StatusUnauthorized {
		t.Fatalf("expected /logout to return 401, got %d", logoutResp.Code)
	}
	if got := logoutResp.Header().Get("WWW-Authenticate"); got != `Basic realm="compass"` {
		t.Fatalf("unexpected logout challenge header: %q", got)
	}

	req := httptest.NewRequest(http.MethodGet, "/services", nil)
	req.SetBasicAuth("admin", "admin")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected admin request to render 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	for _, want := range []string{"Grafana", "Duplicate Demo", "Admin Console"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected admin page to contain %q", want)
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
