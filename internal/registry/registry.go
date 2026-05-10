package registry

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"net/url"
	"path"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/adinhodovic/compass/internal/catalog"
	"github.com/adinhodovic/compass/internal/compass"
	"github.com/adinhodovic/compass/internal/metrics"
	"github.com/adinhodovic/compass/internal/source"
	"github.com/adinhodovic/compass/internal/source/meta"
	"github.com/gosimple/slug"
)

// defaultLoadTimeout caps how long any single source's Load may run before
// the caller cancels it. Without this a wedged source (hung Docker socket,
// dead K8s API, slow upstream JSON endpoint) would block its refresh tick
// forever, piling up goroutines as later ticks fire.
const defaultLoadTimeout = 30 * time.Second

// Registry aggregates services from multiple sources and exposes a single
// thread-safe snapshot. Load() runs the initial sync load; Watch() then
// starts background refresh loops per source. Reads via Services() return
// the current snapshot atomically.
type Registry struct {
	entries     []*entryState
	catalog     catalog.DB
	logger      *slog.Logger
	filters     Filters
	loadTimeout time.Duration

	services atomic.Pointer[[]compass.Service]
}

// Filters narrows the aggregated service list. Applied after every source
// has loaded so all sources are filtered uniformly.
type Filters struct {
	// ExcludeURLPatterns are path.Match globs tested against each service's
	// URL host. `*` spans `.` characters (it only stops at `/`), so
	// `*.findwork.dev` excludes any subdomain depth. A pattern with no glob
	// meta-characters is treated as a suffix match — `findwork.dev` matches
	// the host or any of its subdomains.
	ExcludeURLPatterns []string
	// DedupeWWW drops a service whose host is `www.X` when another service
	// already exists with host `X`.
	DedupeWWW bool
	// ExcludeWildcardHosts drops services whose URL host contains a literal
	// `*` (e.g. a Kubernetes HTTPRoute with `spec.hostnames: ["*.example.com"]`
	// surfaces `https://*.example.com`, which is not a navigable URL).
	ExcludeWildcardHosts bool
}

// Option configures a Registry at construction time.
type Option func(*Registry)

// WithFilters sets the post-aggregation filter set.
func WithFilters(f Filters) Option {
	return func(r *Registry) { r.filters = f }
}

// WithLoadTimeout overrides the per-source load timeout. Zero or negative
// disables the timeout (the parent ctx still applies). Defaults to 30s.
func WithLoadTimeout(d time.Duration) Option {
	return func(r *Registry) { r.loadTimeout = d }
}

type entryState struct {
	src      source.Source
	interval time.Duration

	// services holds this source's last successful normalized services.
	// Stored as *[]compass.Service so we can swap atomically without
	// per-read locking.
	services atomic.Pointer[[]compass.Service]
	// lastErr stores the most recent load error's message, or nil when
	// the last load succeeded. atomic.Pointer[string] avoids a mutex
	// while keeping snapshot reads lock-free.
	lastErr         atomic.Pointer[string]
	lastAttemptUnix atomic.Int64
}

// NewFromEntries constructs a Registry. Each entry pairs a source with its
// refresh interval (0 means "no automatic refresh after boot").
func NewFromEntries(
	entries []source.Entry,
	logger *slog.Logger,
	db catalog.DB,
	opts ...Option,
) *Registry {
	states := make([]*entryState, len(entries))
	for i, e := range entries {
		states[i] = &entryState{src: e.Source, interval: e.RefreshInterval}
	}
	r := &Registry{entries: states, catalog: db, logger: logger, loadTimeout: defaultLoadTimeout}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Load runs every source once, in order, and populates the snapshot. A
// failure on one source does not abort the others; the combined error is
// returned for the caller to log.
func (r *Registry) Load(ctx context.Context) ([]compass.Service, error) {
	var errs []error
	for _, e := range r.entries {
		r.logSource(ctx, slog.LevelInfo, "Starting source load", e.src)
		if err := r.refreshEntry(ctx, e); err != nil {
			errs = append(errs, fmt.Errorf("%s/%s: %w", e.src.Type(), e.src.Name(), err))
		}
	}
	return r.Services(), joinSourceErrors("source load", errs)
}

// Watch starts a background goroutine per source that refreshes on its
// configured interval. Sources with interval == 0 are skipped. Returns
// immediately; goroutines exit when ctx is cancelled.
func (r *Registry) Watch(ctx context.Context) {
	for _, e := range r.entries {
		if e.interval <= 0 {
			continue
		}
		go r.watchLoop(ctx, e)
	}
}

func (r *Registry) Close() error {
	var errs []error
	for _, e := range r.entries {
		if c, ok := e.src.(source.Closer); ok {
			if err := c.Close(); err != nil {
				errs = append(
					errs,
					fmt.Errorf("close source %s/%s: %w", e.src.Type(), e.src.Name(), err),
				)
			}
		}
	}
	return errors.Join(errs...)
}

func (r *Registry) watchLoop(ctx context.Context, e *entryState) {
	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.refreshEntry(ctx, e); err != nil {
				r.logSource(
					ctx,
					slog.LevelWarn,
					"Source refresh failed",
					e.src,
					slog.String("err", err.Error()),
				)
				continue
			}
			services := e.snapshot()
			r.logSource(
				ctx,
				slog.LevelDebug,
				"Source refresh complete",
				e.src,
				slog.Int("services", len(services)),
			)
		}
	}
}

// Refresh forces every source to reload, regardless of its interval.
// refreshEntry already aggregates after each source, so no extra call here.
func (r *Registry) Refresh(ctx context.Context) error {
	var errs []error
	for _, e := range r.entries {
		r.logSource(ctx, slog.LevelInfo, "Starting source load", e.src)
		if err := r.refreshEntry(ctx, e); err != nil {
			errs = append(errs, fmt.Errorf("%s/%s: %w", e.src.Type(), e.src.Name(), err))
		}
	}
	return joinSourceErrors("refresh", errs)
}

func joinSourceErrors(prefix string, errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("%s: %w", prefix, errors.Join(errs...))
}

// Services returns the current aggregated snapshot. Safe for concurrent use.
func (r *Registry) Services() []compass.Service {
	if p := r.services.Load(); p != nil {
		return compass.CloneServices(*p)
	}
	return nil
}

// SourceStatus is a snapshot of one source's last load attempt. Used by
// the /debug page to show per-source health.
type SourceStatus struct {
	Name string
	Type string
	// LastLoaded is the timestamp of the most recent load *attempt*, which
	// may have succeeded or failed — pair with Error to disambiguate.
	LastLoaded time.Time
	Services   int
	Error      string // empty when the last load succeeded
}

// ID returns the canonical "<type>/<name>" identifier matching
// compass.Service.SourceID, so the debug-page Groups map can be looked
// up without colliding when two sources share a Name.
func (s SourceStatus) ID() string {
	return s.Type + "/" + s.Name
}

// SourceStatuses returns the per-source state at this moment.
func (r *Registry) SourceStatuses() []SourceStatus {
	out := make([]SourceStatus, 0, len(r.entries))
	for _, e := range r.entries {
		ts := time.Time{}
		if u := e.lastAttemptUnix.Load(); u > 0 {
			ts = time.Unix(u, 0)
		}
		errStr := ""
		if p := e.lastErr.Load(); p != nil {
			errStr = *p
		}
		out = append(out, SourceStatus{
			Name:       e.src.Name(),
			Type:       e.src.Type(),
			LastLoaded: ts,
			Services:   len(e.snapshot()),
			Error:      errStr,
		})
	}
	return out
}

func (r *Registry) refreshEntry(ctx context.Context, e *entryState) error {
	if r.loadTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.loadTimeout)
		defer cancel()
	}
	start := time.Now()
	loaded, err := e.src.Load(ctx)
	e.lastAttemptUnix.Store(time.Now().Unix())
	if err != nil {
		msg := err.Error()
		e.lastErr.Store(&msg)
		metrics.ObserveSourceRefresh(sourceMetricLabel(e.src), 0, time.Since(start), err)
		return err
	}
	out := make([]compass.Service, 0, len(loaded))
	for _, service := range loaded {
		service, ok := r.normalize(service, e.src.Name(), e.src.Type())
		if ok {
			out = append(out, service)
		}
	}
	e.services.Store(&out)
	e.lastErr.Store(nil)
	metrics.ObserveSourceRefresh(sourceMetricLabel(e.src), len(out), time.Since(start), nil)
	r.logSource(ctx, slog.LevelInfo, "Source load complete", e.src, slog.Int("services", len(out)))
	r.aggregate()
	return nil
}

func sourceMetricLabel(src source.Source) string {
	return src.Type() + "/" + src.Name()
}

func (e *entryState) snapshot() []compass.Service {
	if p := e.services.Load(); p != nil {
		return compass.CloneServices(*p)
	}
	return nil
}

// aggregate snapshots every entry's last known services into the shared
// atomic pointer. Lock-free thanks to per-entry atomic.Pointer.
func (r *Registry) aggregate() []compass.Service {
	var combined []compass.Service
	for _, e := range r.entries {
		combined = append(combined, e.snapshot()...)
	}
	combined = applyFilters(combined, r.filters)
	Sort(combined)
	r.services.Store(&combined)
	return combined
}

// applyFilters drops services that match an ExcludeURLPatterns entry,
// strips wildcard-host services, then optionally collapses `www.X`
// duplicates against a sibling `X`. Hosts are parsed once up-front so the
// three filter passes don't each re-parse every URL.
func applyFilters(services []compass.Service, f Filters) []compass.Service {
	if len(services) == 0 {
		return services
	}
	if !f.ExcludeWildcardHosts && len(f.ExcludeURLPatterns) == 0 && !f.DedupeWWW {
		return services
	}

	hosts := make([]string, len(services))
	for i, svc := range services {
		hosts[i] = serviceHost(svc.URL)
	}
	drop := make([]bool, len(services))

	if f.ExcludeWildcardHosts {
		for i, h := range hosts {
			if !drop[i] && strings.Contains(h, "*") {
				drop[i] = true
			}
		}
	}

	if len(f.ExcludeURLPatterns) > 0 {
		for i, h := range hosts {
			if !drop[i] && hostMatchesAny(h, f.ExcludeURLPatterns) {
				drop[i] = true
			}
		}
	}

	if f.DedupeWWW {
		hostSet := make(map[string]bool, len(hosts))
		for i, h := range hosts {
			if !drop[i] && h != "" {
				hostSet[h] = true
			}
		}
		for i, h := range hosts {
			if !drop[i] && strings.HasPrefix(h, "www.") && hostSet[strings.TrimPrefix(h, "www.")] {
				drop[i] = true
			}
		}
	}

	out := services[:0]
	for i, svc := range services {
		if !drop[i] {
			out = append(out, svc)
		}
	}
	return out
}

// serviceHost extracts the lowercase host from a service URL. Returns "" for
// URLs that lack a host (e.g. bare paths) so they bypass host-based filters.
func serviceHost(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	if host == "" {
		// Bare hosts ("example.com/foo") parse with empty Hostname; reparse as
		// an authority so ports are stripped consistently.
		if bare, err := url.Parse("//" + rawURL); err == nil {
			host = bare.Hostname()
		}
		if host == "" {
			head := rawURL
			if i := strings.IndexAny(head, "/?#"); i >= 0 {
				head = head[:i]
			}
			if split, _, err := net.SplitHostPort(head); err == nil {
				head = split
			}
			host = head
		}
	}
	return strings.ToLower(host)
}

// hostMatchesAny returns true when host matches any of the provided globs.
// In addition to path.Match semantics it treats a bare suffix like
// `findwork.dev` as "host equals findwork.dev or ends with .findwork.dev"
// so users don't have to spell every subdomain depth explicitly.
func hostMatchesAny(host string, patterns []string) bool {
	if host == "" {
		return false
	}
	for _, p := range patterns {
		p = strings.TrimSpace(strings.ToLower(p))
		if p == "" {
			continue
		}
		if !strings.ContainsAny(p, "*?[") {
			if host == p || strings.HasSuffix(host, "."+p) {
				return true
			}
			continue
		}
		if ok, _ := path.Match(p, host); ok {
			return true
		}
	}
	return false
}

func (r *Registry) logSource(
	ctx context.Context,
	level slog.Level,
	msg string,
	src source.Source,
	extra ...slog.Attr,
) {
	if r.logger == nil {
		return
	}
	r.logger.LogAttrs(ctx, level, msg, append(sourceLogAttrs(src), extra...)...)
}

func sourceLogAttrs(src source.Source) []slog.Attr {
	attrs := []slog.Attr{
		slog.String("source", src.Name()),
		slog.String("type", src.Type()),
	}
	if attributer, ok := src.(source.LogAttributer); ok {
		attrs = append(attrs, attributer.LogAttributes()...)
	}
	return attrs
}

// Sort orders services by lower-cased name, then by source name as a
// stable tiebreaker so snapshots with name collisions across sources are
// deterministic.
func Sort(services []compass.Service) {
	slices.SortStableFunc(services, func(a, b compass.Service) int {
		if c := cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
			return c
		}
		return cmp.Compare(a.Source, b.Source)
	})
}

func Group(services []compass.Service, mode string) map[string][]compass.Service {
	groups := make(map[string][]compass.Service)
	for _, service := range services {
		key := service.SourceID()
		if mode == compass.GroupByTags {
			key = "untagged"
			if service.PrimaryTag != "" {
				key = service.PrimaryTag
			} else if len(service.Tags) > 0 {
				key = service.Tags[0]
			}
		}
		groups[key] = append(groups[key], service)
	}
	return groups
}

func GroupNames(groups map[string][]compass.Service) []string {
	return slices.Sorted(maps.Keys(groups))
}

func (r *Registry) normalize(
	service compass.Service,
	fallbackSource string,
	fallbackType string,
) (compass.Service, bool) {
	service.Name = strings.TrimSpace(service.Name)
	service.ID = strings.TrimSpace(service.ID)
	service.PrimaryTag = strings.TrimSpace(service.PrimaryTag)
	if service.Name == "" {
		return compass.Service{}, false
	}
	service.URL = meta.WithScheme(service.URL, "https")
	if normalizedURL, ok := meta.ValidHTTPURL(service.URL); ok {
		service.URL = normalizedURL
	} else {
		return compass.Service{}, false
	}
	if service.Source == "" {
		service.Source = fallbackSource
	}
	if service.SourceType == "" {
		service.SourceType = fallbackType
	}
	if service.ID == "" {
		service.ID = slug.Make(service.SourceType + "-" + service.Source + "-" + service.Name)
	}
	if service.Metadata == nil {
		service.Metadata = map[string]any{}
	}
	if r.catalog != nil {
		if entry, ok := r.catalog.Lookup(service.Name); ok {
			if service.Description == "" {
				service.Description = entry.Description
			}
			if service.Icon == "" {
				service.Icon = entry.Icon
			}
			if service.PrimaryTag == "" && len(service.Tags) == 0 {
				service.PrimaryTag = entry.PrimaryTag
			}
			if len(service.Tags) == 0 && len(entry.Tags) > 0 {
				service.Tags = append([]string(nil), entry.Tags...)
			}
		}
	}
	service.Tags = normalizePrimaryTag(service.PrimaryTag, service.Tags)
	if service.PrimaryTag == "" && len(service.Tags) > 0 {
		service.PrimaryTag = service.Tags[0]
	}
	service.Panels = validPanels(service.Panels, service)
	return service, true
}

func normalizePrimaryTag(primary string, tags []string) []string {
	primary = strings.TrimSpace(primary)
	if primary == "" {
		return tags
	}
	for _, tag := range tags {
		if tag == primary {
			out := []string{primary}
			for _, candidate := range tags {
				if candidate != primary {
					out = append(out, candidate)
				}
			}
			return out
		}
	}
	return append([]string{primary}, tags...)
}

func validPanels(panels []compass.Panel, service compass.Service) []compass.Panel {
	if len(panels) == 0 {
		return nil
	}
	out := make([]compass.Panel, 0, len(panels))
	for _, panel := range panels {
		panel.Title = strings.TrimSpace(panel.Title)
		if panel.Title == "" {
			panel.Title = "Overview"
		}
		panelURL, ok := meta.ValidHTTPURL(expandPanelURL(panel.URL, service))
		if !ok {
			continue
		}
		panel.URL = panelURL
		out = append(out, panel)
	}
	return out
}

func expandPanelURL(raw string, service compass.Service) string {
	raw = strings.ReplaceAll(raw, "{{service.id}}", url.QueryEscape(service.ID))
	raw = strings.ReplaceAll(raw, "{{service.name}}", url.QueryEscape(service.Name))
	raw = strings.ReplaceAll(raw, "{{service.type}}", url.QueryEscape(service.SourceType))
	raw = strings.ReplaceAll(raw, "{{service.url}}", url.QueryEscape(service.URL))
	return raw
}
