package pages

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adinhodovic/compass/internal/compass"
)

func TestLoaderEnabled(t *testing.T) {
	if NewLoader("").Enabled() {
		t.Fatal("expected empty dir to be disabled")
	}
	if !NewLoader("/tmp").Enabled() {
		t.Fatal("expected non-empty dir to be enabled")
	}
}

func TestSectionsTopLevelOnly(t *testing.T) {
	dir := t.TempDir()
	notes := strings.Join([]string{
		"---",
		"title: Notes",
		"order: 5",
		"tags: [meta, docs]",
		"---",
		"# Notes",
		"",
		"Some **bold** text.",
		"",
	}, "\n")
	plain := "Plain markdown without front-matter.\n"
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte(notes), 0o600); err != nil {
		t.Fatalf("write notes: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, "homelab-runbook.md"),
		[]byte(plain),
		0o600,
	); err != nil {
		t.Fatalf("write runbook: %v", err)
	}

	loader := NewLoader(dir)
	sections, err := loader.Sections()
	if err != nil {
		t.Fatalf("sections: %v", err)
	}
	if len(sections) != 1 {
		t.Fatalf("expected 1 section, got %d", len(sections))
	}
	if sections[0].Title != "Pages" || sections[0].Slug != "" {
		t.Fatalf("expected top-level Pages section, got %+v", sections[0])
	}
	if len(sections[0].Pages) != 2 {
		t.Fatalf("expected 2 pages, got %d", len(sections[0].Pages))
	}
	if sections[0].Pages[0].Slug != "homelab-runbook" {
		t.Fatalf("expected runbook first (order 0), got %q", sections[0].Pages[0].Slug)
	}
	if sections[0].Pages[0].URL() != "/pages/homelab-runbook" {
		t.Fatalf("unexpected top-level URL: %q", sections[0].Pages[0].URL())
	}

	page, content, _, err := loader.Get("", "notes", nil)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if page.Title != "Notes" {
		t.Fatalf("title %q", page.Title)
	}
	if !strings.Contains(string(content), "<strong>bold</strong>") {
		t.Fatalf("expected rendered markdown, got %q", content)
	}
}

func TestSectionsWithSubdirectories(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "welcome.md"), []byte("hi\n"), 0o600); err != nil {
		t.Fatalf("write top: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "01-administration"), 0o700); err != nil {
		t.Fatalf("mkdir admin: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, "01-administration", "billing.md"),
		[]byte("# billing\n"),
		0o600,
	); err != nil {
		t.Fatalf("write billing: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "on-call"), 0o700); err != nil {
		t.Fatalf("mkdir on-call: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, "on-call", "escalation.md"),
		[]byte("# escalation\n"),
		0o600,
	); err != nil {
		t.Fatalf("write escalation: %v", err)
	}

	loader := NewLoader(dir)
	sections, err := loader.Sections()
	if err != nil {
		t.Fatalf("sections: %v", err)
	}
	if len(sections) != 3 {
		t.Fatalf("expected 3 sections (top-level + admin + on-call), got %d", len(sections))
	}
	// Order: top-level (Order=-1), administration (Order=1), on-call (Order=0).
	// Lower Order first => top-level (-1), on-call (0), administration (1).
	if sections[0].Title != "Pages" {
		t.Fatalf("expected top-level Pages first, got %q", sections[0].Title)
	}
	if sections[1].Slug != "on-call" || sections[1].Title != "On Call" {
		t.Fatalf("expected on-call section second, got %+v", sections[1])
	}
	if sections[2].Slug != "administration" || sections[2].Title != "Administration" {
		t.Fatalf("expected administration section third (order 1), got %+v", sections[2])
	}
	if sections[2].Pages[0].URL() != "/pages/administration/billing" {
		t.Fatalf("unexpected nested URL: %q", sections[2].Pages[0].URL())
	}

	// Get works for both top-level and nested.
	if _, _, _, err := loader.Get("", "welcome", nil); err != nil {
		t.Fatalf("get top-level: %v", err)
	}
	if _, _, _, err := loader.Get("administration", "billing", nil); err != nil {
		t.Fatalf("get nested: %v", err)
	}
	if _, _, _, err := loader.Get("on-call", "escalation", nil); err != nil {
		t.Fatalf("get on-call: %v", err)
	}
}

func TestGetMissingPage(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(dir)
	if _, _, _, err := loader.Get("", "nope", nil); err == nil {
		t.Fatal("expected error for missing page")
	}
}

func TestServicesShortcodeExpansion(t *testing.T) {
	dir := t.TempDir()
	body := strings.Join([]string{
		"# Monitoring",
		"",
		"{{< services tag=monitoring >}}",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "page.md"), []byte(body), 0o600); err != nil {
		t.Fatalf("write page: %v", err)
	}

	services := []compass.Service{
		{
			ID:          "g",
			Name:        "Grafana",
			URL:         "https://grafana.local",
			Tags:        []string{"monitoring"},
			Description: "dashboards",
		},
		{ID: "p", Name: "Pi-hole", URL: "https://pihole.local", Tags: []string{"network"}},
	}
	loader := NewLoader(dir)
	_, content, _, err := loader.Get("", "page", services)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	html := string(content)
	if !strings.Contains(html, "Grafana") {
		t.Fatalf("expected Grafana in rendered HTML, got %q", html)
	}
	if strings.Contains(html, "Pi-hole") {
		t.Fatalf("did not expect Pi-hole (no monitoring tag) in rendered HTML, got %q", html)
	}
	if !strings.Contains(html, `class="not-prose grid`) {
		t.Fatalf("expected card grid wrapper in rendered HTML, got %q", html)
	}
	if !strings.Contains(html, `class="card`) {
		t.Fatalf("expected card markup in rendered HTML, got %q", html)
	}
}

func TestSingleServiceShortcodeFiltersBySource(t *testing.T) {
	dir := t.TempDir()
	body := strings.Join([]string{
		"# Service",
		"",
		`{{< service id=grafana source=local >}}`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "page.md"), []byte(body), 0o600); err != nil {
		t.Fatalf("write page: %v", err)
	}

	services := []compass.Service{
		{
			ID:          "remote-grafana",
			Name:        "Grafana",
			URL:         "https://grafana.remote",
			Source:      "remote",
			Description: "remote dashboards",
		},
		{
			ID:          "local-grafana",
			Name:        "Grafana",
			URL:         "https://grafana.local",
			Source:      "local",
			Description: "local dashboards",
		},
	}
	_, content, _, err := NewLoader(dir).Get("", "page", services)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	html := string(content)
	if !strings.Contains(html, "local dashboards") {
		t.Fatalf("expected local service card, got %q", html)
	}
	if strings.Contains(html, "remote dashboards") {
		t.Fatalf("did not expect remote service card, got %q", html)
	}
}

func TestServicesShortcodeEscape(t *testing.T) {
	dir := t.TempDir()
	body := strings.Join([]string{
		"# Mixed",
		"",
		"Use `{{</* services tag=monitoring */>}}` to embed live services.",
		"",
		"{{< services tag=monitoring >}}",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "page.md"), []byte(body), 0o600); err != nil {
		t.Fatalf("write page: %v", err)
	}
	services := []compass.Service{{
		ID: "g", Name: "Grafana", URL: "https://grafana.local",
		Tags: []string{"monitoring"}, Description: "dashboards",
	}}
	_, content, _, err := NewLoader(dir).Get("", "page", services)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	html := string(content)
	// The real shortcode expands to one card grid; the escape is unwrapped
	// to the literal text and rendered inside the inline `<code>` span.
	if got := strings.Count(html, `class="not-prose grid`); got != 1 {
		t.Fatalf("expected exactly one expanded shortcode, got %d in %q", got, html)
	}
	if !strings.Contains(html, "{{&lt; services tag=monitoring &gt;}}") {
		t.Fatalf("expected escape to unwrap to literal text, got %q", html)
	}
}

func TestShortcodesInsideCodeAreNotExpanded(t *testing.T) {
	dir := t.TempDir()
	body := strings.Join([]string{
		"# Examples",
		"",
		"Inline `{{< services tag=monitoring >}}` stays literal.",
		"",
		"```md",
		"{{< service id=g >}}",
		"```",
		"",
		"{{< services tag=monitoring >}}",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "page.md"), []byte(body), 0o600); err != nil {
		t.Fatalf("write page: %v", err)
	}
	services := []compass.Service{{
		ID: "g", Name: "Grafana", URL: "https://grafana.local", Tags: []string{"monitoring"},
	}}

	_, content, _, err := NewLoader(dir).Get("", "page", services)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	html := string(content)
	if got := strings.Count(html, `class="not-prose grid`); got != 1 {
		t.Fatalf("expected only real shortcode to expand, got %d expansions in %q", got, html)
	}
	if !strings.Contains(html, "{{&lt; services tag=monitoring &gt;}}") {
		t.Fatalf("expected inline shortcode to stay literal, got %q", html)
	}
	if !strings.Contains(html, "{{&lt; service id=g &gt;}}") {
		t.Fatalf("expected fenced shortcode to stay literal, got %q", html)
	}
}

func TestPanelShortcodeExpansion(t *testing.T) {
	dir := t.TempDir()
	body := strings.Join([]string{
		"# Panels",
		"",
		`{{< panel service=grafana source=local title="Cluster CPU" >}}`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "page.md"), []byte(body), 0o600); err != nil {
		t.Fatalf("write page: %v", err)
	}
	services := []compass.Service{
		{
			ID:     "remote-grafana",
			Name:   "Grafana",
			URL:    "https://grafana.remote",
			Source: "remote",
			Panels: []compass.Panel{{
				Title: "Cluster Memory",
				URL:   "https://grafana.remote/d-solo/cluster/memory?panelId=2",
			}},
		},
		{
			ID:     "local-grafana",
			Name:   "Grafana",
			URL:    "https://grafana.local",
			Source: "local",
			Panels: []compass.Panel{{
				Title: "Cluster CPU",
				URL:   "https://grafana.local/d-solo/cluster/cpu?panelId=1",
			}},
		},
	}

	_, content, _, err := NewLoader(dir).Get("", "page", services)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	html := string(content)
	for _, want := range []string{
		`class="grafana-panel`,
		`title="Cluster CPU"`,
		`src="https://grafana.local/d-solo/cluster/cpu?panelId=1"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected panel shortcode output to contain %q, got %q", want, html)
		}
	}
}

func TestGetRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(dir)
	if _, _, _, err := loader.Get("", "../escape", nil); err == nil {
		t.Fatal("expected error for traversal slug")
	}
	if _, _, _, err := loader.Get("..", "escape", nil); err == nil {
		t.Fatal("expected error for traversal section")
	}
}
