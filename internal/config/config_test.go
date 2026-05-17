package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := writeConfig(t, `
organization:
  name: Compass
unknown: true
services:
  sources:
    - type: static
      name: manual
      services:
        - name: Grafana
          url: https://grafana.local
`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "field unknown not found") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestLoadRejectsUnsafePrimaryColor(t *testing.T) {
	path := writeConfig(t, `
organization:
  name: Compass
ui:
  primary_color: "#2563eb; color: red"
services:
  sources:
    - type: static
      name: manual
      services:
        - name: Grafana
          url: https://grafana.local
`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "ui.primary_color") {
		t.Fatalf("expected primary color validation error, got %v", err)
	}
}

func TestLoadRejectsInvalidTrustedProxy(t *testing.T) {
	path := writeConfig(t, `
organization:
  name: Compass
auth:
  trusted_proxies:
    - not-an-ip
services:
  sources:
    - type: static
      name: manual
      services:
        - name: Grafana
          url: https://grafana.local
`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "auth.trusted_proxies") {
		t.Fatalf("expected trusted proxy validation error, got %v", err)
	}
}

func TestLoadAcceptsValidPrimaryColorAndTrustedProxy(t *testing.T) {
	path := writeConfig(t, `
organization:
  name: Compass
ui:
  primary_color: "oklch(55% 0.18 250)"
auth:
  trusted_proxies:
    - 10.0.0.0/8
    - 192.168.1.10
services:
  sources:
    - type: static
      name: manual
      services:
        - name: Grafana
          url: https://grafana.local
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.UI.PrimaryColor != "oklch(55% 0.18 250)" {
		t.Fatalf("unexpected primary color: %q", cfg.UI.PrimaryColor)
	}
}

func TestLoadRejectsBlankSourceName(t *testing.T) {
	path := writeConfig(t, `
organization:
  name: Compass
services:
  sources:
    - type: static
      services:
        - name: Grafana
          url: https://grafana.local
`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "services.sources[0]: name is required") {
		t.Fatalf("expected source name validation error, got %v", err)
	}
}

func TestLoadRejectsDuplicateSourceIdentity(t *testing.T) {
	path := writeConfig(t, `
organization:
  name: Compass
services:
  sources:
    - type: static
      name: manual
      services:
        - name: Grafana
          url: https://grafana.local
    - type: static
      name: manual
      services:
        - name: Prometheus
          url: https://prometheus.local
`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), `duplicate source identity "static/manual"`) {
		t.Fatalf("expected duplicate source identity error, got %v", err)
	}
}

func TestLoadAllowsSameSourceNameAcrossTypes(t *testing.T) {
	path := writeConfig(t, `
organization:
  name: Compass
services:
  sources:
    - type: static
      name: local
      services:
        - name: Grafana
          url: https://grafana.local
    - type: api
      name: local
      endpoint: https://example.test/services
`)

	if _, err := Load(path); err != nil {
		t.Fatalf("load: %v", err)
	}
}

func TestLoadAcceptsNestedHeaderLinks(t *testing.T) {
	path := writeConfig(t, `
organization:
  name: Compass
header_links:
  - label: Tools
    icon: lucide:wrench
    links:
      - label: Grafana
        url: https://grafana.local
      - label: Runbook
        url: /pages/runbook
services:
  sources:
    - type: static
      name: manual
      services:
        - name: Grafana
          url: https://grafana.local
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.HeaderLinks) != 1 || len(cfg.HeaderLinks[0].Links) != 2 {
		t.Fatalf("unexpected header links: %#v", cfg.HeaderLinks)
	}
	if cfg.HeaderLinks[0].Links[0].Label != "Grafana" ||
		!cfg.HeaderLinks[0].Links[0].OpensInNewTab() {
		t.Fatalf("unexpected first child link: %#v", cfg.HeaderLinks[0].Links[0])
	}
	if cfg.HeaderLinks[0].Links[1].OpensInNewTab() {
		t.Fatalf("internal child link should not open in new tab: %#v", cfg.HeaderLinks[0].Links[1])
	}
}

func TestLoadAcceptsServiceLinks(t *testing.T) {
	path := writeConfig(t, `
organization:
  name: Compass
services:
  sources:
    - type: static
      name: manual
      services:
        - name: Grafana
          url: https://grafana.local
          links:
            - label: Health
              url: https://grafana.local/api/health
              icon: lucide:heart-pulse
            - label: Runbook
              url: /pages/on-call/grafana
              icon: lucide:book-open
              new_tab: false
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	links := cfg.Services.Sources[0].Services[0].Links
	if len(links) != 2 || links[0].Label != "Health" || !links[0].OpensInNewTab() {
		t.Fatalf("unexpected service links: %#v", links)
	}
	if links[1].OpensInNewTab() {
		t.Fatalf("expected new_tab=false to be honored: %#v", links[1])
	}
}

func TestLoadAcceptsOrganizationFullLogo(t *testing.T) {
	path := writeConfig(t, `
organization:
  name: Compass
  logo: lucide:compass
  full_logo: https://example.com/compass-wordmark.svg
services:
  sources:
    - type: static
      name: manual
      services:
        - name: Grafana
          url: https://grafana.local
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Organization.FullLogo != "https://example.com/compass-wordmark.svg" {
		t.Fatalf("unexpected full logo: %q", cfg.Organization.FullLogo)
	}
}

func TestLoadPreservesBcryptDollarHash(t *testing.T) {
	path := writeConfig(t, `
organization:
  name: Compass
auth:
  basic:
    users:
      - name: admin
        password_hash: $2a$10$He3AU65PBOkfE3oeq0dRxuYvEbBkWECslj3JchYXRVAAqoA6FIaAu
services:
  sources:
    - type: static
      name: manual
      services:
        - name: Grafana
          url: https://grafana.local
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := "$2a$10$He3AU65PBOkfE3oeq0dRxuYvEbBkWECslj3JchYXRVAAqoA6FIaAu"
	if cfg.Auth.Basic.Users[0].PasswordHash != want {
		t.Fatalf("expected hash %q, got %q", want, cfg.Auth.Basic.Users[0].PasswordHash)
	}
}

func TestLoadExpandsBracedEnvironmentOnly(t *testing.T) {
	t.Setenv("COMPASS_TEST_URL", "https://grafana.local")
	path := writeConfig(t, `
organization:
  name: Compass
services:
  sources:
    - type: static
      name: manual
      services:
        - name: Grafana
          url: ${COMPASS_TEST_URL}
          metadata:
            literal: $COMPASS_TEST_URL
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	service := cfg.Services.Sources[0].Services[0]
	if service.URL != "https://grafana.local" {
		t.Fatalf("expected braced env expansion, got %q", service.URL)
	}
	if service.Metadata["literal"] != "$COMPASS_TEST_URL" {
		t.Fatalf(
			"expected bare env reference to stay literal, got %#v",
			service.Metadata["literal"],
		)
	}
}

func TestLoadRejectsUnsetEnvVar(t *testing.T) {
	if err := os.Unsetenv("COMPASS_TEST_MISSING"); err != nil {
		t.Fatalf("unsetenv: %v", err)
	}
	path := writeConfig(t, `
organization:
  name: Compass
services:
  sources:
    - type: static
      name: manual
      services:
        - name: Grafana
          url: ${COMPASS_TEST_MISSING}
`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "COMPASS_TEST_MISSING") {
		t.Fatalf("expected unset env error mentioning the variable, got %v", err)
	}
}

func TestLoadDefaultsDebugEnabled(t *testing.T) {
	path := writeConfig(t, `
organization:
  name: Compass
services:
  sources:
    - type: static
      name: manual
      services:
        - name: Grafana
          url: https://grafana.local
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.Debug.IsEnabled() {
		t.Fatalf("expected debug enabled by default")
	}
}

func TestLoadHonorsDebugDisabled(t *testing.T) {
	path := writeConfig(t, `
organization:
  name: Compass
debug:
  enabled: false
services:
  sources:
    - type: static
      name: manual
      services:
        - name: Grafana
          url: https://grafana.local
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Debug.IsEnabled() {
		t.Fatalf("expected debug disabled when set to false")
	}
}

func TestLoadAcceptsAssetsDir(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, `
organization:
  name: Compass
assets:
  dir: `+dir+`
services:
  sources:
    - type: static
      name: manual
      services:
        - name: Grafana
          url: https://grafana.local
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Assets.Dir != dir {
		t.Fatalf("unexpected assets config: %#v", cfg.Assets)
	}
}

func TestLoadRejectsMissingAssetsDir(t *testing.T) {
	path := writeConfig(t, `
organization:
  name: Compass
assets:
  dir: /does/not/exist
services:
  sources:
    - type: static
      name: manual
      services:
        - name: Grafana
          url: https://grafana.local
`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "assets.dir") {
		t.Fatalf("expected assets.dir validation error, got %v", err)
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "compass.yaml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
