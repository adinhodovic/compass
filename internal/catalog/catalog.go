package catalog

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

//go:embed services.yaml
var embedded embed.FS

type Entry struct {
	Description string   `yaml:"description"`
	Icon        string   `yaml:"icon"`
	PrimaryTag  string   `yaml:"primary_tag"`
	Tags        []string `yaml:"tags"`
}

type DB map[string]Entry

func Load(path string) (DB, error) {
	data, err := embedded.ReadFile("services.yaml")
	if err != nil {
		return nil, err
	}
	db, err := parse(data)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(path) == "" {
		return db, nil
	}

	overridePaths, err := overrideFiles(path)
	if err != nil {
		return nil, err
	}
	for _, p := range overridePaths {
		overrideData, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("read catalog override %s: %w", p, err)
		}
		overrides, err := parse(overrideData)
		if err != nil {
			return nil, fmt.Errorf("parse catalog override %s: %w", p, err)
		}
		for slug, entry := range overrides {
			db[slug] = mergeEntry(db[slug], entry)
		}
	}

	return db, nil
}

// overrideFiles returns every *.yaml / *.yml file directly inside path,
// in lexical order. Path must be a directory; a single-file shape is no
// longer supported (split overrides into one file per concern).
func overrideFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat catalog path %s: %w", path, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("catalog path %s must be a directory", path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("read catalog dir %s: %w", path, err)
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		files = append(files, filepath.Join(path, entry.Name()))
	}
	sort.Strings(files)
	return files, nil
}

// mergeEntry copies non-zero fields from override onto base. Slice fields
// (Tags) are replaced wholesale, matching the per-field merge documented in
// catalog.md.
func mergeEntry(base, override Entry) Entry {
	if override.Description != "" {
		base.Description = override.Description
	}
	if override.Icon != "" {
		base.Icon = override.Icon
	}
	if override.PrimaryTag != "" {
		base.PrimaryTag = override.PrimaryTag
	}
	if len(override.Tags) > 0 {
		base.Tags = append([]string(nil), override.Tags...)
	}
	return base
}

func (db DB) Lookup(name string) (Entry, bool) {
	slug := Normalize(name)
	if entry, ok := db[slug]; ok {
		return entry, true
	}
	matches := make([]string, 0, len(db))
	for match := range db {
		if strings.Contains(slug, match) {
			matches = append(matches, match)
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if len(matches[i]) != len(matches[j]) {
			return len(matches[i]) > len(matches[j])
		}
		return matches[i] < matches[j]
	})
	if len(matches) > 0 {
		return db[matches[0]], true
	}
	return Entry{}, false
}

func parse(data []byte) (DB, error) {
	entries := map[string]Entry{}
	if err := yaml.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	db := make(DB, len(entries))
	for name, entry := range entries {
		db[Normalize(name)] = entry
	}
	return db, nil
}

// Normalize collapses a service name into the catalog key form: lowercase,
// alphanumeric only.
func Normalize(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var builder strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}
