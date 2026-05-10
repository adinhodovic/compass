package source

import (
	"net/http"
	"testing"
	"time"

	"github.com/adinhodovic/compass/internal/compass"
	"github.com/adinhodovic/compass/internal/config"
)

func TestBuildSourcesParsesRefreshInterval(t *testing.T) {
	entries, err := BuildSources(
		config.Config{Services: config.ServicesConfig{Sources: []config.SourceConfig{{
			Type:            compass.SourceTypeStatic,
			Name:            "manual",
			RefreshInterval: "30s",
		}}}},
		&http.Client{},
	)
	if err != nil {
		t.Fatalf("build sources: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one entry, got %d", len(entries))
	}
	if entries[0].RefreshInterval != 30*time.Second {
		t.Fatalf("unexpected refresh interval: %v", entries[0].RefreshInterval)
	}
}

func TestBuildSourcesRejectsUnsupportedType(t *testing.T) {
	_, err := BuildSources(
		config.Config{Services: config.ServicesConfig{Sources: []config.SourceConfig{{
			Type: "bogus",
		}}}},
		&http.Client{},
	)
	if err == nil {
		t.Fatal("expected unsupported source type error")
	}
}

func TestParseRefreshIntervalDefaultsAndRejectsNegative(t *testing.T) {
	got, err := parseRefreshInterval("")
	if err != nil {
		t.Fatalf("default interval: %v", err)
	}
	if got != DefaultRefreshInterval {
		t.Fatalf("expected default interval %v, got %v", DefaultRefreshInterval, got)
	}
	if _, err := parseRefreshInterval("-1s"); err == nil {
		t.Fatal("expected negative interval to fail")
	}
}
