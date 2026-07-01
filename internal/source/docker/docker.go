package docker

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/adinhodovic/compass/internal/compass"
	"github.com/adinhodovic/compass/internal/config"
	"github.com/adinhodovic/compass/internal/source/meta"
	dockercontainer "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

const (
	defaultSocket = "/var/run/docker.sock"

	composeProjectLabel = "com.docker.compose.project"
	composeServiceLabel = "com.docker.compose.service"
)

type Source struct {
	name            string
	host            string
	tags            []string
	autoDiscoverAll bool
	includeStopped  bool
	urlScheme       string
	client          dockerClient
}

type dockerClient interface {
	ContainerList(
		ctx context.Context,
		options client.ContainerListOptions,
	) (client.ContainerListResult, error)
}

func New(cfg config.SourceConfig) (Source, error) {
	name := cfg.Name
	if name == "" {
		name = compass.SourceTypeDocker
	}

	dockerCfg := cfg.Docker
	if dockerCfg.Host == "" {
		dockerCfg.Host = strings.TrimSpace(os.Getenv("DOCKER_HOST"))
	}
	if dockerCfg.Host == "" {
		dockerCfg.Host = defaultSocket
	}

	dockerHost, err := normalizeHost(dockerCfg.Host)
	if err != nil {
		return Source{}, err
	}
	cli, err := client.NewClientWithOpts(
		client.WithHost(dockerHost),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return Source{}, fmt.Errorf("source %s: docker client: %w", name, err)
	}

	scheme := strings.TrimSpace(dockerCfg.URLScheme)
	if scheme == "" {
		scheme = "https"
	}

	return Source{
		name:            name,
		host:            dockerHost,
		tags:            dockerTags(cfg.Tags, dockerCfg.Tags),
		autoDiscoverAll: meta.DefaultBool(dockerCfg.AutoDiscoverAll, true),
		includeStopped:  dockerCfg.IncludeStopped,
		urlScheme:       scheme,
		client:          cli,
	}, nil
}

func (s Source) Name() string { return s.name }

func (s Source) Type() string { return compass.SourceTypeDocker }

func (s Source) LogAttributes() []slog.Attr {
	return []slog.Attr{slog.String("host", s.host)}
}

func (s Source) Load(ctx context.Context) ([]compass.Service, error) {
	containers, err := s.listContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("source %s: list containers: %w", s.name, err)
	}

	services := make([]compass.Service, 0, len(containers))
	for _, c := range containers {
		if !containerEnabled(c, s.autoDiscoverAll) {
			continue
		}
		entries := s.containerEntries(c)
		multi := len(entries) > 1
		for _, entry := range entries {
			services = append(services, s.toService(c, entry, multi))
		}
	}

	return services, nil
}

func (s Source) listContainers(ctx context.Context) ([]dockercontainer.Summary, error) {
	result, err := s.client.ContainerList(ctx, client.ContainerListOptions{All: s.includeStopped})
	if err != nil {
		return nil, err
	}
	return result.Items, nil
}

func (s Source) toService(
	c dockercontainer.Summary,
	entry meta.URLEntry,
	multi bool,
) compass.Service {
	labels := c.Labels
	if labels == nil {
		labels = map[string]string{}
	}

	id := "docker/" + c.ID
	name := containerName(c)
	if multi {
		// Multi-URL fan-out from a single container needs distinct IDs and
		// names. Title (when set) takes both; otherwise URL host is the
		// disambiguator.
		suffix := entry.Title
		if u, err := url.Parse(entry.URL); err == nil && u.Host != "" {
			suffix = u.Host
		}
		id += "/" + suffix
		if entry.Title != "" {
			name = entry.Title
		} else {
			name = name + " · " + suffix
		}
	} else if entry.Title != "" {
		name = entry.Title
	}

	return compass.Service{
		ID:          id,
		Name:        name,
		URL:         entry.URL,
		Source:      s.name,
		PrimaryTag:  labels[meta.AnnotationPrimaryTag],
		Tags:        s.containerTags(c),
		Description: labels[meta.AnnotationDescription],
		Icon:        labels[meta.AnnotationIcon],
		Metadata: map[string]any{
			"id":              c.ID,
			"image":           c.Image,
			"state":           c.State,
			"status":          c.Status,
			"compose_project": labels[composeProjectLabel],
			"compose_service": labels[composeServiceLabel],
		},
		Links:  meta.LinksFromAnnotations(labels),
		Panels: meta.PanelsFromAnnotations(labels),
	}
}

func (s Source) containerTags(c dockercontainer.Summary) []string {
	tags := meta.MergeTags(s.tags, meta.CommaList(c.Labels[meta.AnnotationTags]))
	if project := c.Labels[composeProjectLabel]; project != "" {
		tags = meta.MergeTags(tags, []string{project})
	}
	return tags
}

func containerEnabled(c dockercontainer.Summary, autoDiscoverAll bool) bool {
	return meta.EnabledByLabel(c.Labels, autoDiscoverAll)
}

func containerName(c dockercontainer.Summary) string {
	if name := c.Labels[meta.AnnotationName]; name != "" {
		return name
	}
	if service := c.Labels[composeServiceLabel]; service != "" {
		return service
	}
	if len(c.Names) > 0 {
		return strings.TrimPrefix(c.Names[0], "/")
	}
	if len(c.ID) > 12 {
		return c.ID[:12]
	}
	return c.ID
}

// traefikRouterRuleLabel matches Traefik's per-router rule label, e.g.
// `traefik.http.routers.whoami.rule`.
var traefikRouterRuleLabel = regexp.MustCompile(`^traefik\.http\.routers\.[^.]+\.rule$`)

var (
	traefikHostClause = regexp.MustCompile(`Host\(([^)]*)\)`)
	traefikHostValue  = regexp.MustCompile("`([^`]+)`")
)

// containerEntries resolves the URL entries for a container. The compass
// urls label wins if present and supports multi-card fan-out; otherwise
// Traefik `Host(...)` router rules fan out into one URL per hostname. Returns
// nil when nothing is usable.
func (s Source) containerEntries(c dockercontainer.Summary) []meta.URLEntry {
	if entries := meta.URLEntriesFromAnnotation(c.Labels[meta.AnnotationURLs]); len(entries) > 0 {
		return entries
	}
	hosts := traefikHosts(c.Labels)
	if len(hosts) == 0 {
		return nil
	}
	entries := make([]meta.URLEntry, 0, len(hosts))
	for _, host := range hosts {
		entries = append(entries, meta.URLEntry{URL: s.urlScheme + "://" + host})
	}
	return entries
}

func traefikHosts(labels map[string]string) []string {
	rules := make([]string, 0)
	for k := range labels {
		if traefikRouterRuleLabel.MatchString(k) {
			rules = append(rules, k)
		}
	}
	sort.Strings(rules)

	seen := map[string]bool{}
	hosts := make([]string, 0, len(rules))
	for _, key := range rules {
		for _, clause := range traefikHostClause.FindAllStringSubmatch(labels[key], -1) {
			for _, match := range traefikHostValue.FindAllStringSubmatch(clause[1], -1) {
				host := strings.TrimSpace(match[1])
				if host != "" && !seen[host] {
					seen[host] = true
					hosts = append(hosts, host)
				}
			}
		}
	}
	return hosts
}

func dockerTags(top, configured []string) []string {
	merged := meta.MergeTags(top, configured)
	if len(merged) == 0 {
		return []string{compass.SourceTypeDocker}
	}
	return merged
}

// normalizeHost returns a Docker SDK host. Accepts unix:///path, a bare Unix
// socket path, tcp://host:port, and http(s)://host[:port].
func normalizeHost(host string) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", fmt.Errorf("docker host is empty")
	}
	if strings.HasPrefix(host, "/") {
		return "unix://" + host, nil
	}
	u, err := url.Parse(host)
	if err != nil {
		return "", fmt.Errorf("parse docker host: %w", err)
	}
	switch u.Scheme {
	case "unix":
		if u.Path == "" {
			return "", fmt.Errorf("docker unix host path is empty")
		}
		return host, nil
	case "tcp", "http", "https":
		if u.Host == "" {
			return "", fmt.Errorf("docker host address is empty")
		}
		return host, nil
	default:
		return "", fmt.Errorf("unsupported docker host scheme %q", u.Scheme)
	}
}
