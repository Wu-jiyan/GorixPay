package epay

import (
	"path/filepath"
	"testing"
)

func TestManifestLoadsRequiredEntrypoints(t *testing.T) {
	manifest, err := LoadManifest(filepath.Join("manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Version != 1 {
		t.Fatalf("version = %d, want 1", manifest.Version)
	}
	if manifest.Routes.LegacySubmit.Path != "/submit.php" {
		t.Fatalf("legacy submit path = %q", manifest.Routes.LegacySubmit.Path)
	}
	if manifest.Routes.LegacyMAPI.Path != "/mapi.php" {
		t.Fatalf("legacy mapi path = %q", manifest.Routes.LegacyMAPI.Path)
	}
	if len(manifest.Routes.LegacyAPI.Operations) != 6 {
		t.Fatalf("legacy operations = %d, want 6", len(manifest.Routes.LegacyAPI.Operations))
	}
	if len(manifest.Routes.V2API.Operations) != 12 {
		t.Fatalf("v2 operations = %d, want 12", len(manifest.Routes.V2API.Operations))
	}
	if len(manifest.Notifications.V0.Fields) == 0 || len(manifest.Notifications.V1.Fields) == 0 {
		t.Fatal("notification fields are required")
	}
	if len(manifest.CompatibilityQuirks) == 0 {
		t.Fatal("compatibility quirks are required")
	}
}

func TestManifestSourcesAreRepositoryRelativeWithPositiveLines(t *testing.T) {
	manifest, err := LoadManifest("manifest.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range manifest.Sources() {
		if filepath.IsAbs(source.Path) {
			t.Errorf("source path is absolute: %q", source.Path)
		}
		if source.Line <= 0 {
			t.Errorf("source line is not positive: %q", source.Raw)
		}
	}
}
