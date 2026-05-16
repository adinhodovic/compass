package meta

import "testing"

func TestPanelsFromAnnotationsUsesGrafanaPanelsOnly(t *testing.T) {
	panels := PanelsFromAnnotations(map[string]string{
		AnnotationGrafanaPanels: "CPU=https://grafana.local/d-solo/cpu?panelId=1,invalid,Memory=not-a-url",
	})

	if len(panels) != 1 {
		t.Fatalf("expected one valid panel, got %#v", panels)
	}
	if panels[0].Title != "CPU" || panels[0].URL != "https://grafana.local/d-solo/cpu?panelId=1" {
		t.Fatalf("unexpected panel: %#v", panels[0])
	}
}

func TestLinksFromAnnotations(t *testing.T) {
	links := LinksFromAnnotations(map[string]string{
		AnnotationLinks: "lucide:heart-pulse|Health=https://grafana.local/api/health,Runbook=/pages/on-call/grafana,Bad=ftp://bad.local,invalid",
	})

	if len(links) != 2 {
		t.Fatalf("expected two valid links, got %#v", links)
	}
	if links[0].Icon != "lucide:heart-pulse" || links[0].Label != "Health" ||
		links[0].URL != "https://grafana.local/api/health" {
		t.Fatalf("unexpected first link: %#v", links[0])
	}
	if links[1].Label != "Runbook" || links[1].URL != "/pages/on-call/grafana" {
		t.Fatalf("unexpected second link: %#v", links[1])
	}
}
