package headscale

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/adinhodovic/compass/internal/compass"
	"github.com/adinhodovic/compass/internal/config"
	headscalev1 "github.com/juanfont/headscale/gen/go/headscale/v1"
)

type mockClient struct {
	nodes []*headscalev1.Node
	err   error
}

func (m *mockClient) ListNodes(ctx context.Context) ([]*headscalev1.Node, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.nodes, nil
}

func TestHeadscaleLoadMapsNodes(t *testing.T) {
	source := newWithClient(
		"headnet",
		config.HeadscaleConfig{
			Address:    "headscale.example.com:50443",
			Tags:       []string{"homelab"},
			DeviceTags: []string{"node"},
			URLScheme:  "http",
		},
		&mockClient{nodes: []*headscalev1.Node{{
			Id:          7,
			Name:        "alice-laptop.dev.headscale.local",
			GivenName:   "alice-laptop",
			IpAddresses: []string{"100.64.0.5", "fd7a:115c::5"},
			Online:      true,
			Tags:        []string{"tag:server"},
			User:        &headscalev1.User{Name: "alice"},
		}}},
	)

	services, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("load nodes: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("expected one service, got %d", len(services))
	}
	got := services[0]
	if got.ID != "headscale/node/7" {
		t.Fatalf("unexpected ID: %q", got.ID)
	}
	if got.Name != "alice-laptop" {
		t.Fatalf("expected machine (given) name, got %q", got.Name)
	}
	if got.URL != "http://100.64.0.5" {
		t.Fatalf("unexpected URL: %q", got.URL)
	}
	if len(got.Tags) != 3 || got.Tags[0] != "homelab" || got.Tags[1] != "node" ||
		got.Tags[2] != "server" {
		t.Fatalf("unexpected tags: %#v", got.Tags)
	}
	if got.Metadata["user"] != "alice" {
		t.Fatalf("expected user metadata, got %#v", got.Metadata)
	}
	if got.Metadata["online"] != true {
		t.Fatalf("expected online=true metadata, got %#v", got.Metadata["online"])
	}
}

func TestHeadscaleMetadataRedactsKeysAndDedupesTags(t *testing.T) {
	source := newWithClient(
		"headnet",
		config.HeadscaleConfig{Tags: []string{"node"}, DeviceTags: []string{"node"}},
		&mockClient{nodes: []*headscalev1.Node{{
			Id:          7,
			GivenName:   "node",
			IpAddresses: []string{"100.64.0.5"},
			Online:      true,
			Tags:        []string{"tag:node", "tag:server"},
			MachineKey:  "machine-key",
			NodeKey:     "node-key",
		}}},
	)

	services, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("load nodes: %v", err)
	}
	metadata := services[0].Metadata
	if _, ok := metadata["machine_key"]; ok {
		t.Fatalf("machine_key should be redacted: %#v", metadata)
	}
	if _, ok := metadata["node_key"]; ok {
		t.Fatalf("node_key should be redacted: %#v", metadata)
	}
	if got := services[0].Tags; len(got) != 2 || got[0] != "node" || got[1] != "server" {
		t.Fatalf("expected deduped tags, got %#v", got)
	}
}

func TestHeadscaleLoadOfflineTagsNode(t *testing.T) {
	source := newWithClient(
		"headnet",
		config.HeadscaleConfig{},
		&mockClient{nodes: []*headscalev1.Node{{
			Id:          1,
			GivenName:   "stale",
			IpAddresses: []string{"100.64.0.9"},
			Online:      false,
		}}},
	)

	services, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("load nodes: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("expected one service")
	}
	tags := services[0].Tags
	if !slices.Contains(tags, "offline") {
		t.Fatalf("expected offline tag, got %#v", tags)
	}
}

func TestHeadscaleLoadReturnsError(t *testing.T) {
	source := newWithClient(
		"headnet",
		config.HeadscaleConfig{},
		&mockClient{err: errors.New("boom")},
	)

	if _, err := source.Load(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestHeadscaleIncludeDevicesFalseSkipsCall(t *testing.T) {
	client := &mockClient{err: errors.New("should not be called")}
	source := newWithClient(
		"headnet",
		config.HeadscaleConfig{IncludeDevices: new(false)},
		client,
	)

	services, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("expected no error when devices disabled, got %v", err)
	}
	if len(services) != 0 {
		t.Fatalf("expected no services, got %#v", services)
	}
}

func TestHeadscaleNewRequiresAddress(t *testing.T) {
	t.Setenv(envHeadscaleAddress, "")
	t.Setenv(envHeadscaleAPIKey, "k")
	if _, err := New(config.SourceConfig{Type: compass.SourceTypeHeadscale}); err == nil {
		t.Fatal("expected missing address error")
	}
}

func TestHeadscaleNewRequiresAPIKey(t *testing.T) {
	t.Setenv(envHeadscaleAddress, "h:50443")
	t.Setenv(envHeadscaleAPIKey, "")
	if _, err := New(config.SourceConfig{Type: compass.SourceTypeHeadscale}); err == nil {
		t.Fatal("expected missing api key error")
	}
}

func TestHeadscaleNewFillsEmptyFieldsFromEnv(t *testing.T) {
	t.Setenv(envHeadscaleAddress, "h:50443")
	t.Setenv(envHeadscaleAPIKey, "envkey")
	t.Setenv(envHeadscaleInsecure, "true")

	src, err := New(config.SourceConfig{Type: compass.SourceTypeHeadscale})
	if err != nil {
		t.Fatalf("new headscale source: %v", err)
	}
	defer func() { _ = src.Close() }()
	if src.address != "h:50443" {
		t.Fatalf("expected env address, got %q", src.address)
	}
}

func TestHeadscaleNewYAMLWinsOverEnv(t *testing.T) {
	t.Setenv(envHeadscaleAddress, "env:50443")
	t.Setenv(envHeadscaleAPIKey, "env-key")

	src, err := New(config.SourceConfig{
		Type: compass.SourceTypeHeadscale,
		Headscale: config.HeadscaleConfig{
			Address:  "yaml:50443",
			APIKey:   "yaml-key",
			Insecure: new(true),
		},
	})
	if err != nil {
		t.Fatalf("new headscale source: %v", err)
	}
	defer func() { _ = src.Close() }()
	if src.address != "yaml:50443" {
		t.Fatalf("expected YAML address to win, got %q", src.address)
	}
}
