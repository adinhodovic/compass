package registry

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/adinhodovic/compass/internal/compass"
	"github.com/adinhodovic/compass/internal/metrics"
	"github.com/adinhodovic/compass/internal/status"
)

// WithStatusProviders attaches status providers. Their checks are matched
// to services on every Services() read, in provider order; the first
// provider with a match wins.
func WithStatusProviders(providers ...*status.Provider) Option {
	return func(r *Registry) {
		for _, p := range providers {
			r.statuses = append(r.statuses, &statusState{provider: p})
		}
	}
}

type statusState struct {
	provider *status.Provider
	// snapshot is nil until the first successful fetch. A failed fetch
	// keeps the previous snapshot so a flaky provider doesn't blank every
	// status dot.
	snapshot        atomic.Pointer[statusSnapshot]
	lastErr         atomic.Pointer[string]
	lastAttemptUnix atomic.Int64
}

type statusSnapshot struct {
	// up is keyed by statusKey of each check's service value. Duplicate
	// keys resolve to down if any is down.
	up        map[statusKey]bool
	checks    int
	checkedAt time.Time
}

// statusKey is a normalized service identifier. A check's service value
// is stored under its keys (see checkKeys); a compass service is looked up
// under its keys, most specific first (see serviceKeys).
type statusKey struct {
	kind  string // "url", "host", or "name"
	value string
}

func (k statusKey) String() string { return fmt.Sprintf("%s %q", k.kind, k.value) }

// StatusProviderStatus is a snapshot of one status provider for /debug.
type StatusProviderStatus struct {
	Name string
	Type string
	// LastChecked is the most recent fetch attempt, successful or not;
	// pair with Error like SourceStatus.LastLoaded.
	LastChecked time.Time
	Checks      int
	Matched     int
	Error       string
}

// StatusProviders returns the state of every status provider, including how
// many currently published services each one matched.
func (r *Registry) StatusProviders() []StatusProviderStatus {
	services := r.Services()
	out := make([]StatusProviderStatus, 0, len(r.statuses))
	for _, st := range r.statuses {
		ps := StatusProviderStatus{Name: st.provider.Name(), Type: st.provider.Type()}
		if p := st.lastErr.Load(); p != nil {
			ps.Error = *p
		}
		if u := st.lastAttemptUnix.Load(); u > 0 {
			ps.LastChecked = time.Unix(u, 0)
		}
		if snap := st.snapshot.Load(); snap != nil {
			ps.Checks = snap.checks
		}
		for _, service := range services {
			if service.Status != nil && service.Status.Provider == ps.Name {
				ps.Matched++
			}
		}
		out = append(out, ps)
	}
	return out
}

func (r *Registry) refreshStatuses(ctx context.Context) {
	for _, st := range r.statuses {
		r.refreshStatus(ctx, st)
	}
}

func (r *Registry) refreshStatus(ctx context.Context, st *statusState) {
	if r.loadTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.loadTimeout)
		defer cancel()
	}
	name := st.provider.Name()
	start := time.Now()
	checks, err := st.provider.Fetch(ctx)
	st.lastAttemptUnix.Store(time.Now().Unix())
	metrics.ObserveStatusRefresh(name, len(checks), time.Since(start), err)
	if err != nil {
		msg := err.Error()
		st.lastErr.Store(&msg)
		if r.logger != nil {
			r.logger.WarnContext(ctx, "Status refresh failed",
				slog.String("provider", name), slog.String("err", msg))
		}
		return
	}
	up := make(map[statusKey]bool, len(checks))
	for _, check := range checks {
		for _, key := range checkKeys(check.Service) {
			if prev, seen := up[key]; seen {
				up[key] = prev && check.Up
				continue
			}
			up[key] = check.Up
		}
	}
	st.snapshot.Store(&statusSnapshot{up: up, checks: len(checks), checkedAt: time.Now()})
	st.lastErr.Store(nil)
	if r.logger != nil {
		r.logger.DebugContext(ctx, "Status refresh complete",
			slog.String("provider", name), slog.Int("checks", len(checks)))
	}
}

func (r *Registry) statusWatchLoop(ctx context.Context, st *statusState) {
	ticker := time.NewTicker(st.provider.Interval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.refreshStatus(ctx, st)
		}
	}
}

// applyStatuses sets Status on each service from the first provider whose
// snapshot has a matching key. services must already be clones.
func (r *Registry) applyStatuses(services []compass.Service) {
	if len(r.statuses) == 0 {
		return
	}
	for i := range services {
		for _, st := range r.statuses {
			snap := st.snapshot.Load()
			if snap == nil {
				continue
			}
			if up, _, ok := snap.lookup(services[i]); ok {
				services[i].Status = &compass.Status{
					Up:        up,
					Provider:  st.provider.Name(),
					CheckedAt: snap.checkedAt,
				}
				break
			}
		}
	}
}

// lookup finds a check for service, trying its keys from most to least
// specific: URL with path, then host, then name.
func (snap *statusSnapshot) lookup(service compass.Service) (up bool, key statusKey, ok bool) {
	for _, key := range serviceKeys(service) {
		if up, ok := snap.up[key]; ok {
			return up, key, true
		}
	}
	return false, statusKey{}, false
}

// explainStatuses appends a status step to each explanation so /debug says
// which provider set a service's dot, or which keys were tried when none
// matched. Providers that have not fetched successfully yet are skipped.
func (r *Registry) explainStatuses(explanations []ServiceExplanation) {
	if len(r.statuses) == 0 {
		return
	}
	services := make(map[string]compass.Service)
	for _, service := range r.Services() {
		services[service.ID] = service
	}
	for i := range explanations {
		service, ok := services[explanations[i].ID]
		if !ok {
			continue
		}
		if step := r.statusStep(service); step != "" {
			explanations[i].Steps = append(explanations[i].Steps, step)
		}
	}
}

func (r *Registry) statusStep(service compass.Service) string {
	checked := false
	for _, st := range r.statuses {
		snap := st.snapshot.Load()
		if snap == nil {
			continue
		}
		checked = true
		if up, key, ok := snap.lookup(service); ok {
			state := "down"
			if up {
				state = "up"
			}
			return fmt.Sprintf(
				"Status: %s, reported by %s (matched %s)",
				state,
				st.provider.Name(),
				key,
			)
		}
	}
	if !checked {
		return ""
	}
	keys := serviceKeys(service)
	tried := make([]string, len(keys))
	for i, key := range keys {
		tried[i] = key.String()
	}
	return "No status: no check matched " + strings.Join(tried, ", ")
}

// checkKeys classifies a check's service value by its shape: a plain word
// is a service name, and a URL or host[:port] is a host. A value with a
// path is also a URL key, so it matches a service at that exact URL first
// and otherwise its host, as for a blackbox probe of https://app/health.
func checkKeys(value string) []statusKey {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if !strings.Contains(value, "://") && !strings.ContainsAny(value, ".:/") {
		return []statusKey{{kind: "name", value: strings.ToLower(value)}}
	}
	host, path := hostPath(value)
	switch {
	case host == "":
		return nil
	case path != "":
		return []statusKey{{kind: "url", value: host + path}, {kind: "host", value: host}}
	default:
		return []statusKey{{kind: "host", value: host}}
	}
}

// serviceKeys lists the keys a compass service can be matched under, most
// specific first.
func serviceKeys(service compass.Service) []statusKey {
	var keys []statusKey
	host, path := hostPath(service.URL)
	if host != "" && path != "" {
		keys = append(keys, statusKey{kind: "url", value: host + path})
	}
	if host != "" {
		keys = append(keys, statusKey{kind: "host", value: host})
	}
	if name := strings.ToLower(strings.TrimSpace(service.Name)); name != "" {
		keys = append(keys, statusKey{kind: "name", value: name})
	}
	return keys
}

// hostPath reduces a URL or bare host[:port][/path] to a lowercase host and
// a path without trailing slash. The port is kept unless it is 80 or 443, so
// services told apart only by port (http://192.168.1.10:3000 vs :8080) stay
// apart, while https://grafana.home and grafana.home:443 still match.
func hostPath(value string) (host, path string) {
	u, err := url.Parse(value)
	if err != nil || u.Host == "" {
		u, err = url.Parse("//" + value)
		if err != nil {
			return "", ""
		}
	}
	host = strings.ToLower(u.Hostname())
	if host == "" {
		return "", ""
	}
	switch port := u.Port(); port {
	case "", "80", "443":
	default:
		host = net.JoinHostPort(host, port)
	}
	return host, strings.TrimRight(u.Path, "/")
}
