package docker

import (
	"context"
	"errors"
	"testing"

	"github.com/adinhodovic/compass/internal/compass"
	"github.com/adinhodovic/compass/internal/source/meta"
	dockercontainer "github.com/docker/docker/api/types/container"
)

type fakeDockerClient struct {
	containers []dockercontainer.Summary
	options    dockercontainer.ListOptions
	err        error
}

func (m *fakeDockerClient) ContainerList(
	_ context.Context,
	options dockercontainer.ListOptions,
) ([]dockercontainer.Summary, error) {
	m.options = options
	if m.err != nil {
		return nil, m.err
	}
	return m.containers, nil
}

func newTestSource(autoDiscover, includeStopped bool, c dockerClient) Source {
	return Source{
		name:            compass.SourceTypeDocker,
		host:            "unix:///var/run/docker.sock",
		tags:            []string{compass.SourceTypeDocker},
		autoDiscoverAll: autoDiscover,
		includeStopped:  includeStopped,
		urlScheme:       "https",
		client:          c,
	}
}

func TestDockerLoadFiltersByEnabledLabel(t *testing.T) {
	containers := []dockercontainer.Summary{
		{ID: "a", Names: []string{"/grafana"}, Labels: map[string]string{
			meta.LabelEnabled:         "true",
			meta.AnnotationURLs:       "https://grafana.local",
			meta.AnnotationName:       "Grafana",
			meta.AnnotationPrimaryTag: "monitoring",
			meta.AnnotationTags:       "core,monitoring",
			meta.AnnotationLinks:      "lucide:heart-pulse|Health=https://grafana.local/api/health,Runbook=/pages/on-call/grafana",
		}},
		{ID: "b", Names: []string{"/redis"}, Labels: map[string]string{
			meta.AnnotationURLs: "https://redis.local",
		}},
	}
	src := newTestSource(
		false,
		false,
		&fakeDockerClient{containers: containers},
	)

	services, err := src.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("expected one service, got %d", len(services))
	}
	if services[0].Name != "Grafana" || services[0].URL != "https://grafana.local" {
		t.Fatalf("unexpected service: %#v", services[0])
	}
	if services[0].PrimaryTag != "monitoring" {
		t.Fatalf("expected primary tag from label, got %q", services[0].PrimaryTag)
	}
	if len(services[0].Links) != 2 || services[0].Links[0].Label != "Health" ||
		services[0].Links[1].URL != "/pages/on-call/grafana" {
		t.Fatalf("unexpected links from label: %#v", services[0].Links)
	}
}

func TestDockerLoadAutoDiscoverPicksContainersWithURLLabel(t *testing.T) {
	containers := []dockercontainer.Summary{
		{ID: "a", Names: []string{"/grafana"}, Labels: map[string]string{
			meta.AnnotationURLs: "https://grafana.local",
		}},
		{ID: "b", Names: []string{"/postgres"}, Labels: map[string]string{}},
	}
	src := newTestSource(true, false, &fakeDockerClient{containers: containers})

	services, err := src.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("expected one service (the one with a URL label), got %d", len(services))
	}
	if services[0].Name != "grafana" {
		t.Fatalf("expected name from container name, got %q", services[0].Name)
	}
}

func TestDockerLoadUsesComposeServiceForNameAndProjectAsTag(t *testing.T) {
	containers := []dockercontainer.Summary{
		{ID: "a", Names: []string{"/monitoring-grafana-1"}, Labels: map[string]string{
			meta.LabelEnabled:   "true",
			meta.AnnotationURLs: "https://grafana.local",
			composeProjectLabel: "monitoring",
			composeServiceLabel: "grafana",
		}},
	}
	src := newTestSource(
		false,
		false,
		&fakeDockerClient{containers: containers},
	)

	services, err := src.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("expected one service, got %d", len(services))
	}
	if services[0].Name != "grafana" {
		t.Fatalf("expected name from compose service label, got %q", services[0].Name)
	}
	hasMonitoring := false
	for _, tag := range services[0].Tags {
		if tag == "monitoring" {
			hasMonitoring = true
		}
	}
	if !hasMonitoring {
		t.Fatalf("expected compose project tag, got tags %v", services[0].Tags)
	}
}

func TestDockerLoadFallsBackToTraefikHostLabel(t *testing.T) {
	containers := []dockercontainer.Summary{
		{ID: "a", Names: []string{"/whoami"}, Labels: map[string]string{
			"traefik.enable":                          "true",
			"traefik.http.routers.admin.rule":         "Host(`admin.localhost`) && PathPrefix(`/admin`)",
			"traefik.http.routers.api.rule":           "Host(`api.localhost`, `api-alt.localhost`) && PathPrefix(`/v1`)",
			"traefik.http.routers.whoami.rule":        "Host(`whoami.localhost`) && PathPrefix(`/`)",
			"traefik.http.routers.whoami.entrypoints": "web",
			"traefik.tcp.routers.whoami.rule":         "HostSNI(`tcp.localhost`)",
		}},
	}
	src := newTestSource(true, false, &fakeDockerClient{containers: containers})
	src.urlScheme = "http"

	services, err := src.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(services) != 4 {
		t.Fatalf("expected four services from traefik label fallback, got %d", len(services))
	}
	wantURLs := []string{
		"http://admin.localhost",
		"http://api.localhost",
		"http://api-alt.localhost",
		"http://whoami.localhost",
	}
	for i, want := range wantURLs {
		if services[i].URL != want {
			t.Fatalf("service %d URL = %q, want %q", i, services[i].URL, want)
		}
	}
	if services[0].Name != "whoami · admin.localhost" {
		t.Fatalf(
			"expected multi-host service name to include host suffix, got %q",
			services[0].Name,
		)
	}
}

func TestDockerLoadPropagatesClientError(t *testing.T) {
	src := newTestSource(true, false, &fakeDockerClient{err: errors.New("dial fail")})

	if _, err := src.Load(context.Background()); err == nil {
		t.Fatal("expected error on client failure")
	}
}

func TestNormalizeHost(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"/var/run/docker.sock", "unix:///var/run/docker.sock"},
		{"unix:///var/run/docker.sock", "unix:///var/run/docker.sock"},
		{"tcp://localhost:2375", "tcp://localhost:2375"},
	}
	for _, tc := range cases {
		got, err := normalizeHost(tc.input)
		if err != nil {
			t.Fatalf("normalizeHost(%q): %v", tc.input, err)
		}
		if got != tc.want {
			t.Fatalf("normalizeHost(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestDockerLoadMultiURLAnnotationFansOut(t *testing.T) {
	containers := []dockercontainer.Summary{
		{ID: "abc", Names: []string{"/grafana"}, Labels: map[string]string{
			meta.LabelEnabled:   "true",
			meta.AnnotationURLs: "Public=https://grafana.example.com,Internal=https://grafana.internal",
		}},
	}
	src := newTestSource(false, false, &fakeDockerClient{containers: containers})

	services, err := src.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(services) != 2 {
		t.Fatalf("expected 2 services from multi-URL fan-out, got %d: %#v", len(services), services)
	}
	if services[0].Name != "Public" || services[0].URL != "https://grafana.example.com" {
		t.Fatalf("unexpected first service: %#v", services[0])
	}
	if services[1].Name != "Internal" || services[1].URL != "https://grafana.internal" {
		t.Fatalf("unexpected second service: %#v", services[1])
	}
	if services[0].ID == services[1].ID {
		t.Fatalf("expected distinct IDs, both %q", services[0].ID)
	}
}

func TestDockerListContainersIncludeStopped(t *testing.T) {
	client := &fakeDockerClient{}
	src := Source{
		name:           compass.SourceTypeDocker,
		host:           "unix:///var/run/docker.sock",
		tags:           []string{compass.SourceTypeDocker},
		includeStopped: true,
		client:         client,
	}

	if _, err := src.Load(context.Background()); err != nil {
		t.Fatalf("load: %v", err)
	}
	if !client.options.All {
		t.Fatal("expected All=true when includeStopped is set")
	}
}
