package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/adinhodovic/compass/internal/config"
)

func TestAPILoadMapsServices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(
			[]byte(
				`{"services":[{"name":"Grafana","url":"https://grafana.local","primary":"monitoring","tags":["core"]}]}`,
			),
		)
	}))
	defer server.Close()

	source := New(config.SourceConfig{
		Name:     "custom",
		Endpoint: server.URL,
		Mapping: config.MappingConfig{
			ItemsPath: "$.services",
			Fields: map[string]string{
				"name":        "$.name",
				"url":         "$.url",
				"primary_tag": "$.primary",
				"tags":        "$.tags",
			},
		},
	}, server.Client())

	services, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("load api services: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("expected one service, got %d", len(services))
	}
	if services[0].Name != "Grafana" || services[0].Tags[0] != "core" {
		t.Fatalf("unexpected service: %#v", services[0])
	}
	if services[0].PrimaryTag != "monitoring" {
		t.Fatalf("expected primary tag to be mapped, got %q", services[0].PrimaryTag)
	}
}

func TestAPILoadMapValuesMode(t *testing.T) {
	// Consul's /v1/agent/services shape: map keyed by service ID with each
	// value being the service object.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
  "redis-1": {"Service": "redis", "Meta": {"url": "https://redis.local"}, "Tags": ["primary"]},
  "grafana-1": {"Service": "grafana", "Meta": {"url": "https://grafana.local"}, "Tags": ["dashboards"]}
}`))
	}))
	defer server.Close()

	src := New(config.SourceConfig{
		Name:     "consul",
		Endpoint: server.URL,
		Mapping: config.MappingConfig{
			ItemsMode: "values",
			Fields: map[string]string{
				"name": "Service",
				"url":  "Meta.url",
				"tags": "Tags",
			},
		},
	}, server.Client())

	services, err := src.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(services))
	}
	names := map[string]bool{services[0].Name: true, services[1].Name: true}
	if !names["redis"] || !names["grafana"] {
		t.Fatalf("unexpected services: %#v", services)
	}
}

func TestAPILoadUsesGJSONPaths(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
  "response": {
    "services.with.dots": [
      {"display.name": "Grafana", "links": [{"href": "grafana.local"}], "labels": "monitoring, dashboards"}
    ]
  }
}`))
	}))
	defer server.Close()

	src := New(config.SourceConfig{
		Name:     "custom",
		Endpoint: server.URL,
		Mapping: config.MappingConfig{
			ItemsPath: `response.services\.with\.dots`,
			URLScheme: "https",
			Fields: map[string]string{
				"name": `display\.name`,
				"url":  "links.0.href",
				"tags": "labels",
			},
		},
	}, server.Client())

	services, err := src.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("expected one service, got %d", len(services))
	}
	if services[0].Name != "Grafana" || services[0].URL != "https://grafana.local" {
		t.Fatalf("unexpected service: %#v", services[0])
	}
	if len(services[0].Tags) != 2 || services[0].Tags[0] != "monitoring" ||
		services[0].Tags[1] != "dashboards" {
		t.Fatalf("unexpected tags: %#v", services[0].Tags)
	}
}
