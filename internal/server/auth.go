package server

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strings"

	"github.com/adinhodovic/compass/internal/config"
	"golang.org/x/crypto/bcrypt"
)

type basicAuthContextKey struct{}

// authModeName returns the string template helpers and the /debug
// stats use to label the active auth mode. Mirrors the dispatch in
// authMiddleware so the label can never disagree with the runtime.
func authModeName(cfg config.AuthConfig) string {
	switch {
	case len(cfg.Basic.Users) > 0:
		if !cfg.Required {
			return "optional-basic"
		}
		return "basic"
	case cfg.Required:
		return "forwarded"
	default:
		return "open"
	}
}

// dummyBcryptHash is a never-matching bcrypt hash used by verifyBasic to
// keep the wall-clock time of "unknown user" indistinguishable from
// "user exists but wrong password". The plaintext was a one-shot random
// string; nothing in the project stores or compares against it.
var dummyBcryptHash = []byte(
	"$2a$10$abcdefghijklmnopqrstuOCm/CPiP3xL0NSqSpiSb1bQDXn3T6BBO",
)

// authMiddleware wraps the mux with the configured auth surface. Four
// modes (see config.AuthConfig):
//
//   - optional basic: public by default, with /login triggering the Basic
//     auth prompt and authenticated requests receiving user/group headers.
//   - required basic: prompts for HTTP basic auth on every non-exempt route.
//   - forward auth (`required: true`, no basic users): rejects requests without the user
//     header (401), and rejects callers outside `trusted_proxies` (403)
//     when that list is set.
//   - open: passes through. Headers, if present, are still read by
//     userFrom() for personalization.
//
// /static/*, /health, and /metrics (when registered) are exempt — they need
// to be reachable for assets, probes, and Prometheus scrapes.
func authMiddleware(
	next http.Handler,
	cfg config.AuthConfig,
	trustedProxies []trustedProxyMatcher,
	logger *slog.Logger,
) http.Handler {
	if len(cfg.Basic.Users) > 0 {
		return basicAuthMiddleware(next, cfg)
	}
	if cfg.Required {
		if len(cfg.TrustedProxies) == 0 {
			logger.Warn(
				"auth.required is enabled without auth.trusted_proxies; any caller can spoof user headers if Compass is reachable directly",
			)
		}
		return forwardAuthMiddleware(next, cfg, trustedProxies, logger)
	}
	return next
}

func basicLogout(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="compass"`)
	w.Header().Set("Cache-Control", "no-store")
	http.Error(
		w,
		"Logged out. If your browser keeps sending credentials, close the tab or clear saved credentials.",
		http.StatusUnauthorized,
	)
}

func basicChallenge(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="compass"`)
	http.Error(w, "Unauthorized", http.StatusUnauthorized)
}

func basicAuthMiddleware(
	next http.Handler,
	cfg config.AuthConfig,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/logout" {
			basicLogout(w)
			return
		}
		if !cfg.Required && r.URL.Path == "/login" {
			if _, ok := authenticateBasicRequest(r, cfg); !ok {
				basicChallenge(w)
				return
			}
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		if isAuthExempt(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if !cfg.Required {
			if _, _, ok := r.BasicAuth(); ok {
				withUser, authed := authenticateBasicRequest(r, cfg)
				if !authed {
					basicChallenge(w)
					return
				}
				r = withUser
			}
			next.ServeHTTP(w, r)
			return
		}
		withUser, ok := authenticateBasicRequest(r, cfg)
		if !ok {
			basicChallenge(w)
			return
		}
		r = withUser
		if len(cfg.RequiredGroups) > 0 &&
			!hasAnyGroup(parseGroups(r.Header.Get(cfg.GroupsHeader)), cfg.RequiredGroups) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func authenticateBasicRequest(r *http.Request, cfg config.AuthConfig) (*http.Request, bool) {
	user, pass, ok := r.BasicAuth()
	if !ok || !verifyBasic(cfg.Basic.Users, user, pass) {
		return r, false
	}
	// Make the authenticated identity visible to downstream handlers via the
	// same header the forward-auth path uses, so userFrom() has one code path.
	r.Header.Set(cfg.UserHeader, user)
	if cfg.GroupsHeader != "" {
		if groups := groupsForBasicUser(cfg.Basic.Users, user); len(groups) > 0 {
			r.Header.Set(cfg.GroupsHeader, strings.Join(groups, ","))
		}
	}
	ctx := context.WithValue(r.Context(), basicAuthContextKey{}, true)
	return r.WithContext(ctx), true
}

func basicAuthVerified(r *http.Request) bool {
	verified, _ := r.Context().Value(basicAuthContextKey{}).(bool)
	return verified
}

// groupsForBasicUser returns the configured groups for the named basic-auth
// user. Only called after verifyBasic succeeds, so the name is trusted.
func groupsForBasicUser(users []config.BasicAuthUser, name string) []string {
	for _, u := range users {
		if u.Name == name {
			return u.Groups
		}
	}
	return nil
}

func forwardAuthMiddleware(
	next http.Handler,
	cfg config.AuthConfig,
	matchers []trustedProxyMatcher,
	_ *slog.Logger,
) http.Handler {
	required := cfg.RequiredGroups
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAuthExempt(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if len(matchers) > 0 && !remoteAllowed(r, matchers) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if strings.TrimSpace(r.Header.Get(cfg.UserHeader)) == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if len(required) > 0 {
			groups := parseGroups(r.Header.Get(cfg.GroupsHeader))
			if !hasAnyGroup(groups, required) {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// hasAnyGroup reports whether `have` intersects `want`. Both sides are
// expected to be small (single-digit lengths), so a nested loop is fine.
func hasAnyGroup(have, want []string) bool {
	for _, g := range have {
		if slices.Contains(want, g) {
			return true
		}
	}
	return false
}

func trimGroups(groups []string) []string {
	out := make([]string, 0, len(groups))
	for _, group := range groups {
		out = append(out, strings.TrimSpace(group))
	}
	return out
}

// isAuthExempt is the allowlist for paths the auth middleware lets through
// unconditionally: static assets, health probes, /metrics (the normal Prom
// topology scrapes from inside the cluster), and the PWA manifest (browsers
// fetch it without credentials).
func isAuthExempt(path string) bool {
	return strings.HasPrefix(path, "/static/") || strings.HasPrefix(path, "/assets/") ||
		path == "/health" || path == "/metrics" || path == "/manifest.webmanifest"
}

// verifyBasic checks every configured user even when the supplied username
// matches none, so the wall-clock time of "unknown user" is
// indistinguishable from "user exists but wrong password" — i.e. an attacker
// can't enumerate usernames via response timing.
func verifyBasic(users []config.BasicAuthUser, name, pass string) bool {
	matched := false
	for _, u := range users {
		hash := []byte(u.PasswordHash)
		if subtle.ConstantTimeCompare([]byte(u.Name), []byte(name)) != 1 {
			hash = dummyBcryptHash
		}
		if bcrypt.CompareHashAndPassword(hash, []byte(pass)) == nil &&
			subtle.ConstantTimeCompare([]byte(u.Name), []byte(name)) == 1 {
			matched = true
		}
	}
	return matched
}

// trustedProxyMatcher accepts both single IPs and CIDRs.
type trustedProxyMatcher struct {
	ip   net.IP
	cidr *net.IPNet
}

func (m trustedProxyMatcher) match(ip net.IP) bool {
	if m.cidr != nil {
		return m.cidr.Contains(ip)
	}
	return m.ip.Equal(ip)
}

func compileTrustedProxies(entries []string) ([]trustedProxyMatcher, error) {
	out := make([]trustedProxyMatcher, 0, len(entries))
	for _, raw := range entries {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			_, cidr, err := net.ParseCIDR(entry)
			if err != nil {
				return nil, fmt.Errorf("invalid CIDR %q: %w", entry, err)
			}
			out = append(out, trustedProxyMatcher{cidr: cidr})
			continue
		}
		ip := net.ParseIP(entry)
		if ip == nil {
			return nil, fmt.Errorf("invalid IP %q", entry)
		}
		out = append(out, trustedProxyMatcher{ip: ip})
	}
	return out, nil
}

func remoteAllowed(r *http.Request, matchers []trustedProxyMatcher) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, m := range matchers {
		if m.match(ip) {
			return true
		}
	}
	return false
}
