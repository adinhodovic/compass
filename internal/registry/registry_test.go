package registry

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/adinhodovic/compass/internal/catalog"
	"github.com/adinhodovic/compass/internal/compass"
	"github.com/adinhodovic/compass/internal/source"
)

func TestSort(t *testing.T) {
	services := []compass.Service{
		{Name: "Zed"},
		{Name: "Alpha"},
		{Name: "Beta"},
	}

	Sort(services)

	if services[0].Name != "Alpha" || services[1].Name != "Beta" || services[2].Name != "Zed" {
		t.Fatalf("unexpected sort order: %#v", services)
	}
}

func TestGroupByTagsUsesPrimaryTag(t *testing.T) {
	services := []compass.Service{
		{
			Name:       "Grafana",
			Source:     "manual",
			PrimaryTag: "monitoring",
			Tags:       []string{"core", "monitoring"},
		},
		{Name: "Docs", Source: "manual"},
	}

	groups := Group(services, compass.GroupByTags)

	if len(groups["monitoring"]) != 1 {
		t.Fatalf("expected monitoring group to contain service")
	}
	if len(groups["untagged"]) != 1 {
		t.Fatalf("expected untagged group to contain service")
	}
}

func TestGroupByTagsFallsBackToFirstTag(t *testing.T) {
	services := []compass.Service{
		{Name: "Grafana", Source: "manual", Tags: []string{"core", "monitoring"}},
	}

	groups := Group(services, compass.GroupByTags)

	if len(groups["core"]) != 1 {
		t.Fatalf("expected core group to contain service")
	}
}

func TestNormalizeUsesCatalogDescriptionFallback(t *testing.T) {
	reg := &Registry{catalog: catalog.DB{
		"grafana": {Description: "Dashboards."},
	}}

	service, _, ok := reg.normalize(
		compass.Service{Name: "Grafana", URL: "https://grafana.local"},
		"manual",
		compass.SourceTypeStatic,
	)
	if !ok {
		t.Fatal("expected service to normalize")
	}
	if service.Description != "Dashboards." {
		t.Fatalf("expected catalog fallback description, got %q", service.Description)
	}

	service, _, ok = reg.normalize(
		compass.Service{Name: "Grafana", URL: "https://grafana.local", Description: "Configured."},
		"manual",
		compass.SourceTypeStatic,
	)
	if !ok {
		t.Fatal("expected service to normalize")
	}
	if service.Description != "Configured." {
		t.Fatalf("expected configured description to win, got %q", service.Description)
	}
}

func TestNormalizeFallbackIDIncludesSourceType(t *testing.T) {
	reg := &Registry{}

	staticService, _, ok := reg.normalize(
		compass.Service{Name: "Grafana", URL: "https://grafana.local"},
		"local",
		compass.SourceTypeStatic,
	)
	if !ok {
		t.Fatal("expected static service to normalize")
	}
	dockerService, _, ok := reg.normalize(
		compass.Service{Name: "Grafana", URL: "https://grafana.local"},
		"local",
		compass.SourceTypeDocker,
	)
	if !ok {
		t.Fatal("expected docker service to normalize")
	}
	if staticService.ID == dockerService.ID {
		t.Fatalf("expected source-type-specific IDs, both got %q", staticService.ID)
	}
}

func TestApplyFiltersExcludeURLPatterns(t *testing.T) {
	services := []compass.Service{
		{Name: "Apex", URL: "https://example.com"},
		{Name: "Sub", URL: "https://api.example.com"},
		{Name: "Deep", URL: "https://foo.bar.example.com"},
		{Name: "Other", URL: "https://other.test"},
	}

	got := applyFilters(services, Filters{ExcludeURLPatterns: []string{"example.com"}})

	if len(got) != 1 || got[0].Name != "Other" {
		t.Fatalf("expected only Other to remain, got %#v", got)
	}
}

func TestApplyFiltersExcludeGlobPattern(t *testing.T) {
	// `*` in path.Match spans dots in hostnames (it only stops at `/`), so
	// `*.example.com` excludes any subdomain depth. The apex (no leading
	// label) survives because `*` requires at least one character.
	services := []compass.Service{
		{Name: "Sub", URL: "https://api.example.com"},
		{Name: "Deep", URL: "https://foo.bar.example.com"},
		{Name: "Apex", URL: "https://example.com"},
	}

	got := applyFilters(services, Filters{ExcludeURLPatterns: []string{"*.example.com"}})

	if len(got) != 1 || got[0].Name != "Apex" {
		t.Fatalf("expected only the apex host to remain, got %#v", got)
	}
}

func TestApplyFiltersDedupeWWW(t *testing.T) {
	services := []compass.Service{
		{Name: "Apex", URL: "https://example.com"},
		{Name: "Apex WWW", URL: "https://www.example.com"},
		{Name: "Lonely", URL: "https://www.other.test"},
	}

	got := applyFilters(services, Filters{DedupeWWW: true})

	names := map[string]bool{}
	for _, svc := range got {
		names[svc.Name] = true
	}
	if names["Apex WWW"] {
		t.Fatalf("expected www.example.com to be dropped, got %#v", got)
	}
	if !names["Apex"] || !names["Lonely"] {
		t.Fatalf("expected canonical + un-paired www to survive, got %#v", got)
	}
}

func TestApplyFiltersExcludeWildcardHosts(t *testing.T) {
	services := []compass.Service{
		{Name: "Wildcard", URL: "https://*.example.com"},
		{Name: "Concrete", URL: "https://api.example.com"},
	}

	got := applyFilters(services, Filters{ExcludeWildcardHosts: true})

	if len(got) != 1 || got[0].Name != "Concrete" {
		t.Fatalf("expected wildcard host to be dropped, got %#v", got)
	}
}

func TestApplyFiltersReportsDroppedReasons(t *testing.T) {
	services := []compass.Service{
		{
			Name:       "Wildcard",
			URL:        "https://*.example.com",
			Source:     "manual",
			SourceType: compass.SourceTypeStatic,
		},
		{
			Name:       "Blocked",
			URL:        "https://blocked.example.com",
			Source:     "manual",
			SourceType: compass.SourceTypeStatic,
		},
		{
			Name:       "Apex",
			URL:        "https://example.org",
			Source:     "manual",
			SourceType: compass.SourceTypeStatic,
		},
		{
			Name:       "WWW",
			URL:        "https://www.example.org",
			Source:     "manual",
			SourceType: compass.SourceTypeStatic,
		},
	}

	got, dropped := applyFiltersWithDropped(services, Filters{
		ExcludeWildcardHosts: true,
		ExcludeURLPatterns:   []string{"blocked.example.com"},
		DedupeWWW:            true,
	})

	if len(got) != 1 || got[0].Name != "Apex" {
		t.Fatalf("expected only Apex to remain, got %#v", got)
	}
	want := map[string]string{
		"Wildcard": "wildcard host excluded",
		"Blocked":  "URL host excluded by pattern",
		"WWW":      "duplicate www host",
	}
	if len(dropped) != len(want) {
		t.Fatalf("expected %d dropped services, got %#v", len(want), dropped)
	}
	for _, drop := range dropped {
		if want[drop.Name] != drop.Reason {
			t.Fatalf("unexpected drop record: %#v", drop)
		}
		if drop.SourceID() != "static/manual" {
			t.Fatalf("expected source ID static/manual, got %q", drop.SourceID())
		}
	}
}

func TestRegistryDroppedServicesIncludesNormalizationAndFilters(t *testing.T) {
	entries := []source.Entry{{Source: quickSource{name: "manual", services: []compass.Service{
		{Name: "", URL: "https://unnamed.local"},
		{Name: "Broken", URL: "://bad"},
		{Name: "Wildcard", URL: "https://*.example.com"},
		{Name: "Concrete", URL: "https://api.example.com"},
	}}}}
	reg := NewFromEntries(
		entries,
		nil,
		nil,
		WithLoadTimeout(time.Second),
		WithFilters(Filters{ExcludeWildcardHosts: true}),
	)

	loaded, err := reg.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != 1 || loaded[0].Name != "Concrete" {
		t.Fatalf("expected only Concrete to load, got %#v", loaded)
	}
	dropped := reg.DroppedServices()
	want := map[string]string{
		"":         "missing name",
		"Broken":   "invalid URL",
		"Wildcard": "wildcard host excluded",
	}
	if len(dropped) != len(want) {
		t.Fatalf("expected %d dropped services, got %#v", len(want), dropped)
	}
	for _, drop := range dropped {
		if want[drop.Name] != drop.Reason {
			t.Fatalf("unexpected drop record: %#v", drop)
		}
	}

	dropped[0].Reason = "mutated"
	if reg.DroppedServices()[0].Reason == "mutated" {
		t.Fatal("DroppedServices did not return a copy")
	}
}

func TestNormalizeUsesCatalogIconAndTagsFallback(t *testing.T) {
	reg := &Registry{catalog: catalog.DB{
		"grafana": {
			Icon:       "simple-icons:grafana",
			PrimaryTag: "dashboards",
			Tags:       []string{"observability", "dashboards"},
		},
	}}

	service, _, ok := reg.normalize(
		compass.Service{Name: "Grafana", URL: "https://grafana.local"},
		"manual",
		compass.SourceTypeStatic,
	)
	if !ok {
		t.Fatal("expected service to normalize")
	}
	if service.Icon != "simple-icons:grafana" {
		t.Fatalf("expected catalog icon fallback, got %q", service.Icon)
	}
	if len(service.Tags) != 2 || service.Tags[0] != "dashboards" {
		t.Fatalf("expected catalog tags fallback, got %v", service.Tags)
	}
	if service.PrimaryTag != "dashboards" {
		t.Fatalf("expected primary tag from catalog, got %q", service.PrimaryTag)
	}
	if service.Tags[0] != "dashboards" {
		t.Fatalf("expected catalog primary tag first, got %v", service.Tags)
	}

	service, _, ok = reg.normalize(
		compass.Service{
			Name: "Grafana",
			URL:  "https://grafana.local",
			Icon: "https://x/y.svg",
			Tags: []string{"core"},
		},
		"manual",
		compass.SourceTypeStatic,
	)
	if !ok {
		t.Fatal("expected service to normalize")
	}
	if service.Icon != "https://x/y.svg" {
		t.Fatalf("explicit icon should win, got %q", service.Icon)
	}
	if len(service.Tags) != 1 || service.Tags[0] != "core" {
		t.Fatalf("explicit tags should win, got %v", service.Tags)
	}
}

func TestNormalizePrimaryTag(t *testing.T) {
	reg := &Registry{}

	service, _, ok := reg.normalize(
		compass.Service{
			Name:       "Grafana",
			URL:        "https://grafana.local",
			PrimaryTag: "monitoring",
			Tags:       []string{"core", "monitoring"},
		},
		"manual",
		compass.SourceTypeStatic,
	)
	if !ok {
		t.Fatal("expected service to normalize")
	}
	if service.PrimaryTag != "monitoring" {
		t.Fatalf("expected explicit primary tag, got %q", service.PrimaryTag)
	}
	if len(service.Tags) != 2 || service.Tags[0] != "monitoring" || service.Tags[1] != "core" {
		t.Fatalf("expected primary tag first, got %#v", service.Tags)
	}

	service, _, ok = reg.normalize(
		compass.Service{
			Name:       "Docs",
			URL:        "https://docs.local",
			PrimaryTag: "docs",
			Tags:       []string{"internal"},
		},
		"manual",
		compass.SourceTypeStatic,
	)
	if !ok {
		t.Fatal("expected service to normalize")
	}
	if len(service.Tags) != 2 || service.Tags[0] != "docs" || service.Tags[1] != "internal" {
		t.Fatalf("expected primary tag to be prepended, got %#v", service.Tags)
	}
}

func TestNormalizeExpandsPanelServicePlaceholders(t *testing.T) {
	reg := &Registry{}

	service, _, ok := reg.normalize(
		compass.Service{
			Name: "Grafana API",
			URL:  "https://grafana.local",
			Panels: []compass.Panel{
				{
					Title: "Service traffic",
					URL:   "https://grafana.local/d-solo/services?var-service={{service.name}}&var-type={{service.type}}",
				},
			},
		},
		"manual",
		compass.SourceTypeStatic,
	)
	if !ok {
		t.Fatal("expected service to normalize")
	}
	if len(service.Panels) != 1 {
		t.Fatalf("expected one panel, got %#v", service.Panels)
	}
	want := "https://grafana.local/d-solo/services?var-service=Grafana+API&var-type=static"
	if service.Panels[0].URL != want {
		t.Fatalf("expected expanded panel URL %q, got %q", want, service.Panels[0].URL)
	}
}

func TestNormalizeValidatesServiceLinks(t *testing.T) {
	reg := &Registry{}
	openInline := false

	service, _, ok := reg.normalize(
		compass.Service{
			Name: "Grafana",
			URL:  "https://grafana.local",
			Links: []compass.Link{
				{
					Label: "Health",
					URL:   "https://grafana.local/api/health",
					Icon:  "lucide:heart-pulse",
				},
				{Label: "Runbook", URL: "/pages/on-call/grafana", NewTab: &openInline},
				{Label: "Bad", URL: "ftp://grafana.local"},
			},
		},
		"manual",
		compass.SourceTypeStatic,
	)
	if !ok {
		t.Fatal("expected service to normalize")
	}
	if len(service.Links) != 2 {
		t.Fatalf("expected two valid links, got %#v", service.Links)
	}
	if !service.Links[0].OpensInNewTab() {
		t.Fatalf("expected service links to open in a new tab by default")
	}
	if service.Links[1].OpensInNewTab() {
		t.Fatalf("expected explicit new_tab=false to be honored")
	}
}

// hangingSource blocks Load until ctx is cancelled — used to verify the
// per-refresh timeout actually fires.
type hangingSource struct{ name string }

func (h hangingSource) Name() string { return h.name }
func (h hangingSource) Type() string { return "hanging" }
func (h hangingSource) Load(ctx context.Context) ([]compass.Service, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestRegistryLoadTimeoutCancelsHangingSource(t *testing.T) {
	entries := []source.Entry{{Source: hangingSource{name: "hang"}}}
	reg := NewFromEntries(entries, nil, nil, WithLoadTimeout(50*time.Millisecond))

	start := time.Now()
	_, err := reg.Load(context.Background())
	elapsed := time.Since(start)

	if elapsed > time.Second {
		t.Fatalf("expected timeout to fire within ~50ms, took %v", elapsed)
	}
	if err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("expected error to mention deadline exceeded, got %v", err)
	}
}

// quickSource returns immediately, used to verify WithLoadTimeout doesn't
// false-trip on normal loads that complete well within the budget.
type quickSource struct {
	name     string
	services []compass.Service
}

func (q quickSource) Name() string                                      { return q.name }
func (q quickSource) Type() string                                      { return "quick" }
func (q quickSource) Load(_ context.Context) ([]compass.Service, error) { return q.services, nil }

type closeSource struct {
	quickSource
	closed *bool
}

func (c closeSource) Close() error {
	*c.closed = true
	return nil
}

func TestRegistryLoadTimeoutAllowsNormalLoad(t *testing.T) {
	svc := compass.Service{Name: "Grafana", URL: "https://grafana.local"}
	entries := []source.Entry{{Source: quickSource{name: "ok", services: []compass.Service{svc}}}}
	reg := NewFromEntries(entries, nil, nil, WithLoadTimeout(time.Second))

	loaded, err := reg.Load(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(loaded) != 1 || loaded[0].Name != "Grafana" {
		t.Fatalf("expected Grafana to load, got %#v", loaded)
	}
}

func TestRegistryServicesReturnsDeepCopy(t *testing.T) {
	services := []compass.Service{
		{
			Name: "Grafana",
			URL:  "https://grafana.local",
			Tags: []string{"monitoring"},
			Links: []compass.Link{
				{Label: "Health", URL: "https://grafana.local/api/health", NewTab: boolPtr(false)},
			},
			Panels:   []compass.Panel{{Title: "CPU", URL: "https://grafana.local/panel"}},
			Metadata: map[string]any{"nested": map[string]any{"key": "value"}},
		},
	}
	entries := []source.Entry{{Source: quickSource{name: "ok", services: services}}}
	reg := NewFromEntries(entries, nil, nil, WithLoadTimeout(time.Second))

	if _, err := reg.Load(context.Background()); err != nil {
		t.Fatalf("load: %v", err)
	}
	first := reg.Services()
	first[0].Tags[0] = "mutated"
	first[0].Links[0].Label = "mutated"
	*first[0].Links[0].NewTab = true
	first[0].Panels[0].Title = "mutated"
	first[0].Metadata["nested"].(map[string]any)["key"] = "mutated"

	second := reg.Services()
	if second[0].Tags[0] != "monitoring" {
		t.Fatalf("tags were not deep-copied: %#v", second[0].Tags)
	}
	if second[0].Links[0].Label != "Health" || second[0].Links[0].OpensInNewTab() {
		t.Fatalf("links were not deep-copied: %#v", second[0].Links)
	}
	if second[0].Panels[0].Title != "CPU" {
		t.Fatalf("panels were not deep-copied: %#v", second[0].Panels)
	}
	if second[0].Metadata["nested"].(map[string]any)["key"] != "value" {
		t.Fatalf("metadata was not deep-copied: %#v", second[0].Metadata)
	}
}

func boolPtr(v bool) *bool { return &v }

func TestRegistryCloseClosesSources(t *testing.T) {
	closed := false
	reg := NewFromEntries([]source.Entry{{Source: closeSource{closed: &closed}}}, nil, nil)

	if err := reg.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !closed {
		t.Fatal("expected source close to be called")
	}
}
