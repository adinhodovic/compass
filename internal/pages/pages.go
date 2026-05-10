// Package pages loads user-authored markdown files from a directory and
// renders them as additional Compass pages.
//
// Files are read on demand at request time so updating a file is reflected on
// the next page load without restarting the server.
//
// # Layout
//
// The configured directory may contain top-level *.md files and/or
// sub-directories of *.md files. Each top-level file becomes /pages/{slug}.
// Each sub-directory becomes its own nav dropdown with URLs of the form
// /pages/{section}/{slug}. Sub-directory names may be prefixed with a numeric
// order (01-administration) which is stripped from the displayed title.
package pages

import (
	"bytes"
	"cmp"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/adinhodovic/compass/internal/compass"
	"github.com/adrg/frontmatter"
	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/text"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// Page describes one markdown page.
type Page struct {
	Slug      string // file slug (without .md)
	Section   string // section slug; "" for top-level pages
	Title     string
	Order     int
	Tags      []string
	UpdatedAt time.Time // file mtime; zero when stat fails
}

// Section is a group of pages, either the implicit top-level group ("") or a
// sub-directory.
type Section struct {
	Slug  string // "" for top-level
	Title string // displayed in the nav (top-level uses "Pages")
	Order int
	Pages []Page
}

// URL returns the URL for the page, accounting for whether it's nested.
func (p Page) URL() string {
	if p.Section == "" {
		return "/pages/" + p.Slug
	}
	return "/pages/" + p.Section + "/" + p.Slug
}

type frontMatter struct {
	Title string   `yaml:"title"`
	Order int      `yaml:"order"`
	Tags  []string `yaml:"tags"`
}

// Loader knows how to list and render pages from a directory.
type Loader struct {
	dir      string
	markdown goldmark.Markdown
	policy   *bluemonday.Policy
}

// NewLoader returns a Loader rooted at dir. An empty dir yields no pages.
func NewLoader(dir string) *Loader {
	return &Loader{
		dir: strings.TrimSpace(dir),
		policy: bluemonday.UGCPolicy().
			AllowAttrs("class").Globally().
			AllowAttrs("id").OnElements("h1", "h2", "h3", "h4", "h5", "h6").
			AllowElements("iconify-icon").
			AllowAttrs("icon", "width", "height", "aria-hidden").
			OnElements("iconify-icon").
			// Iframes for embedded Grafana panels via {{< panel … >}}.
			// URL is escaped at render time and pulled from a curated
			// catalog/source — not arbitrary user input.
			AllowElements("iframe").
			AllowAttrs("src", "title", "loading", "allow", "sandbox").
			OnElements("iframe"),
		markdown: newMarkdown(),
	}
}

// Backlinks returns every page whose body mentions serviceID, either
// via a `[[id]]` / `[[name]]` wiki-link or one of the service-targeting
// shortcodes (`{{< service id=X >}}`, `{{< panel service=X >}}`). Walks
// the pages directory once per call — at homelab scale (dozens of
// files) the cost is negligible and keeps the "edit a page, see it
// next request" promise intact.
//
// Pass the same services slice you'd pass to Get; the lookup matches
// by ID or by display name (case-insensitive).
func (l *Loader) Backlinks(serviceID string, services []compass.Service) ([]Page, error) {
	if !l.Enabled() {
		return nil, nil
	}
	serviceID = strings.TrimSpace(serviceID)
	if serviceID == "" {
		return nil, nil
	}
	// Every key (id / name / simple-slug) that should resolve back to
	// this service. Keeps the matching logic symmetric with
	// renderSingleService and the wiki-link parser.
	var aliases []string
	for _, s := range services {
		if s.ID != serviceID {
			continue
		}
		aliases = []string{
			strings.ToLower(s.ID),
			strings.ToLower(strings.TrimSpace(s.Name)),
			simpleSlug(s.Name),
		}
		break
	}
	if len(aliases) == 0 {
		aliases = []string{strings.ToLower(serviceID)}
	}

	matches := func(body []byte) bool {
		text := strings.ToLower(string(body))
		for _, alias := range aliases {
			if alias == "" {
				continue
			}
			if strings.Contains(text, "[["+alias+"]]") {
				return true
			}
		}
		check := func(re *regexp.Regexp, key string) bool {
			for _, m := range re.FindAllStringSubmatch(string(body), -1) {
				args := parseShortcodeArgs(m[1])
				v := strings.ToLower(strings.TrimSpace(args[key]))
				for _, alias := range aliases {
					if v != "" && v == alias {
						return true
					}
				}
			}
			return false
		}
		return check(singleServiceRe, shortcodeArgID) ||
			check(panelRe, shortcodeArgService)
	}

	var hits []Page
	scan := func(path, sectionSlug string) error {
		page, body, err := l.readPage(path, sectionSlug)
		if err != nil {
			return err
		}
		if matches(body) {
			hits = append(hits, page)
		}
		return nil
	}

	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return nil, fmt.Errorf("read pages dir %s: %w", l.dir, err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if entry.IsDir() {
			subSlug, _, _ := parseSectionName(entry.Name())
			subEntries, err := os.ReadDir(filepath.Join(l.dir, entry.Name()))
			if err != nil {
				continue
			}
			for _, sub := range subEntries {
				if strings.HasPrefix(sub.Name(), ".") || sub.IsDir() || !isMarkdown(sub.Name()) {
					continue
				}
				if err := scan(
					filepath.Join(l.dir, entry.Name(), sub.Name()),
					subSlug,
				); err != nil {
					return nil, err
				}
			}
			continue
		}
		if !isMarkdown(entry.Name()) {
			continue
		}
		if err := scan(filepath.Join(l.dir, entry.Name()), ""); err != nil {
			return nil, err
		}
	}
	sortPages(hits)
	return hits, nil
}

// simpleSlug lowercases and replaces runs of non-alphanumeric chars with
// a single dash. Mirrors what an author would intuitively type for a
// service named "Grafana" → "grafana".
func simpleSlug(name string) string {
	var b strings.Builder
	prevDash := true
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// Enabled reports whether a pages directory was configured.
func (l *Loader) Enabled() bool {
	return l != nil && l.dir != ""
}

// Sections returns the page index grouped by sub-directory. The result is
// sorted: top-level pages (if any) come first under the "Pages" label, then
// sub-directory sections in lexical order of the on-disk dir name (with any
// numeric prefix preserved for ordering).
func (l *Loader) Sections() ([]Section, error) {
	if !l.Enabled() {
		return nil, nil
	}
	info, err := os.Stat(l.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat pages dir %s: %w", l.dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("pages path %s is not a directory", l.dir)
	}
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return nil, fmt.Errorf("read pages dir %s: %w", l.dir, err)
	}

	var topLevel []Page
	sectionMap := map[string]*Section{}
	for _, entry := range entries {
		// Skip hidden entries. Kubernetes ConfigMap volume mounts ship a
		// `..data` symlink plus a timestamped `..2026_05_06_…` directory
		// alongside the real files; treating either as a page section
		// produces nonsense nav entries (e.g. dropdown titled with the
		// mount mtime).
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if entry.IsDir() {
			subPages, err := l.readDir(filepath.Join(l.dir, entry.Name()), entry.Name())
			if err != nil {
				return nil, err
			}
			if len(subPages) == 0 {
				continue
			}
			slug, title, order := parseSectionName(entry.Name())
			sectionMap[slug] = &Section{
				Slug:  slug,
				Title: title,
				Order: order,
				Pages: subPages,
			}
			continue
		}
		if !isMarkdown(entry.Name()) {
			continue
		}
		page, err := l.describe(filepath.Join(l.dir, entry.Name()), "")
		if err != nil {
			return nil, err
		}
		topLevel = append(topLevel, page)
	}

	var sections []Section
	if len(topLevel) > 0 {
		sortPages(topLevel)
		sections = append(sections, Section{
			Slug:  "",
			Title: "Pages",
			Order: -1,
			Pages: topLevel,
		})
	}
	for _, section := range sectionMap {
		sortPages(section.Pages)
		sections = append(sections, *section)
	}
	slices.SortStableFunc(sections, func(a, b Section) int {
		if c := cmp.Compare(a.Order, b.Order); c != 0 {
			return c
		}
		return cmp.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
	})
	return sections, nil
}

// Get loads, parses, and renders a single page. Pass section="" for a
// top-level page. Services are passed in for shortcode expansion (see
// {{< services tag=foo >}}); pass nil if shortcode results aren't
// needed. Returns the rendered HTML and the page's heading TOC.
func (l *Loader) Get(
	section, slug string,
	services []compass.Service,
) (Page, template.HTML, []TOCEntry, error) {
	if !l.Enabled() {
		return Page{}, "", nil, os.ErrNotExist
	}
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return Page{}, "", nil, os.ErrNotExist
	}
	if !isSafeSegment(slug) {
		return Page{}, "", nil, os.ErrNotExist
	}
	if section != "" && !isSafeSegment(section) {
		return Page{}, "", nil, os.ErrNotExist
	}
	path := l.pathFor(section, slug)
	page, body, err := l.readPage(path, section)
	if err != nil {
		return Page{}, "", nil, err
	}
	content := expandShortcodes(body, services)

	// Parse first to extract the TOC, then render to HTML. The parser
	// state caches heading IDs that the renderer reuses, so anchors
	// match the rendered <h2 id="...">.
	md := l.markdownFor(services)
	reader := text.NewReader(content)
	parsed := md.Parser().Parse(reader)
	toc := extractTOC(parsed, content)

	var rendered bytes.Buffer
	if err := md.Renderer().Render(&rendered, content, parsed); err != nil {
		return Page{}, "", nil, fmt.Errorf("render %s: %w", path, err)
	}
	return page, template.HTML(l.policy.Sanitize(rendered.String())), toc, nil
}

func (l *Loader) pathFor(section, slug string) string {
	if section == "" {
		return filepath.Join(l.dir, slug+".md")
	}
	// section here is the displayed slug (no numeric prefix). The on-disk
	// directory may have a "01-" prefix; resolve by scanning.
	entries, err := os.ReadDir(l.dir)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			dirSlug, _, _ := parseSectionName(entry.Name())
			if dirSlug == section {
				return filepath.Join(l.dir, entry.Name(), slug+".md")
			}
		}
	}
	// Fall back to the literal section name for nicer error messages.
	return filepath.Join(l.dir, section, slug+".md")
}

func (l *Loader) readDir(path, sectionDirName string) ([]Page, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("read pages section %s: %w", path, err)
	}
	sectionSlug, _, _ := parseSectionName(sectionDirName)
	var pages []Page
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue // skip ConfigMap volume mount internals (`..data`, etc.)
		}
		if entry.IsDir() {
			// One level of nesting only; deeper folders are ignored.
			continue
		}
		if !isMarkdown(entry.Name()) {
			continue
		}
		page, err := l.describe(filepath.Join(path, entry.Name()), sectionSlug)
		if err != nil {
			return nil, err
		}
		pages = append(pages, page)
	}
	return pages, nil
}

// describe is the metadata-only path used by Sections() during nav listing.
// It reads the file once and discards the body.
func (l *Loader) describe(path, section string) (Page, error) {
	page, _, err := l.readPage(path, section)
	return page, err
}

// readPage reads a markdown file once and returns both the parsed Page
// metadata and the raw body, so renderers don't have to re-read the file.
func (l *Loader) readPage(path, section string) (Page, []byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Page{}, nil, err
	}
	if info.IsDir() {
		return Page{}, nil, fmt.Errorf("pages entry %s is a directory", path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return Page{}, nil, err
	}
	var meta frontMatter
	content, err := frontmatter.Parse(bytes.NewReader(body), &meta)
	if err != nil {
		return Page{}, nil, fmt.Errorf("parse front-matter for %s: %w", path, err)
	}
	page := Page{
		Slug:      slugFromFilename(info.Name()),
		Section:   section,
		UpdatedAt: info.ModTime(),
	}
	page.Title = strings.TrimSpace(meta.Title)
	page.Order = meta.Order
	page.Tags = meta.Tags
	if page.Title == "" {
		page.Title = titleFromSlug(page.Slug)
	}
	return page, content, nil
}

func sortPages(pages []Page) {
	slices.SortStableFunc(pages, func(a, b Page) int {
		if c := cmp.Compare(a.Order, b.Order); c != 0 {
			return c
		}
		return cmp.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
	})
}

func slugFromFilename(name string) string {
	return strings.TrimSuffix(name, filepath.Ext(name))
}

func isMarkdown(name string) bool {
	return strings.ToLower(filepath.Ext(name)) == ".md"
}

func titleFromSlug(slug string) string {
	parts := strings.FieldsFunc(slug, func(r rune) bool {
		return r == '-' || r == '_'
	})
	title := cases.Title(language.Und)
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = title.String(strings.ToLower(p))
	}
	return strings.Join(parts, " ")
}

// parseSectionName turns a directory name like "01-administration" into
// (slug="administration", title="Administration", order=1). A leading
// "<digits>-" prefix becomes the order; anything else falls back to 0.
func parseSectionName(name string) (slug, title string, order int) {
	slug = name
	for i, r := range name {
		if r == '-' && i > 0 {
			prefix := name[:i]
			parsed := 0
			allDigits := true
			for _, pr := range prefix {
				if pr < '0' || pr > '9' {
					allDigits = false
					break
				}
				parsed = parsed*10 + int(pr-'0')
			}
			if allDigits {
				order = parsed
				slug = name[i+1:]
			}
			break
		}
	}
	title = titleFromSlug(slug)
	return slug, title, order
}

// isSafeSegment rejects URL segments that would let a request escape the
// pages directory. The "..", path-separator, and substring-".." checks
// together cover the absolute, relative, and concealed-traversal cases.
func isSafeSegment(segment string) bool {
	if segment == "" || segment == "." {
		return false
	}
	if strings.ContainsAny(segment, "/\\") {
		return false
	}
	return !strings.Contains(segment, "..")
}
