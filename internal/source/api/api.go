// Package api implements the generic JSON-over-HTTP source. It hits a
// configurable endpoint, walks the response with gjson paths from
// `mapping.fields`, and emits one compass.Service per item.
package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/adinhodovic/compass/internal/compass"
	"github.com/adinhodovic/compass/internal/config"
	"github.com/adinhodovic/compass/internal/source/meta"
	"github.com/tidwall/gjson"
)

const maxResponseBytes = 10 << 20

type Source struct {
	name     string
	endpoint string
	headers  map[string]string
	tags     []string
	mapping  config.MappingConfig
	client   *http.Client
}

func New(cfg config.SourceConfig, client *http.Client) Source {
	name := cfg.Name
	if name == "" {
		name = compass.SourceTypeAPI
	}
	if client == nil {
		client = http.DefaultClient
	}

	return Source{
		name:     name,
		endpoint: cfg.Endpoint,
		headers:  cfg.Headers,
		tags:     cfg.Tags,
		mapping:  cfg.Mapping,
		client:   client,
	}
}

func (a Source) Name() string {
	return a.name
}

func (a Source) Type() string {
	return compass.SourceTypeAPI
}

func (a Source) Load(ctx context.Context) ([]compass.Service, error) {
	if a.endpoint == "" {
		return nil, fmt.Errorf("source %s: endpoint is required", a.name)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("source %s: build request: %w", a.name, err)
	}
	for k, v := range a.headers {
		req.Header.Set(k, v)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("source %s: request %s: %w", a.name, a.endpoint, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("source %s: status %d from %s", a.name, resp.StatusCode, a.endpoint)
	}

	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("source %s: read response: %w", a.name, err)
	}
	if len(payload) > maxResponseBytes {
		return nil, fmt.Errorf(
			"source %s: read response: response exceeds %d bytes",
			a.name,
			maxResponseBytes,
		)
	}

	itemsValue, err := meta.JSONItems(payload, a.mapping.ItemsPath, a.mapping.ItemsMode)
	if err != nil {
		return nil, fmt.Errorf("source %s: map response: %w", a.name, err)
	}

	services := make([]compass.Service, 0, len(itemsValue))
	for _, item := range itemsValue {
		service := compass.Service{Source: a.name}
		service.Name = stringFromPath(item, a.mapping.Fields["name"])
		service.URL = meta.WithScheme(
			stringFromPath(item, a.mapping.Fields["url"]),
			a.mapping.URLScheme,
		)
		service.ID = stringFromPath(item, a.mapping.Fields["id"])
		service.PrimaryTag = stringFromPath(item, a.mapping.Fields["primary_tag"])
		service.Description = stringFromPath(item, a.mapping.Fields["description"])
		service.Icon = stringFromPath(item, a.mapping.Fields["icon"])
		service.Tags = meta.MergeTags(
			a.tags,
			stringSliceFromPath(item, a.mapping.Fields["tags"]),
		)
		services = append(services, service)
	}

	return services, nil
}

func stringFromPath(item gjson.Result, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	value := item.Get(meta.JSONPath(path))
	if !value.Exists() {
		return ""
	}
	if value.IsArray() || value.IsObject() {
		return ""
	}
	return value.String()
}

func stringSliceFromPath(item gjson.Result, path string) []string {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	value := item.Get(meta.JSONPath(path))
	if !value.Exists() {
		return nil
	}
	if value.IsArray() {
		items := value.Array()
		out := make([]string, 0, len(items))
		for _, item := range items {
			if !item.IsArray() && !item.IsObject() {
				out = append(out, item.String())
			}
		}
		return meta.MergeTags(nil, out)
	}
	if value.IsObject() {
		return nil
	}
	return meta.CommaList(value.String())
}
