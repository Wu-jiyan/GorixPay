package epay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// upstreamRoot returns the local checkout of the upstream Epay source. It is not
// part of this repository: point EPAY_UPSTREAM_DIR at one, or drop a checkout at
// <repo>/Epay.
func upstreamRoot(repositoryRoot string) string {
	if configured := os.Getenv("EPAY_UPSTREAM_DIR"); configured != "" {
		return configured
	}
	return filepath.Join(repositoryRoot, "Epay")
}

// upstreamFile maps a manifest source path such as
// Epay/includes/lib/Payment.php onto the local checkout.
func upstreamFile(root, sourcePath string) string {
	relative := strings.TrimPrefix(filepath.ToSlash(sourcePath), "Epay/")
	return filepath.Join(root, filepath.FromSlash(relative))
}

func TestSourceTraceFilesAndLinesExist(t *testing.T) {
	manifest, err := LoadManifest("manifest.yaml")
	if err != nil {
		t.Fatal(err)
	}
	repositoryRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	root := upstreamRoot(repositoryRoot)
	if _, err := os.Stat(filepath.Join(root, "includes", "lib", "Payment.php")); err != nil {
		t.Skip("upstream Epay source not found, set EPAY_UPSTREAM_DIR to a checkout: " + err.Error())
	}
	for _, source := range manifest.Sources() {
		path := upstreamFile(root, source.Path)
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
