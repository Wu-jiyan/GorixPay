package epay

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSourceTraceFilesAndLinesExist(t *testing.T) {
	manifest, err := LoadManifest("manifest.yaml")
	if err != nil {
		t.Fatal(err)
	}
	repositoryRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	for _, source := range manifest.Sources() {
		path := filepath.Join(repositoryRoot, source.Path)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("source %s: %v", source.Raw, err)
			continue
		}
		lines := 1
		for _, value := range data {
			if value == '\n' {
				lines++
			}
		}
		if source.Line > lines {
			t.Errorf("source %s exceeds file length %d", source.Raw, lines)
		}
	}
}

func TestCompatibilityQuirksHaveDisposition(t *testing.T) {
	manifest, err := LoadManifest("manifest.yaml")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, values := range manifest.CompatibilityDisposition {
		for _, value := range values {
			if seen[value] {
				t.Errorf("duplicate compatibility disposition for %s", value)
			}
			seen[value] = true
		}
	}
	for _, quirk := range manifest.CompatibilityQuirks {
		if !seen[quirk] {
			t.Errorf("compatibility quirk %s has no disposition", quirk)
		}
	}
	for _, category := range []string{"strict_legacy", "configurable_legacy", "forbidden_in_v1"} {
		if len(manifest.CompatibilityDisposition[category]) == 0 {
			t.Errorf("compatibility disposition category %s is empty", category)
		}
	}
}
