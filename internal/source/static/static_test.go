package static

import (
	"context"
	"slices"
	"testing"

	"github.com/adinhodovic/compass/internal/compass"
	"github.com/adinhodovic/compass/internal/config"
)

func TestNewDefaultsNameToTypeWhenBlank(t *testing.T) {
	s := New(config.SourceConfig{})
	if s.Name() != compass.SourceTypeStatic {
		t.Fatalf("Name() = %q, want %q", s.Name(), compass.SourceTypeStatic)
	}
	if s.Type() != compass.SourceTypeStatic {
		t.Fatalf("Type() = %q, want %q", s.Type(), compass.SourceTypeStatic)
	}
}

func TestNewKeepsConfiguredName(t *testing.T) {
	s := New(config.SourceConfig{Name: "manual"})
	if s.Name() != "manual" {
		t.Fatalf("Name() = %q, want manual", s.Name())
	}
}

func TestLoadStampsSourceAndMergesTags(t *testing.T) {
	s := New(config.SourceConfig{
		Name: "manual",
		Tags: []string{"home", "manual"},
		Services: []compass.Service{
			{Name: "Grafana", URL: "https://grafana.example", Tags: []string{"observability"}},
			{Name: "Prometheus", URL: "https://prom.example"},
		},
	})

	out, err := s.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 services, got %d", len(out))
	}

	for _, svc := range out {
		if svc.Source != "manual" {
			t.Errorf("service %q: Source = %q, want manual", svc.Name, svc.Source)
		}
	}
	wantGrafanaTags := []string{"home", "manual", "observability"}
	if !slices.Equal(out[0].Tags, wantGrafanaTags) {
		t.Errorf("Grafana tags = %v, want %v", out[0].Tags, wantGrafanaTags)
	}
	wantPromTags := []string{"home", "manual"}
	if !slices.Equal(out[1].Tags, wantPromTags) {
		t.Errorf("Prometheus tags = %v, want %v", out[1].Tags, wantPromTags)
	}
}

func TestLoadReturnsDeepCopy(t *testing.T) {
	original := []compass.Service{
		{Name: "Grafana", URL: "https://grafana.example", Tags: []string{"a"}},
	}
	s := New(config.SourceConfig{Name: "manual", Services: original})

	out, err := s.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	out[0].Name = "Mutated"
	out[0].Tags[0] = "mutated"

	again, err := s.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if again[0].Name != "Grafana" {
		t.Fatalf("internal state mutated: Name = %q", again[0].Name)
	}
	// MergeTags allocates a fresh slice so the inner mutation should not
	// reach the second Load's output either.
	if again[0].Tags[len(again[0].Tags)-1] != "a" {
		t.Fatalf("internal state mutated: Tags = %v", again[0].Tags)
	}
}

func TestNewWarnsOnInvalidURLButStillReturns(t *testing.T) {
	// The warn log is fire-and-forget; we just confirm New() doesn't panic
	// or drop services with bad URLs. Registry.normalize is the layer that
	// actually filters them; this source forwards everything verbatim.
	s := New(config.SourceConfig{
		Name: "manual",
		Services: []compass.Service{
			{Name: "Bad", URL: "not a url"},
			{Name: "Good", URL: "https://ok.example"},
		},
	})
	out, err := s.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("static source should forward every configured service; got %d", len(out))
	}
}

func TestLoadHonorsCanceledContext(t *testing.T) {
	// Static source doesn't actually do I/O, so cancellation just has to
	// not corrupt the output. Mostly here so future contributors see the
	// per-source cancellation pattern documented in AGENTS.md.
	s := New(config.SourceConfig{
		Name:     "manual",
		Services: []compass.Service{{Name: "Grafana", URL: "https://ok.example"}},
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err := s.Load(ctx)
	if err != nil {
		t.Fatalf("Load with canceled ctx: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 service, got %d", len(out))
	}
}
