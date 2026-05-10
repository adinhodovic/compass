package server

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/adinhodovic/compass/internal/config"
	"github.com/adinhodovic/compass/internal/testutil"
	"golang.org/x/crypto/bcrypt"
)

func okHandler() http.Handler     { return testutil.OKHandler() }
func discardLogger() *slog.Logger { return testutil.DiscardLogger() }

func testAuthMiddleware(t *testing.T, next http.Handler, cfg config.AuthConfig) http.Handler {
	t.Helper()
	matchers, err := compileTrustedProxies(cfg.TrustedProxies)
	if err != nil {
		t.Fatalf("compile trusted proxies: %v", err)
	}
	return authMiddleware(next, cfg, matchers, discardLogger())
}

func TestAuthMiddlewareOpenMode(t *testing.T) {
	mw := testAuthMiddleware(t, okHandler(), config.AuthConfig{})
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 200 {
		t.Fatalf("expected open mode to pass through, got %d", rec.Code)
	}
}

func TestAuthMiddlewareForwardAuthRejectsMissingHeader(t *testing.T) {
	cfg := config.AuthConfig{
		UserHeader: "X-Forwarded-User",
		Required:   true,
	}
	mw := testAuthMiddleware(t, okHandler(), cfg)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing user header, got %d", rec.Code)
	}
}

func TestAuthMiddlewareForwardAuthAcceptsHeader(t *testing.T) {
	cfg := config.AuthConfig{
		UserHeader: "X-Forwarded-User",
		Required:   true,
	}
	mw := testAuthMiddleware(t, okHandler(), cfg)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-User", "alice")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("expected 200 with valid header, got %d", rec.Code)
	}
}

func TestAuthMiddlewareForwardAuthTrustedProxies(t *testing.T) {
	cfg := config.AuthConfig{
		UserHeader:     "X-Forwarded-User",
		Required:       true,
		TrustedProxies: []string{"10.0.0.0/8", "192.168.1.5"},
	}
	mw := testAuthMiddleware(t, okHandler(), cfg)

	tests := []struct {
		remote string
		want   int
	}{
		{"10.1.2.3:443", http.StatusOK},
		{"192.168.1.5:443", http.StatusOK},
		{"203.0.113.7:443", http.StatusForbidden},
	}
	for _, tt := range tests {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Forwarded-User", "alice")
		req.RemoteAddr = tt.remote
		rec := httptest.NewRecorder()
		mw.ServeHTTP(rec, req)
		if rec.Code != tt.want {
			t.Errorf("remote=%s: want %d, got %d", tt.remote, tt.want, rec.Code)
		}
	}
}

func TestAuthMiddlewareBasicAuthRejectsAndAccepts(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("hunter2"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	cfg := config.AuthConfig{
		UserHeader: "X-Forwarded-User",
		Basic: config.BasicAuthConfig{
			Users: []config.BasicAuthUser{{Name: "alice", PasswordHash: string(hash)}},
		},
	}
	mw := testAuthMiddleware(t, okHandler(), cfg)

	t.Run("missing creds", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mw.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
		if !strings.Contains(rec.Header().Get("WWW-Authenticate"), "Basic") {
			t.Fatalf("expected basic challenge, got %q", rec.Header().Get("WWW-Authenticate"))
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.SetBasicAuth("alice", "wrong")
		rec := httptest.NewRecorder()
		mw.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("correct creds inject user header", func(t *testing.T) {
		captured := ""
		check := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			captured = r.Header.Get("X-Forwarded-User")
		})
		mw := testAuthMiddleware(t, check, cfg)
		req := httptest.NewRequest("GET", "/", nil)
		req.SetBasicAuth("alice", "hunter2")
		rec := httptest.NewRecorder()
		mw.ServeHTTP(rec, req)
		if captured != "alice" {
			t.Fatalf("expected user header injected, got %q", captured)
		}
	})
}

func TestAuthMiddlewareForwardAuthEdgeCases(t *testing.T) {
	cfg := config.AuthConfig{
		UserHeader: "X-Forwarded-User",
		Required:   true,
	}
	mw := testAuthMiddleware(t, okHandler(), cfg)

	tests := []struct {
		name       string
		userHeader string
		want       int
	}{
		{"whitespace-only header", "   ", http.StatusUnauthorized},
		{"tab-only header", "\t", http.StatusUnauthorized},
		{"valid trimmed value", "  alice  ", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Set("X-Forwarded-User", tt.userHeader)
			rec := httptest.NewRecorder()
			mw.ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Errorf("want %d, got %d", tt.want, rec.Code)
			}
		})
	}
}

func TestAuthMiddlewareForwardAuthIPv6AndMalformedRemote(t *testing.T) {
	cfg := config.AuthConfig{
		UserHeader:     "X-Forwarded-User",
		Required:       true,
		TrustedProxies: []string{"::1", "fd00::/8", "10.0.0.0/8"},
	}
	mw := testAuthMiddleware(t, okHandler(), cfg)

	tests := []struct {
		remote string
		want   int
	}{
		{"[::1]:443", http.StatusOK},
		{"[fd00::1]:443", http.StatusOK},
		{"[2001:db8::1]:443", http.StatusForbidden},
		{"10.0.0.5:443", http.StatusOK},
		{"unparseable", http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.remote, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Set("X-Forwarded-User", "alice")
			req.RemoteAddr = tt.remote
			rec := httptest.NewRecorder()
			mw.ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Errorf("remote=%s: want %d, got %d", tt.remote, tt.want, rec.Code)
			}
		})
	}
}

func TestVerifyBasicConstantTime(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("hunter2"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	users := []config.BasicAuthUser{{Name: "alice", PasswordHash: string(hash)}}

	if !verifyBasic(users, "alice", "hunter2") {
		t.Fatalf("expected correct creds to verify")
	}
	if verifyBasic(users, "alice", "wrong") {
		t.Fatalf("wrong password must not verify")
	}
	if verifyBasic(users, "mallory", "anything") {
		t.Fatalf("unknown user must not verify")
	}
	if verifyBasic(users, "mallory", "hunter2") {
		t.Fatalf("right password against unknown user must not verify")
	}
}

func TestAuthMiddlewareExemptsProbeAndScrapePaths(t *testing.T) {
	cfg := config.AuthConfig{
		UserHeader: "X-Forwarded-User",
		Required:   true,
	}
	mw := testAuthMiddleware(t, okHandler(), cfg)
	for _, path := range []string{"/static/app.css", "/health", "/metrics"} {
		rec := httptest.NewRecorder()
		mw.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %s to bypass auth, got %d", path, rec.Code)
		}
	}
}
