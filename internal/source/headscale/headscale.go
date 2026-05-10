package headscale

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/adinhodovic/compass/internal/compass"
	"github.com/adinhodovic/compass/internal/config"
	"github.com/adinhodovic/compass/internal/source/meta"
	headscalev1 "github.com/juanfont/headscale/gen/go/headscale/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	grpcmetadata "google.golang.org/grpc/metadata"
)

// Well-known env vars consulted when the matching YAML field is empty.
// Mirrors the tailscale source pattern; multi-tenant configs use explicit
// ${VAR} interpolation in YAML instead.
const (
	envHeadscaleAddress  = "HEADSCALE_ADDRESS"
	envHeadscaleAPIKey   = "HEADSCALE_API_KEY"
	envHeadscaleInsecure = "HEADSCALE_INSECURE"
)

// Client is the small surface the source needs from a Headscale gRPC client.
// Mirrors the shape used in tailscale-exporter so the same mock pattern works.
type Client interface {
	ListNodes(ctx context.Context) ([]*headscalev1.Node, error)
}

type Source struct {
	name           string
	address        string
	urlScheme      string
	tags           []string
	deviceTags     []string
	includeDevices bool
	client         Client
	conn           *grpc.ClientConn
}

// grpcClient wraps the generated HeadscaleService client and injects the
// bearer API key on every outgoing request.
type grpcClient struct {
	client headscalev1.HeadscaleServiceClient
	apiKey string
}

func newGRPCClient(client headscalev1.HeadscaleServiceClient, apiKey string) Client {
	return &grpcClient{client: client, apiKey: apiKey}
}

func (c *grpcClient) ctxWithAuth(ctx context.Context) context.Context {
	if c.apiKey == "" {
		return ctx
	}
	return grpcmetadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+c.apiKey)
}

func (c *grpcClient) ListNodes(ctx context.Context) ([]*headscalev1.Node, error) {
	ctx = c.ctxWithAuth(ctx)
	resp, err := c.client.ListNodes(ctx, &headscalev1.ListNodesRequest{})
	if err != nil {
		return nil, err
	}
	return resp.GetNodes(), nil
}

func New(cfg config.SourceConfig) (Source, error) {
	name := cfg.Name
	if name == "" {
		name = compass.SourceTypeHeadscale
	}
	headscaleCfg := cfg.Headscale
	if headscaleCfg.Address == "" {
		headscaleCfg.Address = strings.TrimSpace(os.Getenv(envHeadscaleAddress))
	}
	if headscaleCfg.APIKey == "" {
		headscaleCfg.APIKey = strings.TrimSpace(os.Getenv(envHeadscaleAPIKey))
	}
	if headscaleCfg.Insecure == nil {
		headscaleCfg.Insecure = parseEnvBool(os.Getenv(envHeadscaleInsecure))
	}

	if headscaleCfg.Address == "" {
		return Source{}, errors.New("headscale address is required")
	}
	if headscaleCfg.APIKey == "" {
		return Source{}, errors.New("headscale API key is required")
	}

	var transportCreds credentials.TransportCredentials
	if meta.DefaultBool(headscaleCfg.Insecure, false) {
		transportCreds = insecure.NewCredentials()
	} else {
		transportCreds = credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
	}

	conn, err := grpc.NewClient(headscaleCfg.Address, grpc.WithTransportCredentials(transportCreds))
	if err != nil {
		return Source{}, fmt.Errorf("source %s: dial: %w", name, err)
	}

	headscaleCfg.Tags = meta.MergeTags(cfg.Tags, headscaleCfg.Tags)

	source := newWithClient(
		name,
		headscaleCfg,
		newGRPCClient(headscalev1.NewHeadscaleServiceClient(conn), headscaleCfg.APIKey),
	)
	source.conn = conn
	return source, nil
}

// parseEnvBool returns nil for empty/unparseable input so callers can keep
// using *bool semantics (nil = "not set, fall through to default").
func parseEnvBool(value string) *bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	switch strings.ToLower(value) {
	case "1", "t", "true", "yes", "on":
		v := true
		return &v
	case "0", "f", "false", "no", "off":
		v := false
		return &v
	}
	return nil
}

func newWithClient(name string, cfg config.HeadscaleConfig, client Client) Source {
	if cfg.URLScheme == "" {
		cfg.URLScheme = "http"
	}
	if len(cfg.Tags) == 0 {
		cfg.Tags = []string{compass.SourceTypeHeadscale}
	}

	return Source{
		name:           name,
		address:        cfg.Address,
		urlScheme:      cfg.URLScheme,
		tags:           cfg.Tags,
		deviceTags:     cfg.DeviceTags,
		includeDevices: meta.DefaultBool(cfg.IncludeDevices, true),
		client:         client,
	}
}

func (s Source) Name() string {
	return s.name
}

func (s Source) Type() string {
	return compass.SourceTypeHeadscale
}

func (s Source) LogAttributes() []slog.Attr {
	return []slog.Attr{slog.String("address", s.address)}
}

// Close releases the underlying gRPC connection. Safe to call when the
// source was constructed via newWithClient (no conn).
func (s Source) Close() error {
	if s.conn == nil {
		return nil
	}
	return s.conn.Close()
}

func (s Source) Load(ctx context.Context) ([]compass.Service, error) {
	if !s.includeDevices {
		return nil, nil
	}

	nodes, err := s.client.ListNodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("source %s: list nodes: %w", s.name, err)
	}

	services := make([]compass.Service, 0, len(nodes))
	for _, node := range nodes {
		nodeURL := s.nodeURL(node)
		if nodeURL == "" {
			continue
		}
		metadata := map[string]any{
			"id":              strconv.FormatUint(node.GetId(), 10),
			"name":            node.GetName(),
			"given_name":      node.GetGivenName(),
			"addresses":       node.GetIpAddresses(),
			"online":          node.GetOnline(),
			"register_method": node.GetRegisterMethod().String(),
			"tags":            node.GetTags(),
		}
		if user := node.GetUser(); user != nil {
			metadata["user"] = user.GetName()
		}
		if ts := node.GetLastSeen(); ts != nil {
			metadata["last_seen"] = ts.AsTime()
		}
		if ts := node.GetCreatedAt(); ts != nil {
			metadata["created_at"] = ts.AsTime()
		}
		if ts := node.GetExpiry(); ts != nil && ts.AsTime().Unix() > 0 {
			metadata["expiry"] = ts.AsTime()
		}
		if routes := node.GetAvailableRoutes(); len(routes) > 0 {
			metadata["available_routes"] = routes
		}
		if routes := node.GetApprovedRoutes(); len(routes) > 0 {
			metadata["approved_routes"] = routes
		}
		if routes := node.GetSubnetRoutes(); len(routes) > 0 {
			metadata["subnet_routes"] = routes
		}

		services = append(services, compass.Service{
			ID:       "headscale/node/" + strconv.FormatUint(node.GetId(), 10),
			Name:     nodeName(node),
			URL:      nodeURL,
			Source:   s.name,
			Tags:     s.tagsForNode(node),
			Metadata: metadata,
		})
	}

	return services, nil
}

func (s Source) nodeURL(node *headscalev1.Node) string {
	addrs := node.GetIpAddresses()
	if len(addrs) > 0 {
		return meta.WithScheme(addrs[0], s.urlScheme)
	}
	if name := node.GetGivenName(); name != "" {
		return meta.WithScheme(name, s.urlScheme)
	}
	return ""
}

func (s Source) tagsForNode(node *headscalev1.Node) []string {
	var nodeTags []string
	for _, tag := range node.GetTags() {
		nodeTags = append(nodeTags, strings.TrimPrefix(tag, "tag:"))
	}
	if !node.GetOnline() {
		nodeTags = append(nodeTags, "offline")
	}
	return meta.MergeTags(meta.MergeTags(s.tags, s.deviceTags), nodeTags)
}

func nodeName(node *headscalev1.Node) string {
	if name := node.GetGivenName(); name != "" {
		return name
	}
	if name, _, ok := strings.Cut(node.GetName(), "."); ok && name != "" {
		return name
	}
	if name := node.GetName(); name != "" {
		return name
	}
	if addrs := node.GetIpAddresses(); len(addrs) > 0 {
		return addrs[0]
	}
	return strconv.FormatUint(node.GetId(), 10)
}
