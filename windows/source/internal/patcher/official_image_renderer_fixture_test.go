package patcher

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestOfficialIDEImageRendererWhenFixturePresent validates the exact renderer
// bytes from an official IDE application without modifying the installation.
// The app root must be the directory that directly contains out/ and
// product.json (resources/app on Windows or Contents/Resources/app on macOS).
func TestOfficialIDEImageRendererWhenFixturePresent(t *testing.T) {
	root := os.Getenv("ANTIGRAVITY_WF_TEST_IDE_APP_ROOT")
	if root == "" {
		t.Skip("official IDE renderer fixture is not configured")
	}
	paths := []string{
		filepath.Join(root, "out", "jetskiAgent", "main.js"),
		filepath.Join(root, "out", "vs", "workbench", "workbench.desktop.main.js"),
	}
	dedupeSeen := false
	for _, path := range paths {
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		updated, result := patchImagePreviewRenderer(string(original))
		if !result.Recognized {
			t.Fatalf("official IDE renderer is not safely recognized: %s %#v", path, result)
		}
		// A user's real installation may already contain this exact current
		// patch. Idempotent no-change is a valid fixture state; readiness and
		// syntax are still checked below against a temporary candidate.
		if !windowsImageRendererReady([]byte(updated)) {
			t.Fatalf("official IDE renderer is not ready after patch planning: %s %#v", path, result)
		}
		t.Logf("%s changed=%t markers: preview=%t native=%t ui=%t dedupe=%t", filepath.Base(path), result.Changed,
			strings.Contains(updated, imagePreviewPatchMarker), strings.Contains(updated, imagePreviewNativeCompatibleMarker),
			strings.Contains(updated, imageGenerationUIPatchMarker), strings.Contains(updated, imageGenerationDedupePatchMarker))
		if !strings.Contains(updated, imageGenerationUIPatchMarker) {
			t.Fatalf("official IDE renderer is missing %s after patch: %s", imageGenerationUIPatchMarker, path)
		}
		dedupeSeen = dedupeSeen || strings.Contains(updated, imageGenerationDedupePatchMarker)
		if !strings.Contains(updated, imagePreviewPatchMarker) && !strings.Contains(updated, imagePreviewNativeCompatibleMarker) {
			t.Fatalf("official IDE renderer has neither a fallback nor a validated native preview after patch: %s", path)
		}
		if !bytes.Equal(original, mustReadOfficialImageRendererFixture(t, path)) {
			t.Fatalf("read-only renderer validation modified the fixture: %s", path)
		}
		if node, err := exec.LookPath("node"); err == nil {
			candidate := filepath.Join(t.TempDir(), filepath.Base(path))
			if err := os.WriteFile(candidate, []byte(updated), 0o600); err != nil {
				t.Fatal(err)
			}
			if output, err := exec.Command(node, "--check", candidate).CombinedOutput(); err != nil {
				t.Fatalf("patched official renderer failed node --check: %s: %v", output, err)
			}
		}
	}
	if !dedupeSeen {
		t.Fatal("official IDE renderer set did not expose a safe duplicate-image suppression component")
	}
}

func mustReadOfficialImageRendererFixture(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
