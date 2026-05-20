package dnssd

import (
	"context"
	"net"
	"reflect"
	"testing"

	"github.com/adinhodovic/compass/internal/config"
)

type fakeResolver struct {
	records map[string][]*net.SRV
	calls   []string
}

func (f *fakeResolver) LookupSRV(
	_ context.Context,
	service, proto, name string,
) (string, []*net.SRV, error) {
	key := service + "/" + proto + "/" + name
	f.calls = append(f.calls, key)
	return "", f.records[key], nil
}

func TestLoadMapsSRVRecords(t *testing.T) {
	resolver := &fakeResolver{records: map[string][]*net.SRV{
		"http/tcp/home.arpa": {
			{Target: "grafana.home.arpa.", Port: 3000, Priority: 10, Weight: 20},
		},
		"https/tcp/home.arpa": {
			{Target: "paperless.home.arpa.", Port: 443},
		},
	}}
	src := New(config.SourceConfig{
		Name: "lan",
		Tags: []string{"homelab"},
		DNSSD: config.DNSSDConfig{
			Names: []string{"_http._tcp.home.arpa", "_https._tcp.home.arpa"},
		},
	})
	src.resolver = resolver

	services, err := src.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(services) != 2 {
		t.Fatalf("expected two services, got %d", len(services))
	}
	if !reflect.DeepEqual(resolver.calls, []string{"http/tcp/home.arpa", "https/tcp/home.arpa"}) {
		t.Fatalf("unexpected resolver calls: %#v", resolver.calls)
	}
	if services[0].Name != "grafana" || services[0].URL != "http://grafana.home.arpa:3000" {
		t.Fatalf("unexpected http service: %#v", services[0])
	}
	if services[1].Name != "paperless" || services[1].URL != "https://paperless.home.arpa:443" {
		t.Fatalf("unexpected https service: %#v", services[1])
	}
	if !reflect.DeepEqual(services[0].Tags, []string{"homelab", "http"}) {
		t.Fatalf("unexpected tags: %#v", services[0].Tags)
	}
	if services[0].Metadata["priority"] != uint16(10) ||
		services[0].Metadata["weight"] != uint16(20) {
		t.Fatalf("unexpected metadata: %#v", services[0].Metadata)
	}
}

func TestLoadUsesExplicitURLScheme(t *testing.T) {
	resolver := &fakeResolver{records: map[string][]*net.SRV{
		"app/tcp/home.arpa": {{Target: "app.home.arpa.", Port: 8443}},
	}}
	src := New(config.SourceConfig{
		Name: "lan",
		DNSSD: config.DNSSDConfig{
			Names:     []string{"_app._tcp.home.arpa"},
			URLScheme: "https",
		},
	})
	src.resolver = resolver

	services, err := src.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if services[0].URL != "https://app.home.arpa:8443" {
		t.Fatalf("unexpected URL: %q", services[0].URL)
	}
}
