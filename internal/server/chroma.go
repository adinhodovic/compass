package server

import (
	"bytes"
	"net/http"
	"sync"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
)

// Syntax-highlighting CSS for fenced code blocks in markdown pages. The
// goldmark highlighting extension emits class-based markup; this CSS
// fills in the colors. Style + class prefix must match
// internal/pages.newMarkdown.
//
// Generated lazily on the first /static/chroma.css request and cached
// for the process lifetime — the output depends only on the chosen
// chroma style, which doesn't change at runtime.
var (
	chromaCSSOnce sync.Once
	chromaCSSData []byte
)

func chromaCSSHandler(w http.ResponseWriter, _ *http.Request) {
	chromaCSSOnce.Do(func() {
		formatter := chromahtml.New(
			chromahtml.WithClasses(true),
			chromahtml.ClassPrefix("chroma-"),
		)
		var buf bytes.Buffer
		_ = formatter.WriteCSS(&buf, styles.Get("github-dark"))
		chromaCSSData = buf.Bytes()
	})
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	_, _ = w.Write(chromaCSSData)
}
