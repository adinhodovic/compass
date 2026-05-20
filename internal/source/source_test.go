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
			Name: "bogus",
		}}}},
		&http.Client{},
	)
	if err == nil {
		t.Fatal("expected unsupported source type error")
	}
}

func TestBuildSourcesRejectsBlankName(t *testing.T) {
	_, err := BuildSources(
		config.Config{Services: config.ServicesConfig{Sources: []config.SourceConfig{{
			Type: compass.SourceTypeStatic,
		}}}},
		&http.Client{},
	)
	if err == nil || err.Error() != "services.sources[0]: name is required" {
		t.Fatalf("expected name required error, got %v", err)
	}
}

func TestBuildSourcesRejectsDuplicateIdentity(t *testing.T) {
	_, err := BuildSources(
		config.Config{Services: config.ServicesConfig{Sources: []config.SourceConfig{
			{Type: compass.SourceTypeStatic, Name: "manual"},
			{Type: compass.SourceTypeStatic, Name: "manual"},
		}}},
		&http.Client{},
	)
	if err == nil ||
		err.Error() != `services.sources[1]: duplicate source identity "static/manual"` {
		t.Fatalf("expected duplicate identity error, got %v", err)
	}
}

func TestBuildSourcesAllowsSameNameAcrossTypes(t *testing.T) {
	entries, err := BuildSources(
		config.Config{Services: config.ServicesConfig{Sources: []config.SourceConfig{
			{Type: compass.SourceTypeStatic, Name: "local"},
			{Type: compass.SourceTypeAPI, Name: "local", Endpoint: "https://example.test"},
		}}},
		&http.Client{},
	)
	if err != nil {
		t.Fatalf("build sources: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected two entries, got %d", len(entries))
	}
}

func TestBuildSourcesSupportsDNSSD(t *testing.T) {
	entries, err := BuildSources(
		config.Config{Services: config.ServicesConfig{Sources: []config.SourceConfig{{
			Type: compass.SourceTypeDNSSD,
			Name: "lan",
			DNSSD: config.DNSSDConfig{
				Names: []string{"_http._tcp.home.arpa"},
			},
		}}}},
		&http.Client{},
	)
	if err != nil {
		t.Fatalf("build sources: %v", err)
	}
	if len(entries) != 1 || entries[0].Source.Type() != compass.SourceTypeDNSSD {
		t.Fatalf("expected dns_sd source, got %#v", entries)
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
