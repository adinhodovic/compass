// Package netbird reads peers from a NetBird management server's REST API
// (NetBird Cloud or self-hosted) and emits a compass.Service for each peer
// that has an IP or DNS label.
package netbird

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/adinhodovic/compass/internal/compass"
	"github.com/adinhodovic/compass/internal/config"
	"github.com/adinhodovic/compass/internal/source/meta"
)

// Env vars read only when the matching YAML field is empty, so an explicit
// value always wins. Configs with several NetBird sources use ${VAR}
// interpolation per source instead.
const (
	envNetbirdAPIURL   = "NETBIRD_API_URL"
	envNetbirdAPIToken = "NETBIRD_API_TOKEN"
)

// defaultAPIURL is NetBird Cloud's management API, used when neither api_url
// nor NETBIRD_API_URL is set.
const defaultAPIURL = "https://api.netbird.io"

// allGroupName is the built-in group every peer belongs to. Tagging every peer
// with it adds nothing, so it is skipped when mapping groups to tags.
const allGroupName = "All"

const maxResponseBytes = 10 << 20

type Source struct {
	name           string
	apiURL         string
	apiToken       string
	urlScheme      string
	tags           []string
	deviceTags     []string
	includeDevices bool
	client         *http.Client
}

// peer is the subset of NetBird's Peer schema the source maps. Location
// (connection_ip, city, country) and hardware serials are deliberately left
// out so they never reach metadata.
type peer struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Hostname         string    `json:"hostname"`
	IP               string    `json:"ip"`
	IPv6             string    `json:"ipv6"`
	DNSLabel         string    `json:"dns_label"`
	ExtraDNSLabels   []string  `json:"extra_dns_labels"`
	Connected        bool      `json:"connected"`
	LastSeen         time.Time `json:"last_seen"`
	CreatedAt        time.Time `json:"created_at"`
	OS               string    `json:"os"`
	Version          string    `json:"version"`
	SSHEnabled       bool      `json:"ssh_enabled"`
	ApprovalRequired bool      `json:"approval_required"`
	LoginExpired     bool      `json:"login_expired"`
	Groups           []struct {
		Name string `json:"name"`
	} `json:"groups"`
}

func New(cfg config.SourceConfig, client *http.Client) (Source, error) {
	name := cfg.Name
	if name == "" {
		name = compass.SourceTypeNetbird
	}
	netbirdCfg := cfg.Netbird
	if netbirdCfg.APIURL == "" {
		netbirdCfg.APIURL = strings.TrimSpace(os.Getenv(envNetbirdAPIURL))
	}
	if netbirdCfg.APIToken == "" {
		netbirdCfg.APIToken = strings.TrimSpace(os.Getenv(envNetbirdAPIToken))
	}
	if netbirdCfg.APIURL == "" {
		netbirdCfg.APIURL = defaultAPIURL
	}
	if _, ok := meta.ValidHTTPURL(netbirdCfg.APIURL); !ok {
		return Source{}, fmt.Errorf("netbird api_url %q must be an http(s) URL", netbirdCfg.APIURL)
	}
	if netbirdCfg.APIToken == "" {
		return Source{}, errors.New("netbird API token is required")
	}
	if client == nil {
		client = http.DefaultClient
	}

	netbirdCfg.Tags = meta.MergeTags(cfg.Tags, netbirdCfg.Tags)
	return newWithClient(name, netbirdCfg, client), nil
}

func newWithClient(name string, cfg config.NetbirdConfig, client *http.Client) Source {
	if cfg.URLScheme == "" {
		cfg.URLScheme = "http"
	}
	if len(cfg.Tags) == 0 {
		cfg.Tags = []string{compass.SourceTypeNetbird}
	}

	return Source{
		name:           name,
		apiURL:         strings.TrimRight(cfg.APIURL, "/"),
		apiToken:       cfg.APIToken,
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
	return compass.SourceTypeNetbird
}

func (s Source) LogAttributes() []slog.Attr {
	return []slog.Attr{slog.String("api_url", s.apiURL)}
}

func (s Source) Load(ctx context.Context) ([]compass.Service, error) {
	if !s.includeDevices {
		return nil, nil
	}

	peers, err := s.listPeers(ctx)
	if err != nil {
		return nil, fmt.Errorf("source %s: list peers: %w", s.name, err)
	}

	services := make([]compass.Service, 0, len(peers))
	for _, p := range peers {
		peerURL := s.peerURL(p)
		if peerURL == "" {
			continue
		}
		metadata := map[string]any{
			"id":          p.ID,
			"name":        p.Name,
			"hostname":    p.Hostname,
			"ip":          p.IP,
			"dns_label":   p.DNSLabel,
			"connected":   p.Connected,
			"os":          p.OS,
			"version":     p.Version,
			"ssh_enabled": p.SSHEnabled,
			"groups":      groupNames(p),
		}
		if p.IPv6 != "" {
			metadata["ipv6"] = p.IPv6
		}
		if len(p.ExtraDNSLabels) > 0 {
			metadata["extra_dns_labels"] = p.ExtraDNSLabels
		}
		if !p.LastSeen.IsZero() {
			metadata["last_seen"] = p.LastSeen
		}
		if !p.CreatedAt.IsZero() {
			metadata["created_at"] = p.CreatedAt
		}
		if p.ApprovalRequired {
			metadata["approval_required"] = true
		}
		if p.LoginExpired {
			metadata["login_expired"] = true
		}

		services = append(services, compass.Service{
			ID:       "netbird/peer/" + p.ID,
			Name:     peerName(p),
			URL:      peerURL,
			Source:   s.name,
			Tags:     s.tagsForPeer(p),
			Metadata: metadata,
		})
	}

	return services, nil
}

func (s Source) listPeers(ctx context.Context) ([]peer, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.apiURL+"/api/peers", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Token "+s.apiToken)
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(payload) > maxResponseBytes {
		return nil, fmt.Errorf("response exceeds %d bytes", maxResponseBytes)
	}
	var peers []peer
	if err := json.Unmarshal(payload, &peers); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return peers, nil
}

func (s Source) peerURL(p peer) string {
	if p.IP != "" {
		return meta.WithScheme(p.IP, s.urlScheme)
	}
	if p.DNSLabel != "" {
		return meta.WithScheme(p.DNSLabel, s.urlScheme)
	}
	return ""
}

func (s Source) tagsForPeer(p peer) []string {
	var peerTags []string
	for _, group := range groupNames(p) {
		if group != allGroupName {
			peerTags = append(peerTags, group)
		}
	}
	if !p.Connected {
		peerTags = append(peerTags, "offline")
	}
	return meta.MergeTags(meta.MergeTags(s.tags, s.deviceTags), peerTags)
}

func groupNames(p peer) []string {
	names := make([]string, 0, len(p.Groups))
	for _, group := range p.Groups {
		names = append(names, group.Name)
	}
	return names
}

func peerName(p peer) string {
	if p.Name != "" {
		return p.Name
	}
	if label, _, _ := strings.Cut(p.DNSLabel, "."); label != "" {
		return label
	}
	if p.Hostname != "" {
		return p.Hostname
	}
	if p.IP != "" {
		return p.IP
	}
	return p.ID
}
