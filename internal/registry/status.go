package registry

import (
	"context"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/adinhodovic/compass/internal/compass"
	"github.com/adinhodovic/compass/internal/config"
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
	// up is keyed by the check's match value, normalized per the
	// provider's MatchOn. Duplicate keys resolve to down if any is down.
	up        map[string]bool
	checks    int
	checkedAt time.Time
}

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
	matchOn := st.provider.MatchOn()
	up := make(map[string]bool, len(checks))
	for _, check := range checks {
		key := statusKey(check.Match, matchOn)
		if key == "" {
			continue
		}
		if prev, ok := up[key]; ok {
			up[key] = prev && check.Up
			continue
		}
		up[key] = check.Up
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
			up, ok := snap.up[serviceStatusKey(services[i], st.provider.MatchOn())]
			if !ok {
				continue
			}
			services[i].Status = &compass.Status{
				Up:        up,
				Provider:  st.provider.Name(),
				CheckedAt: snap.checkedAt,
			}
			break
		}
	}
}

func serviceStatusKey(service compass.Service, matchOn string) string {
	switch matchOn {
	case config.StatusMatchName:
		return statusKey(service.Name, matchOn)
	case config.StatusMatchID:
		return statusKey(service.ID, matchOn)
	default:
		return statusKey(service.URL, matchOn)
	}
}

// statusKey normalizes a value so a provider's match value and a service
// field compare equal when they refer to the same thing: hosts drop
// scheme, path, and default ports; URLs drop a trailing slash; names
// ignore case.
func statusKey(value, matchOn string) string {
	value = strings.TrimSpace(value)
	switch matchOn {
	case config.StatusMatchHost:
		return hostPortKey(value)
	case config.StatusMatchURL:
		return strings.TrimRight(value, "/")
	case config.StatusMatchName:
		return strings.ToLower(value)
	default:
		return value
	}
}

// hostPortKey reduces a URL or bare host[:port] to a lowercase host, keeping
// the port only when it isn't 80 or 443. Services told apart only by port
// (http://192.168.1.10:3000 vs :8080) must not collapse into one key, while
// https://grafana.home and grafana.home:443 should still match.
func hostPortKey(value string) string {
	u, err := url.Parse(value)
	if err != nil || u.Host == "" {
		u, err = url.Parse("//" + value)
		if err != nil {
			return ""
		}
	}
	host := strings.ToLower(u.Hostname())
	switch port := u.Port(); port {
	case "", "80", "443":
		return host
	default:
		return net.JoinHostPort(host, port)
	}
}
