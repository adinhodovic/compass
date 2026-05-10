package logo

import (
	"fmt"
	"html/template"
	"strings"

	"github.com/adinhodovic/compass/internal/compass"
)

// IconHTML emits an iconify web-component reference. Shared between the
// server templates and the pages package so embedded service cards look
// identical to dashboard cards.
func IconHTML(name string) template.HTML {
	return template.HTML(fmt.Sprintf(
		`<iconify-icon class="inline-block align-middle" icon="%s" width="16" height="16" aria-hidden="true"></iconify-icon>`,
		template.HTMLEscapeString(name),
	))
}

const (
	iconifyBaseURL        = "https://api.iconify.design/"
	dashboardiconsBaseURL = "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/svg/"
	selfhstBaseURL        = "https://cdn.jsdelivr.net/gh/selfhst/icons/svg/"
)

type Logo struct {
	Kind string
	URL  string
	Text string
}

// Resolve picks a logo for a service based on the explicit icon override, the
// originating source type, and finally the service name (initials).
//
// The catalog is the source of truth for default icons: registry.normalize is
// expected to backfill service.Icon from the catalog before this function
// runs. If callers do not have a registry-normalized service, they can pass an
// empty icon and the source-type fallback or initials will be used.
func Resolve(icon, name, source string) Logo {
	icon = strings.TrimSpace(icon)
	if icon == "" {
		icon = sourceIcon(source)
	}

	switch {
	case strings.HasPrefix(icon, "http://"),
		strings.HasPrefix(icon, "https://"),
		strings.HasPrefix(icon, "/"):
		return Logo{Kind: "image", URL: icon}
	case strings.HasPrefix(icon, "dashboardicons:"):
		return Logo{
			Kind: "image",
			URL:  dashboardiconsBaseURL + strings.TrimPrefix(icon, "dashboardicons:") + ".svg",
		}
	case strings.HasPrefix(icon, "selfhst:"):
		// Back-compat: selfhst: keeps pointing at selfh.st's CDN.
		// dashboardicons: is the preferred prefix going forward.
		return Logo{
			Kind: "image",
			URL:  selfhstBaseURL + strings.TrimPrefix(icon, "selfhst:") + ".svg",
		}
	case strings.Contains(icon, ":"):
		return Logo{
			Kind: "image",
			URL:  iconifyBaseURL + strings.TrimPrefix(icon, "iconify:") + ".svg",
		}
	default:
		return Logo{Kind: "text", Text: initials(name)}
	}
}

func sourceIcon(source string) string {
	source = strings.ToLower(strings.TrimSpace(source))
	switch {
	case strings.Contains(source, compass.SourceTypeDocker):
		return "dashboardicons:docker"
	case strings.Contains(source, compass.SourceTypeKubernetes),
		strings.Contains(source, "cluster"):
		return "dashboardicons:kubernetes"
	case strings.Contains(source, compass.SourceTypeTailscale), strings.Contains(source, "tailnet"):
		return "dashboardicons:tailscale"
	case strings.Contains(source, compass.SourceTypeAPI):
		return "lucide:plug"
	case strings.Contains(source, compass.SourceTypeStatic), strings.Contains(source, "manual"):
		return "lucide:app-window"
	case source != "":
		return "lucide:app-window"
	default:
		return ""
	}
}

func initials(name string) string {
	fields := strings.Fields(strings.TrimSpace(name))
	if len(fields) == 0 {
		return "?"
	}
	if len(fields) == 1 {
		return strings.ToUpper(string([]rune(fields[0])[0]))
	}
	return strings.ToUpper(string([]rune(fields[0])[0]) + string([]rune(fields[1])[0]))
}
