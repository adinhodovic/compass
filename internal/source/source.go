package source

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/adinhodovic/compass/internal/compass"
	"github.com/adinhodovic/compass/internal/config"
	apisource "github.com/adinhodovic/compass/internal/source/api"
	dockersource "github.com/adinhodovic/compass/internal/source/docker"
	headscalesource "github.com/adinhodovic/compass/internal/source/headscale"
	kubernetessource "github.com/adinhodovic/compass/internal/source/kubernetes"
	staticsource "github.com/adinhodovic/compass/internal/source/static"
	tailscalesource "github.com/adinhodovic/compass/internal/source/tailscale"
)

type Source interface {
	Name() string
	Type() string
	Load(ctx context.Context) ([]compass.Service, error)
}

// LogAttributer is an optional source capability for adding safe,
// source-specific context to lifecycle logs.
type LogAttributer interface {
	LogAttributes() []slog.Attr
}

// Closer is an optional source capability for implementations that hold
// resources beyond a single Load call.
type Closer interface {
	Close() error
}

var (
	_ Source        = apisource.Source{}
	_ Source        = dockersource.Source{}
	_ LogAttributer = dockersource.Source{}
	_ Source        = headscalesource.Source{}
	_ LogAttributer = headscalesource.Source{}
	_ Closer        = headscalesource.Source{}
	_ Source        = kubernetessource.Source{}
	_ LogAttributer = kubernetessource.Source{}
	_ Source        = staticsource.Source{}
	_ Source        = tailscalesource.Source{}
	_ LogAttributer = tailscalesource.Source{}
)

// Entry pairs a Source with its refresh interval. The registry uses this to
// run periodic re-discovery in the background.
//
// RefreshInterval == 0 means "do not refresh after the initial load".
type Entry struct {
	Source          Source
	RefreshInterval time.Duration
}

// DefaultRefreshInterval is used for any source that did not configure
// `refresh_interval`. Picked to balance "feels live" with not hammering
// upstream APIs.
const DefaultRefreshInterval = 5 * time.Minute

func BuildSources(cfg config.Config, client *http.Client) ([]Entry, error) {
	entries := make([]Entry, 0, len(cfg.Services.Sources))
	seen := map[string]struct{}{}
	for i, sourceConfig := range cfg.Services.Sources {
		if err := validateSourceIdentity(i, sourceConfig, seen); err != nil {
			return nil, err
		}
		var src Source
		switch sourceConfig.Type {
		case compass.SourceTypeStatic:
			src = staticsource.New(sourceConfig)
		case compass.SourceTypeAPI:
			src = apisource.New(sourceConfig, client)
		case compass.SourceTypeKubernetes:
			s, err := kubernetessource.New(sourceConfig)
			if err != nil {
				return nil, err
			}
			src = s
		case compass.SourceTypeTailscale:
			s, err := tailscalesource.New(sourceConfig)
			if err != nil {
				return nil, err
			}
			src = s
		case compass.SourceTypeHeadscale:
			s, err := headscalesource.New(sourceConfig)
			if err != nil {
				return nil, err
			}
			src = s
		case compass.SourceTypeDocker:
			s, err := dockersource.New(sourceConfig)
			if err != nil {
				return nil, err
			}
			src = s
		default:
			return nil, fmt.Errorf("unsupported source type %q", sourceConfig.Type)
		}
		interval, err := parseRefreshInterval(sourceConfig.RefreshInterval)
		if err != nil {
			return nil, fmt.Errorf("source %q: %w", sourceConfig.Name, err)
		}
		entries = append(entries, Entry{Source: src, RefreshInterval: interval})
	}

	return entries, nil
}

func validateSourceIdentity(
	i int,
	sourceConfig config.SourceConfig,
	seen map[string]struct{},
) error {
	typeName := strings.TrimSpace(sourceConfig.Type)
	name := strings.TrimSpace(sourceConfig.Name)
	if name == "" {
		return fmt.Errorf("services.sources[%d]: name is required", i)
	}
	identity := typeName + "/" + name
	if _, ok := seen[identity]; ok {
		return fmt.Errorf("services.sources[%d]: duplicate source identity %q", i, identity)
	}
	seen[identity] = struct{}{}
	return nil
}

// parseRefreshInterval interprets a yaml duration string with these rules:
//
//   - "" (unset) -> DefaultRefreshInterval (5m).
//   - "0" / "0s" -> 0 (no refresh after boot).
//   - any other Go duration ("30s", "2m", "1h") -> that value.
func parseRefreshInterval(raw string) (time.Duration, error) {
	if raw == "" {
		return DefaultRefreshInterval, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid refresh_interval %q: %w", raw, err)
	}
	if d < 0 {
		return 0, fmt.Errorf("invalid refresh_interval %q: must be non-negative", raw)
	}
	return d, nil
}
