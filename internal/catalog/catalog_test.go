package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefaultCatalog(t *testing.T) {
	db, err := Load("")
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	entry, ok := db.Lookup("Grafana")
	if !ok || entry.Description == "" {
		t.Fatal("expected default Grafana description")
	}
}

func TestLoadRejectsFilePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.yaml")
	if err := os.WriteFile(path, []byte("grafana: {description: x}\n"), 0o600); err != nil {
		t.Fatalf("write override: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected single-file catalog path to be rejected")
	}
}

func TestLoadOverrideDirectoryMergesAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	icons := []byte(strings.Join([]string{
		"my-service:",
		"  icon: simple-icons:cloudflare",
		"  primary_tag: network",
		"  tags: [core, network]",
		"",
	}, "\n"))
	descriptions := []byte(strings.Join([]string{
		"my-service:",
		"  description: Internal app.",
		"  tags: [internal]",
		"",
	}, "\n"))
	if err := os.WriteFile(filepath.Join(dir, "01-icons.yaml"), icons, 0o600); err != nil {
		t.Fatalf("write icons: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, "02-descriptions.yaml"),
		descriptions,
		0o600,
	); err != nil {
		t.Fatalf("write descriptions: %v", err)
	}

	db, err := Load(dir)
	if err != nil {
		t.Fatalf("load catalog dir: %v", err)
	}
	entry, ok := db.Lookup("My Service")
	if !ok {
		t.Fatal("expected my-service entry to exist")
	}
	if entry.Icon != "simple-icons:cloudflare" {
		t.Fatalf("expected icon from icons.yaml to remain, got %q", entry.Icon)
	}
	if entry.Description != "Internal app." {
		t.Fatalf("expected description from descriptions.yaml, got %q", entry.Description)
	}
	if entry.PrimaryTag != "network" {
		t.Fatalf("expected primary tag from icons.yaml to remain, got %q", entry.PrimaryTag)
	}
	if len(entry.Tags) != 1 || entry.Tags[0] != "internal" {
		t.Fatalf("expected later-file tags to win, got %v", entry.Tags)
	}
}

func TestLoadOverrideDirectoryDeepMergesEmbeddedEntry(t *testing.T) {
	dir := t.TempDir()
	override := []byte(strings.Join([]string{
		"grafana:",
		"  tags: [core]",
		"",
	}, "\n"))
	if err := os.WriteFile(filepath.Join(dir, "tags.yaml"), override, 0o600); err != nil {
		t.Fatalf("write override: %v", err)
	}

	db, err := Load(dir)
	if err != nil {
		t.Fatalf("load catalog dir: %v", err)
	}
	entry, ok := db.Lookup("Grafana")
	if !ok {
		t.Fatal("expected grafana entry to exist")
	}
	if entry.Description == "" {
		t.Fatal("expected embedded description to be preserved")
	}
	if len(entry.Tags) != 1 || entry.Tags[0] != "core" {
		t.Fatalf("expected merged tags, got %v", entry.Tags)
	}
}
