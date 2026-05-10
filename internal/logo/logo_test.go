package logo

import "testing"

func TestResolveDashboardIconsLogo(t *testing.T) {
	logo := Resolve("dashboardicons:argo-cd", "Argo CD", "manual")

	if logo.Kind != "image" ||
		logo.URL != "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/svg/argo-cd.svg" {
		t.Fatalf("unexpected dashboardicons logo: %#v", logo)
	}
}

func TestResolveSelfhstBackCompatLogo(t *testing.T) {
	logo := Resolve("selfhst:argo-cd", "Argo CD", "manual")

	if logo.Kind != "image" ||
		logo.URL != "https://cdn.jsdelivr.net/gh/selfhst/icons/svg/argo-cd.svg" {
		t.Fatalf("unexpected selfhst back-compat logo: %#v", logo)
	}
}

func TestResolveIconifyLogo(t *testing.T) {
	logo := Resolve("simple-icons:grafana", "Grafana", "manual")

	if logo.Kind != "image" || logo.URL != "https://api.iconify.design/simple-icons:grafana.svg" {
		t.Fatalf("unexpected logo: %#v", logo)
	}
}

func TestResolveURLLogo(t *testing.T) {
	logo := Resolve("https://example.com/logo.svg", "App", "manual")

	if logo.Kind != "image" || logo.URL != "https://example.com/logo.svg" {
		t.Fatalf("unexpected URL logo: %#v", logo)
	}
}

func TestResolveBareIconFallsBackToInitials(t *testing.T) {
	logo := Resolve("prometheus", "Prometheus", "")

	if logo.Kind != "text" || logo.Text != "P" {
		t.Fatalf("unexpected bare icon fallback: %#v", logo)
	}
}

func TestResolveStaticPathLogo(t *testing.T) {
	logo := Resolve("/static/logo.svg", "Internal App", "")

	if logo.Kind != "image" || logo.URL != "/static/logo.svg" {
		t.Fatalf("unexpected static path fallback: %#v", logo)
	}
}

func TestResolveDefaultSourceLogo(t *testing.T) {
	logo := Resolve("", "Unknown App", "prod-kubernetes")

	if logo.URL != "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/svg/kubernetes.svg" {
		t.Fatalf("unexpected source default logo: %#v", logo)
	}
}

func TestResolveDockerSourceLogo(t *testing.T) {
	logo := Resolve("", "Unknown App", "docker")

	if logo.URL != "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/svg/docker.svg" {
		t.Fatalf("unexpected docker source logo: %#v", logo)
	}
}

func TestResolveGenericSourceFallback(t *testing.T) {
	logo := Resolve("", "Unknown App", "custom")

	if logo.URL != "https://api.iconify.design/lucide:app-window.svg" {
		t.Fatalf("unexpected generic source logo: %#v", logo)
	}
}

func TestResolveInitialsFallback(t *testing.T) {
	logo := Resolve("", "Long App", "")

	if logo.Kind != "text" || logo.Text != "LA" {
		t.Fatalf("unexpected initials fallback: %#v", logo)
	}
}
