package pages

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"regexp"
	"slices"
	"strings"

	"github.com/adinhodovic/compass/internal/compass"
	"github.com/adinhodovic/compass/internal/logo"
)

//go:embed templates
var templatesFS embed.FS

// shortcodeTmpl renders the shortcode output (services grid + panel
// card). Parsed once at startup; html/template's contextual escaping
// handles URL/attr safety. The bluemonday pass after goldmark drops
// anything unexpected. Always invoke via ExecuteTemplate(name, ...) so
// the right template fires regardless of file order.
var shortcodeTmpl = template.Must(
	template.New("").
		Funcs(template.FuncMap{"icon": logo.IconHTML}).
		ParseFS(templatesFS, "templates/*.html"),
)

// shortcodeData feeds the service_cards template.
type shortcodeData struct {
	Cards []shortcodeCard
}

type shortcodeCard struct {
	Service compass.Service
	Logo    logo.Logo
}

// panelCardData feeds the panel_card template.
type panelCardData struct {
	Title string
	URL   string
}

// shortcodeRe matches {{< services key=value key=value >}} blocks.
var shortcodeRe = regexp.MustCompile(`(?s)\{\{<\s*services\b([^>]*)>\}\}`)

// singleServiceRe matches {{< service id=X >}}. The `\b` after `service`
// keeps the plural shortcode from matching here; they're disjoint.
var singleServiceRe = regexp.MustCompile(`(?s)\{\{<\s*service\b([^>]*)>\}\}`)

// panelRe matches {{< panel service=X title=Y >}} — embeds a single
// Grafana panel from a service's `panels:` list.
var panelRe = regexp.MustCompile(`(?s)\{\{<\s*panel\b([^>]*)>\}\}`)

// shortcodeEscapeRe matches the Hugo-style escape form
// `{{</* service[s] key=value */>}}`. The first two regexes skip it
// (they don't see `service\b` directly after `\s*`), so we can do a
// straight third pass that swaps escapes for the literal shortcode
// text — which is what an author writing docs about the feature wants.
// Capture group 1 is the keyword (`service` or `services`); group 2 is
// the args.
var shortcodeEscapeRe = regexp.MustCompile(`(?s)\{\{</\*\s*(services?)\b(.*?)\*/>\}\}`)

const (
	shortcodeArgSource  = "source"
	shortcodeArgTag     = "tag"
	shortcodeArgID      = "id"
	shortcodeArgService = "service"
	shortcodeArgTitle   = "title"
)

// expandShortcodes replaces every services / service shortcode in body
// with HTML, then unwraps any Hugo-style escape (`{{</* … */>}}`) into
// its literal text so docs about the feature can show the syntax
// without it being expanded. Goldmark renders the resulting raw HTML
// thanks to WithUnsafe.
//
// Plural runs before singular because the regexes are disjoint anyway
// (`\bservices\b` vs `\bservice\b`) but the ordering matches how
// authors typically reach for the feature.
func expandShortcodes(body []byte, services []compass.Service) []byte {
	body = shortcodeEscapeRe.ReplaceAllFunc(body, func(match []byte) []byte {
		groups := shortcodeEscapeRe.FindSubmatch(match)
		keyword := string(groups[1])
		args := strings.TrimSpace(string(groups[2]))
		if args == "" {
			return []byte("{{< " + keyword + " >}}")
		}
		return []byte("{{< " + keyword + " " + args + " >}}")
	})
	body, restore := maskMarkdownCode(body)
	body = shortcodeRe.ReplaceAllFunc(body, func(match []byte) []byte {
		args := parseShortcodeArgs(string(shortcodeRe.FindSubmatch(match)[1]))
		return renderServiceList(filterServices(services, args))
	})
	body = singleServiceRe.ReplaceAllFunc(body, func(match []byte) []byte {
		args := parseShortcodeArgs(string(singleServiceRe.FindSubmatch(match)[1]))
		return renderSingleService(services, args[shortcodeArgID], args[shortcodeArgSource])
	})
	body = panelRe.ReplaceAllFunc(body, func(match []byte) []byte {
		args := parseShortcodeArgs(string(panelRe.FindSubmatch(match)[1]))
		return renderPanel(
			services,
			args[shortcodeArgService],
			args[shortcodeArgTitle],
			args[shortcodeArgSource],
		)
	})
	return restore(body)
}

var inlineCodeRe = regexp.MustCompile("`[^`\n]*`")

func maskMarkdownCode(body []byte) ([]byte, func([]byte) []byte) {
	var masked [][]byte
	store := func(segment []byte) []byte {
		token := []byte(fmt.Sprintf("\x00compass-code-%d\x00", len(masked)))
		masked = append(masked, append([]byte(nil), segment...))
		return token
	}

	var out bytes.Buffer
	lines := bytes.SplitAfter(body, []byte("\n"))
	for i := 0; i < len(lines); i++ {
		trimmed := bytes.TrimLeft(lines[i], " \t")
		fence := codeFence(trimmed)
		if fence == "" {
			out.Write(lines[i])
			continue
		}
		var block bytes.Buffer
		block.Write(lines[i])
		i++
		for ; i < len(lines); i++ {
			block.Write(lines[i])
			if strings.HasPrefix(string(bytes.TrimLeft(lines[i], " \t")), fence) {
				break
			}
		}
		out.Write(store(block.Bytes()))
	}

	body = inlineCodeRe.ReplaceAllFunc(out.Bytes(), store)
	restore := func(value []byte) []byte {
		for i, segment := range masked {
			token := []byte(fmt.Sprintf("\x00compass-code-%d\x00", i))
			value = bytes.ReplaceAll(value, token, segment)
		}
		return value
	}
	return body, restore
}

func codeFence(line []byte) string {
	s := string(line)
	switch {
	case strings.HasPrefix(s, "```"):
		return "```"
	case strings.HasPrefix(s, "~~~"):
		return "~~~"
	default:
		return ""
	}
}

// renderSingleService looks up `id` in services and emits a card for it.
// Falls back to a small italic note when the key doesn't resolve so the
// author sees the typo at render time instead of in their head. Accepts
// the literal service ID, the display name, or a simple slug of the
// name — see serviceLookup.
func renderSingleService(services []compass.Service, id, source string) []byte {
	id = strings.TrimSpace(id)
	source = strings.TrimSpace(source)
	if id == "" {
		return []byte(
			`<p class="text-sm italic text-base-content/60">{{< service >}} requires id=&lt;service-id&gt;.</p>`,
		)
	}
	if source != "" {
		services = filterServices(services, map[string]string{shortcodeArgSource: source})
	}
	if s, ok := serviceLookup(services)[strings.ToLower(id)]; ok {
		return renderServiceList([]compass.Service{s})
	}
	if source != "" {
		return []byte(
			`<p class="text-sm italic text-base-content/60">Unknown service: ` +
				template.HTMLEscapeString(id) + ` in source ` +
				template.HTMLEscapeString(source) + `</p>`,
		)
	}
	return []byte(
		`<p class="text-sm italic text-base-content/60">Unknown service: ` +
			template.HTMLEscapeString(id) + `</p>`,
	)
}

// renderPanel emits a Grafana iframe by service ID + panel title. Same
// markup as the service detail page so embeds inherit any future
// styling tweaks consistently. The service key follows the same
// tolerant resolution as renderSingleService.
func renderPanel(services []compass.Service, serviceKey, title, source string) []byte {
	serviceKey = strings.TrimSpace(serviceKey)
	title = strings.TrimSpace(title)
	source = strings.TrimSpace(source)
	if serviceKey == "" || title == "" {
		return []byte(
			`<p class="text-sm italic text-base-content/60">{{< panel >}} requires service=&lt;id&gt; and title=&lt;panel-title&gt;.</p>`,
		)
	}
	if source != "" {
		services = filterServices(services, map[string]string{shortcodeArgSource: source})
	}
	s, ok := serviceLookup(services)[strings.ToLower(serviceKey)]
	if !ok {
		if source != "" {
			return []byte(
				`<p class="text-sm italic text-base-content/60">Unknown service: ` +
					template.HTMLEscapeString(serviceKey) + ` in source ` +
					template.HTMLEscapeString(source) + `</p>`,
			)
		}
		return []byte(
			`<p class="text-sm italic text-base-content/60">Unknown service: ` +
				template.HTMLEscapeString(serviceKey) + `</p>`,
		)
	}
	for _, p := range s.Panels {
		if p.Title != title {
			continue
		}
		var buf bytes.Buffer
		if err := shortcodeTmpl.ExecuteTemplate(&buf, "panel_card.html", panelCardData{
			Title: p.Title,
			URL:   p.URL,
		}); err != nil {
			return []byte(
				`<p class="text-sm italic text-base-content/60">Panel unavailable.</p>`,
			)
		}
		return buf.Bytes()
	}
	return []byte(
		`<p class="text-sm italic text-base-content/60">Service ` +
			template.HTMLEscapeString(s.ID) + ` has no panel titled "` +
			template.HTMLEscapeString(title) + `".</p>`,
	)
}

// parseShortcodeArgs parses key=value pairs with simple single/double-quoted
// values. It is intentionally small because shortcode syntax only supports a
// few filters, not shell semantics.
func parseShortcodeArgs(s string) map[string]string {
	out := map[string]string{}
	for _, part := range splitShortcodeFields(s) {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		out[strings.TrimSpace(k)] = v
	}
	return out
}

func splitShortcodeFields(s string) []string {
	var fields []string
	var current strings.Builder
	var quote rune
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			current.WriteRune(r)
		case r == '\'' || r == '"':
			quote = r
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if current.Len() > 0 {
				fields = append(fields, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		fields = append(fields, current.String())
	}
	return fields
}

func filterServices(services []compass.Service, args map[string]string) []compass.Service {
	out := make([]compass.Service, 0, len(services))
	tag := args[shortcodeArgTag]
	src := args[shortcodeArgSource]
	for _, s := range services {
		if tag != "" && !slices.Contains(s.Tags, tag) {
			continue
		}
		if src != "" && s.Source != src {
			continue
		}
		out = append(out, s)
	}
	return out
}

func renderServiceList(services []compass.Service) []byte {
	data := shortcodeData{Cards: make([]shortcodeCard, len(services))}
	for i, s := range services {
		data.Cards[i] = shortcodeCard{
			Service: s,
			Logo:    logo.Resolve(s.Icon, s.Name, s.SourceType),
		}
	}
	var buf bytes.Buffer
	buf.WriteString("\n\n")
	if err := shortcodeTmpl.ExecuteTemplate(&buf, "service_cards.html", data); err != nil {
		// Embedded template, validated at startup — failure here is a
		// programming error, not user input. Fall back to a minimal
		// inline message rather than crashing the request.
		return []byte(
			"\n\n<p class=\"text-sm italic text-base-content/60\">Service list unavailable.</p>\n\n",
		)
	}
	buf.WriteString("\n\n")
	return buf.Bytes()
}
