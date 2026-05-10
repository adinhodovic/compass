package pages

import (
	"bytes"
	"strings"

	"github.com/adinhodovic/compass/internal/compass"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// newMarkdown builds the goldmark instance shared by every render. GFM,
// auto-heading-id, raw-HTML passthrough (for shortcode output), and
// chroma-class-based syntax highlighting on fenced code blocks. The
// matching CSS lives at /static/chroma.css and ships from
// internal/server.
func newMarkdown(extra ...goldmark.Option) goldmark.Markdown {
	opts := []goldmark.Option{
		goldmark.WithExtensions(
			extension.GFM,
			highlighting.NewHighlighting(
				highlighting.WithStyle("github-dark"),
				highlighting.WithFormatOptions(
					chromahtml.WithClasses(true),
					chromahtml.ClassPrefix("chroma-"),
				),
			),
		),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(html.WithUnsafe()),
	}
	opts = append(opts, extra...)
	return goldmark.New(opts...)
}

// markdownFor builds a goldmark instance whose inline parsers know
// about the current services list, so `[[name]]` and `[[id]]`
// references in prose resolve to /services/{id} links. Inherits the
// loader's base options (GFM, syntax highlighting, etc.).
func (l *Loader) markdownFor(services []compass.Service) goldmark.Markdown {
	if len(services) == 0 {
		return l.markdown
	}
	return newMarkdown(
		goldmark.WithParserOptions(
			parser.WithInlineParsers(
				util.Prioritized(newWikiLinkParser(services), 199),
			),
		),
	)
}

// wikiLinkParser turns `[[name]]` or `[[service-id]]` into an inline
// link to /services/{id} when the bracket contents resolve to a known
// service. Unknown names fall through to other parsers (so a literal
// `[[…]]` survives unchanged in prose).
//
// Implemented as a goldmark inline parser specifically so it doesn't
// fire inside code spans or fenced blocks — those skip inline parsing
// natively, which is exactly the behavior we want.
type wikiLinkParser struct {
	byKey map[string]compass.Service
}

func newWikiLinkParser(services []compass.Service) *wikiLinkParser {
	return &wikiLinkParser{byKey: serviceLookup(services)}
}

func (p *wikiLinkParser) Trigger() []byte { return []byte{'['} }

func (p *wikiLinkParser) Parse(_ ast.Node, block text.Reader, _ parser.Context) ast.Node {
	line, _ := block.PeekLine()
	if len(line) < 5 || line[0] != '[' || line[1] != '[' {
		return nil
	}
	close := bytes.Index(line[2:], []byte("]]"))
	if close < 0 {
		return nil
	}
	name := strings.TrimSpace(string(line[2 : 2+close]))
	if name == "" {
		return nil
	}
	service, ok := p.byKey[strings.ToLower(name)]
	if !ok {
		return nil
	}
	block.Advance(2 + close + 2)
	link := ast.NewLink()
	link.Destination = []byte("/services/" + service.ID)
	link.AppendChild(link, ast.NewString([]byte(service.Name)))
	return link
}

// serviceLookup returns a case-insensitive index keyed by every plausible
// way an author might refer to a service in markdown: the literal ID, the
// display name, and the simple slug of the display name (so `Grafana`
// resolves the same as the auto-generated `manual-grafana` ID). First
// entry wins on collision, so explicit ID matches always beat name
// fallbacks.
func serviceLookup(services []compass.Service) map[string]compass.Service {
	out := make(map[string]compass.Service, len(services)*3)
	put := func(key string, s compass.Service) {
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" {
			return
		}
		if _, exists := out[key]; !exists {
			out[key] = s
		}
	}
	// Pass 1: IDs win.
	for _, s := range services {
		put(s.ID, s)
	}
	// Pass 2: display names + simple slug fallback.
	for _, s := range services {
		put(s.Name, s)
		put(simpleSlug(s.Name), s)
	}
	return out
}

// TOCEntry is one heading in a page's table of contents.
type TOCEntry struct {
	Level  int // 2 for h2, 3 for h3, etc.
	Text   string
	Anchor string // matches the auto-heading-id on the rendered heading
}

// extractTOC walks the parsed AST and collects h2/h3 entries with their
// auto-generated anchor IDs.
func extractTOC(root ast.Node, source []byte) []TOCEntry {
	var toc []TOCEntry
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		h, ok := n.(*ast.Heading)
		if !ok {
			return ast.WalkContinue, nil
		}
		if h.Level < 2 || h.Level > 3 {
			return ast.WalkContinue, nil
		}
		anchor := ""
		if id, found := h.AttributeString("id"); found {
			if b, ok := id.([]byte); ok {
				anchor = string(b)
			}
		}
		toc = append(toc, TOCEntry{
			Level:  h.Level,
			Text:   headingText(h, source),
			Anchor: anchor,
		})
		return ast.WalkContinue, nil
	})
	return toc
}

// headingText returns the plain-text content of a heading, including text
// nested inside emphasis / links / code spans, by walking descendant
// *ast.Text nodes and concatenating their source segments.
func headingText(h *ast.Heading, source []byte) string {
	var sb strings.Builder
	_ = ast.Walk(h, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if t, ok := n.(*ast.Text); ok {
			sb.Write(t.Segment.Value(source))
		}
		return ast.WalkContinue, nil
	})
	return sb.String()
}
