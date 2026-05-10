// Package static implements the trivial source whose services are listed
// directly in compass.yaml. Useful for one-off bookmarks, things outside the
// dynamic discovery surface (Tailscale/k8s/Docker/API), or as a fallback
// while a real source is being set up.
package static

import (
	"context"
	"log/slog"

	"github.com/adinhodovic/compass/internal/compass"
	"github.com/adinhodovic/compass/internal/config"
	"github.com/adinhodovic/compass/internal/source/meta"
)

type Source struct {
	name     string
	tags     []string
	services []compass.Service
}

func New(cfg config.SourceConfig) Source {
	name := cfg.Name
	if name == "" {
		name = compass.SourceTypeStatic
	}
	for _, svc := range cfg.Services {
		if _, ok := meta.ValidHTTPURL(meta.WithScheme(svc.URL, "https")); !ok {
			slog.Warn("static service has invalid URL; will be dropped during normalization",
				"source", name, "name", svc.Name, "url", svc.URL)
		}
	}
	return Source{name: name, tags: cfg.Tags, services: cfg.Services}
}

func (s Source) Name() string {
	return s.name
}

func (s Source) Type() string {
	return compass.SourceTypeStatic
}

func (s Source) Load(_ context.Context) ([]compass.Service, error) {
	services := compass.CloneServices(s.services)
	for i := range services {
		services[i].Source = s.name
		services[i].Tags = meta.MergeTags(s.tags, services[i].Tags)
	}

	return services, nil
}
