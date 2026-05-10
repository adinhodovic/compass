package server

import (
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// loggingMiddleware records one structured log line per HTTP request:
// method, path, status, byte count, duration, and the remote addr.
//
// Static assets (`/static/*`), health probes (`/health`), and metrics scrapes
// (`/metrics`) are logged at Debug so steady background traffic doesn't drown
// the rest of the log; everything else logs at Info. 5xx responses log at Error.
func loggingMiddleware(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &loggingResponseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		level := slog.LevelInfo
		switch {
		case rec.status >= 500:
			level = slog.LevelError
		case strings.HasPrefix(r.URL.Path, "/static/") || r.URL.Path == "/health" || r.URL.Path == "/metrics":
			level = slog.LevelDebug
		}

		logger.LogAttrs(r.Context(), level, "http",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rec.status),
			slog.Int("bytes", rec.bytes),
			slog.Duration("duration", time.Since(start)),
			slog.String("remote", r.RemoteAddr),
		)
	})
}

// loggingResponseWriter captures the status and byte count so the
// middleware can include them in the log line. WriteHeader and Write are
// the only methods we need to override; the embedded ResponseWriter
// handles everything else.
type loggingResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (l *loggingResponseWriter) WriteHeader(code int) {
	l.status = code
	l.ResponseWriter.WriteHeader(code)
}

func (l *loggingResponseWriter) Write(b []byte) (int, error) {
	n, err := l.ResponseWriter.Write(b)
	l.bytes += n
	return n, err
}
