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
