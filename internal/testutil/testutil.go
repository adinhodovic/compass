// Package testutil holds the small set of fakes that more than one test
// package reaches for. Per-source mocks (Docker / Headscale / Tailscale
// fake clients) live next to their interfaces because their shape is
// source-specific; this package only carries truly cross-cutting helpers.
package testutil

import (
	"io"
	"log/slog"
	"net/http"
)

// DiscardLogger returns a slog.Logger that drops every record. Use in
// tests where logger output would be noise.
func DiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// OKHandler returns a 200-OK handler. Useful as the `next` handler when
// exercising middleware in isolation.
func OKHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}
