package meta

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/adinhodovic/compass/internal/compass"
)

const (
	AnnotationPrefix        = "compass.adinhodovic.com/"
	AnnotationTags          = AnnotationPrefix + "tags"
	AnnotationDescription   = AnnotationPrefix + "description"
	AnnotationIcon          = AnnotationPrefix + "icon"
	AnnotationName          = AnnotationPrefix + "name"
	AnnotationPrimaryTag    = AnnotationPrefix + "primary-tag"
	AnnotationURLs          = AnnotationPrefix + "urls"
	AnnotationGrafanaPanels = AnnotationPrefix + "grafana-panels"
	LabelEnabled            = AnnotationPrefix + "enabled"
)

// StringFromPath returns the scalar string value at path. Composite values
// (maps, slices) deliberately return "" so that a misconfigured mapping path
// can't dump the entire item into a service field.
func StringFromPath(item any, path string) string {
	value := LookupPath(item, path)
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case bool, int, int64, float64:
		return fmt.Sprint(typed)
	default:
		return ""
	}
}

// StringSliceFromPath returns a string list at path. Strings are split as a
// comma-separated list; scalar values are ignored so misconfigured paths do not
// accidentally leak object dumps into service tags.
func StringSliceFromPath(item any, path string) []string {
	value := LookupPath(item, path)
	return StringSlice(value)
}

func StringSlice(value any) []string {
	var raw []string
	switch typed := value.(type) {
	case []any:
		raw = make([]string, len(typed))
		for i, item := range typed {
			raw[i] = fmt.Sprint(item)
		}
	case []string:
		raw = typed
	case string:
		return CommaList(typed)
	default:
		return nil
	}
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

// DefaultBool returns fallback when value is nil.
func DefaultBool(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

// EnabledByLabel applies the compass.adinhodovic.com/enabled gate shared by source
// implementations. Empty labels follow the source's auto-discovery default.
func EnabledByLabel(labels map[string]string, autoDiscoverAll bool) bool {
	value := strings.TrimSpace(labels[LabelEnabled])
	if value == "" {
		return autoDiscoverAll
	}
	enabled, err := strconv.ParseBool(value)
	return err == nil && enabled
}

// WithScheme prepends scheme:// when value has no URL scheme.
func WithScheme(value, scheme string) string {
	value = strings.TrimSpace(value)
	scheme = strings.TrimSpace(scheme)
	if value == "" || strings.Contains(value, "://") || scheme == "" {
		return value
	}
	return scheme + "://" + value
}

// ValidHTTPURL returns a normalized HTTP(S) URL and whether it is safe to use
// as a service link or iframe source.
func ValidHTTPURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	return u.String(), true
}

// MergeTags returns base ++ extra with whitespace trimmed, blanks dropped,
// and duplicates removed. Order is preserved (base first, then extra).
// Returns nil when both inputs are empty.
func MergeTags(base, extra []string) []string {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(base)+len(extra))
	merged := make([]string, 0, len(base)+len(extra))
	for _, group := range [][]string{base, extra} {
		for _, t := range group {
			t = strings.TrimSpace(t)
			if t != "" && !seen[t] {
				seen[t] = true
				merged = append(merged, t)
			}
		}
	}
	return merged
}

func LookupPath(value any, path string) any {
	if path == "" || path == "$" {
		return value
	}
	path = strings.TrimPrefix(path, "$.")
	path = strings.TrimPrefix(path, ".")
	current := value
	for _, part := range strings.Split(path, ".") {
		if part == "" {
			continue
		}
		switch typed := current.(type) {
		case map[string]any:
			current = typed[part]
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(typed) {
				return nil
			}
			current = typed[index]
		case []string:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(typed) {
				return nil
			}
			current = typed[index]
		default:
			return nil
		}
	}

	return current
}

func CommaList(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}

	return result
}

// URLEntry is one item from a multi-URL annotation. Title is empty when the
// entry was given as a bare URL.
type URLEntry struct {
	Title string
	URL   string
}

// URLEntriesFromAnnotation parses the AnnotationURLs value into one or more
// entries. Format: [Title=]URL,[Title=]URL,... Bare URLs are accepted (no
// Title=); a single bare URL preserves the legacy single-card behaviour.
//
// "Title=" parsing is skipped when the part before "=" looks like a URL
// scheme, so query strings like "https://x?foo=bar" don't get mistaken for a
// title.
func URLEntriesFromAnnotation(value string) []URLEntry {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	entries := make([]URLEntry, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		title, raw, ok := strings.Cut(part, "=")
		if !ok || strings.Contains(title, "://") {
			entries = append(entries, URLEntry{URL: part})
			continue
		}
		title = strings.TrimSpace(title)
		raw = strings.TrimSpace(raw)
		if title == "" || raw == "" {
			continue
		}
		entries = append(entries, URLEntry{Title: title, URL: raw})
	}
	return entries
}

func PanelsFromAnnotations(annotations map[string]string) []compass.Panel {
	if annotations[AnnotationGrafanaPanels] == "" {
		return nil
	}

	entries := strings.Split(annotations[AnnotationGrafanaPanels], ",")
	panels := make([]compass.Panel, 0, len(entries))
	for _, entry := range entries {
		title, panelURL, ok := strings.Cut(strings.TrimSpace(entry), "=")
		if !ok || title == "" || panelURL == "" {
			continue
		}
		panelURL, ok = ValidHTTPURL(panelURL)
		if !ok {
			continue
		}
		panels = append(panels, compass.Panel{Title: title, URL: panelURL})
	}

	return panels
}
