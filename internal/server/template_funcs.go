package server

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"html/template"
	"slices"
	"strings"
	"time"

	"github.com/adinhodovic/compass/internal/compass"
	"github.com/adinhodovic/compass/internal/logo"
	"github.com/adinhodovic/compass/internal/registry"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// marshalJS encodes value as JSON for direct embedding in a <script> tag.
// Falls back to "[]" on error so the template never breaks. Used by
// commandIndex / serviceIDs / servicesJSON.
//
// Go's json.Marshal escapes <, >, & for HTML safety but leaves the JS
// line terminators U+2028/U+2029 raw — those break out of string literals
// in pre-ES2019 engines (and some embedded webviews still pin to older
// targets). Replace them with their \u escapes so a service name carrying
// U+2028 can't terminate the inline <script>.
func marshalJS(value any) template.JS {
	data, err := json.Marshal(value)
	if err != nil {
		return template.JS("[]")
	}
	data = bytes.ReplaceAll(data, []byte{0xe2, 0x80, 0xa8}, []byte(`\u2028`))
	data = bytes.ReplaceAll(data, []byte{0xe2, 0x80, 0xa9}, []byte(`\u2029`))
	return template.JS(data)
}

type metadataItem struct {
	Key   string
	Value string
	URL   bool
}

type serviceJSON struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	URL         string   `json:"url"`
	Source      string   `json:"source"`
	SourceType  string   `json:"sourceType"`
	SourceID    string   `json:"sourceID"`
	SourceLabel string   `json:"sourceLabel"`
	PrimaryTag  string   `json:"primaryTag"`
	Tags        []string `json:"tags"`
	Description string   `json:"description"`
	IconURL     string   `json:"iconURL,omitempty"`
	IconText    string   `json:"iconText,omitempty"`
}

// commandIndex builds the JSON payload that powers the global command palette.
// It's the union of services + pages with a `type` discriminator so the
// client-side code can group results.
func commandIndex(b Base) template.JS {
	type item struct {
		Type     string `json:"type"`
		Value    string `json:"value"`
		Label    string `json:"label"`
		Section  string `json:"section,omitempty"`
		Keywords string `json:"keywords,omitempty"`
	}
	out := make([]item, 0, len(b.Services))
	for _, s := range b.Services {
		out = append(out, item{
			Type:     "service",
			Value:    "/services/" + s.ID,
			Label:    s.Name,
			Section:  sourceLabel(s.SourceType, s.Source),
			Keywords: strings.Join(append([]string{s.Source, s.SourceType}, s.Tags...), " "),
		})
	}
	for _, section := range b.Pages {
		for _, p := range section.Pages {
			out = append(out, item{
				Type:    "page",
				Value:   p.URL(),
				Label:   p.Title,
				Section: section.Title,
			})
		}
	}
	return marshalJS(out)
}

// timeAgo renders a duration since t in a compact human form. Empty when t is
// the zero value.
func timeAgo(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours())/24)
	}
}

// sourceTypeLabel renders the human-readable label for a source's type.
func sourceTypeLabel(sourceType string) string {
	switch strings.ToLower(strings.TrimSpace(sourceType)) {
	case compass.SourceTypeKubernetes:
		return "Kubernetes"
	case compass.SourceTypeTailscale:
		return "Tailscale"
	case compass.SourceTypeStatic:
		return "Static"
	case compass.SourceTypeAPI:
		return "API"
	case "":
		return "Unknown Source"
	default:
		return titleWords(sourceType)
	}
}

// sourceLabel renders the type label for a source. If sourceName differs from
// the type, the name is appended; otherwise it's hidden as redundant.
func sourceLabel(sourceType, sourceName string) string {
	typeLabel := sourceTypeLabel(sourceType)
	name := strings.TrimSpace(sourceName)
	if name == "" || strings.EqualFold(name, sourceType) {
		return typeLabel
	}
	return typeLabel + " · " + titleWords(name)
}

func groupLabel(group string, mode string, services []compass.Service) string {
	if group == "untagged" {
		return "Untagged Services"
	}
	if mode == compass.GroupByTags {
		return titleWords(group)
	}
	if len(services) > 0 {
		return sourceLabel(services[0].SourceType, services[0].Source)
	}
	return titleWords(group)
}

func serviceIDs(services []compass.Service) template.JS {
	ids := make([]string, 0, len(services))
	for _, service := range services {
		ids = append(ids, service.ID)
	}
	return marshalJS(ids)
}

// sourceNames emits the per-source picker options as a JSON array of
// {value, label} pairs. Value is the canonical "<type>/<name>" so two
// sources sharing a Name don't collide in the filter; label uses the
// human "<Type> · <Name>" form for display.
func sourceNames(statuses []registry.SourceStatus) template.JS {
	type option struct {
		Value string `json:"value"`
		Label string `json:"label"`
	}
	out := make([]option, 0, len(statuses))
	for _, s := range statuses {
		out = append(out, option{
			Value: s.ID(),
			Label: sourceLabel(s.Type, s.Name),
		})
	}
	return marshalJS(out)
}

func titleWords(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '-' || r == '_' || r == ' '
	})
	title := cases.Title(language.Und)
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = title.String(strings.ToLower(part))
	}

	return strings.Join(parts, " ")
}

func metadataItems(metadata map[string]any) []metadataItem {
	items := make([]metadataItem, 0, len(metadata))
	for key, value := range metadata {
		formatted := metadataValue(value)
		isURL := strings.HasPrefix(formatted, "http://") || strings.HasPrefix(formatted, "https://")
		items = append(items, metadataItem{
			Key:   titleWords(key),
			Value: formatted,
			URL:   isURL,
		})
	}
	slices.SortFunc(items, func(a, b metadataItem) int {
		return cmp.Compare(a.Key, b.Key)
	})

	return items
}

func metadataValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case []string:
		return strings.Join(typed, ", ")
	case []any, map[string]any, map[string]string:
		data, err := json.MarshalIndent(typed, "", "  ")
		if err == nil {
			return string(data)
		}
	}

	return fmt.Sprint(value)
}

func sub(a, b int) int {
	return max(a-b, 0)
}

func servicesJSON(services []compass.Service) template.JS {
	items := make([]serviceJSON, 0, len(services))
	for _, service := range services {
		// Pre-resolve the icon so client-side chips (Pinned, Recent,
		// command palette) can render an avatar without duplicating the
		// resolution logic in JS.
		resolved := logo.Resolve(service.Icon, service.Name, service.SourceType)
		entry := serviceJSON{
			ID:          service.ID,
			Name:        service.Name,
			URL:         service.URL,
			Source:      service.Source,
			SourceType:  service.SourceType,
			SourceID:    service.SourceID(),
			SourceLabel: sourceLabel(service.SourceType, service.Source),
			PrimaryTag:  service.PrimaryTag,
			Tags:        service.Tags,
			Description: service.Description,
		}
		if resolved.Kind == "image" {
			entry.IconURL = resolved.URL
		} else {
			entry.IconText = resolved.Text
		}
		items = append(items, entry)
	}
	return marshalJS(items)
}
