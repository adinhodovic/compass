// Package status fetches up/down checks from an external monitoring
// system. Every provider is a JSON endpoint walked with gjson paths; the
// prometheus and gatus config types are presets expanded in
// internal/config, so this package only knows the generic form. Matching
// checks to services is the registry's job.
package status

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/adinhodovic/compass/internal/config"
	"github.com/adinhodovic/compass/internal/source/meta"
	"github.com/tidwall/gjson"
)

const maxResponseBytes = 10 << 20

// Check is one result from a provider: the raw value to match a service
// against (per the provider's MatchOn) and whether it is up.
type Check struct {
	Match string
	Up    bool
}

type Provider struct {
	name     string
	typ      string
	endpoint string
	headers  map[string]string
	interval time.Duration
	matchOn  string
	mapping  config.StatusMappingConfig
	client   *http.Client
}

// New builds a provider from a validated, defaulted config entry.
func New(cfg config.StatusConfig, client *http.Client) *Provider {
	if client == nil {
		client = http.DefaultClient
	}
	// config.validate already rejected non-positive or unparsable intervals.
	interval, _ := time.ParseDuration(cfg.Interval)
	return &Provider{
		name:     cfg.Name,
		typ:      cfg.Type,
		endpoint: cfg.Endpoint,
		headers:  cfg.Headers,
		interval: interval,
		matchOn:  cfg.MatchOn,
		mapping:  cfg.Mapping,
		client:   client,
	}
}

func (p *Provider) Name() string            { return p.name }
func (p *Provider) Type() string            { return p.typ }
func (p *Provider) Interval() time.Duration { return p.interval }
func (p *Provider) MatchOn() string         { return p.matchOn }

// Fetch requests the endpoint and maps each item to a Check. Items with no
// match value, or whose up value is missing or not interpretable, are
// skipped rather than reported as down.
func (p *Provider) Fetch(ctx context.Context) ([]Check, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("status %s: build request: %w", p.name, err)
	}
	for k, v := range p.headers {
		req.Header.Set(k, v)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("status %s: request: %w", p.name, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("status %s: request: status %d", p.name, resp.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("status %s: read response: %w", p.name, err)
	}
	if len(payload) > maxResponseBytes {
		return nil, fmt.Errorf(
			"status %s: read response: response exceeds %d bytes",
			p.name,
			maxResponseBytes,
		)
	}
	items, err := meta.JSONItems(payload, p.mapping.ItemsPath, p.mapping.ItemsMode)
	if err != nil {
		return nil, fmt.Errorf("status %s: map response: %w", p.name, err)
	}
	checks := make([]Check, 0, len(items))
	for _, item := range items {
		match := item.Get(meta.JSONPath(p.mapping.Match))
		if !match.Exists() || match.IsArray() || match.IsObject() ||
			strings.TrimSpace(match.String()) == "" {
			continue
		}
		up, ok := p.up(item.Get(meta.JSONPath(p.mapping.Up)))
		if !ok {
			continue
		}
		checks = append(checks, Check{Match: match.String(), Up: up})
	}
	return checks, nil
}

func (p *Provider) up(value gjson.Result) (up, ok bool) {
	if !value.Exists() {
		return false, false
	}
	if p.mapping.UpValue != "" {
		return value.String() == p.mapping.UpValue, true
	}
	switch value.Type {
	case gjson.True:
		return true, true
	case gjson.False:
		return false, true
	default:
		return false, false
	}
}
