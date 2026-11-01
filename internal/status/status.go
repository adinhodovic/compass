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

// Check is one result from a provider: the URL, host, or name of the
// service it is about, and whether it is up.
type Check struct {
	Service string
	Up      bool
}

type Provider struct {
	name        string
	typ         string
	endpoint    string
	headers     map[string]string
	interval    time.Duration
	itemsPath   string
	servicePath string
	upPath      string
	client      *http.Client
}

// New builds a provider from a validated, defaulted config entry.
func New(cfg config.StatusConfig, client *http.Client) *Provider {
	if client == nil {
		client = http.DefaultClient
	}
	// config.validate already rejected non-positive or unparsable intervals.
	interval, _ := time.ParseDuration(cfg.Interval)
	return &Provider{
		name:        cfg.Name,
		typ:         cfg.Type,
		endpoint:    cfg.Endpoint,
		headers:     cfg.Headers,
		interval:    interval,
		itemsPath:   cfg.Items,
		servicePath: cfg.Service,
		upPath:      cfg.Up,
		client:      client,
	}
}

func (p *Provider) Name() string            { return p.name }
func (p *Provider) Type() string            { return p.typ }
func (p *Provider) Interval() time.Duration { return p.interval }

// Fetch requests the endpoint and maps each item to a Check. Items with no
// service value, or whose up value is missing or composite, are skipped
// rather than reported as down.
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
	items, err := meta.JSONItems(payload, p.itemsPath, "")
	if err != nil {
		return nil, fmt.Errorf("status %s: map response: %w", p.name, err)
	}
	checks := make([]Check, 0, len(items))
	for _, item := range items {
		service := item.Get(meta.JSONPath(p.servicePath))
		if !service.Exists() || service.IsArray() || service.IsObject() ||
			strings.TrimSpace(service.String()) == "" {
			continue
		}
		up, ok := isUp(item.Get(meta.JSONPath(p.upPath)))
		if !ok {
			continue
		}
		checks = append(checks, Check{Service: service.String(), Up: up})
	}
	return checks, nil
}

// isUp interprets a check result by convention: true, a non-zero number,
// or one of upWords (any case) is up; any other scalar is down. Missing,
// null, and composite values are not a result at all.
func isUp(value gjson.Result) (up, ok bool) {
	switch value.Type {
	case gjson.True:
		return true, true
	case gjson.False:
		return false, true
	case gjson.Number:
		return value.Num != 0, true
	case gjson.String:
		_, up := upWords[strings.ToLower(strings.TrimSpace(value.Str))]
		return up, true
	default:
		return false, false
	}
}

var upWords = map[string]struct{}{
	"1": {}, "true": {}, "up": {}, "ok": {}, "healthy": {},
}
