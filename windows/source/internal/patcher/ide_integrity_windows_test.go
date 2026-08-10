//go:build windows

package patcher

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIDEChecksumPatchMigratesAlreadyPatchedRendererWithOfficialChecksum(t *testing.T) {
	root := t.TempDir()
	renderer := filepath.Join(root, "resources", "app", "out", "jetskiAgent", "main.js")
	product := filepath.Join(root, "resources", "app", "product.json")
	official := []byte("official renderer")
	patched := []byte("patched renderer")
	writeIDEIntegrityFixture(t, renderer, patched)
	productBefore := []byte("{\n  \"nameShort\": \"Antigravity\",\n  \"checksums\": {\n    \"jetskiAgent/main.js\": \"" + windowsIDEChecksum(official) + "\"\n  },\n  \"preserved\": true\n}\n")
	writeIDEIntegrityFixture(t, product, productBefore)

	target := windowsTarget{root: root, kind: "ide"}
	rendererPlan := &windowsPatchPlan{path: renderer, original: official, updated: patched, mode: 0o644, changed: true}
	plan, err := prepareWindowsIDEProductChecksumPatch(target, []*windowsPatchPlan{rendererPlan})
	if err != nil {
		t.Fatal(err)
	}
	if plan == nil || !bytes.Contains(plan.updated, []byte(windowsIDEChecksum(patched))) {
		t.Fatalf("official checksum was not migrated to the active patched renderer: %#v", plan)
	}
	if !bytes.Contains(plan.updated, []byte(`"preserved": true`)) {
		t.Fatal("unrelated product.json content was not preserved")
	}
	if err := windowsWriteFileAtomic(plan.path, plan.updated, plan.mode); err != nil {
		t.Fatal(err)
	}
	if err := verifyWindowsIDEProductChecksums(target, []string{renderer}); err != nil {
		t.Fatal(err)
	}
}

func TestIDEChecksumPatchRejectsUnknownChecksumWithoutWriting(t *testing.T) {
	root := t.TempDir()
	renderer := filepath.Join(root, "resources", "app", "out", "jetskiAgent", "main.js")
	product := filepath.Join(root, "resources", "app", "product.json")
	official := []byte("official renderer")
	patched := []byte("patched renderer")
	writeIDEIntegrityFixture(t, renderer, official)
	productBefore := []byte(`{"checksums":{"jetskiAgent/main.js":"unknown-checksum"}}`)
	writeIDEIntegrityFixture(t, product, productBefore)

	target := windowsTarget{root: root, kind: "ide"}
	rendererPlan := &windowsPatchPlan{path: renderer, original: official, updated: patched, mode: 0o644, changed: true}
	if plan, err := prepareWindowsIDEProductChecksumPatch(target, []*windowsPatchPlan{rendererPlan}); err == nil || plan != nil {
		t.Fatalf("unknown product checksum must be rejected: plan=%#v err=%v", plan, err)
	}
	productAfter, err := os.ReadFile(product)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(productAfter, productBefore) {
		t.Fatal("checksum validation failure changed product.json")
	}
	rendererAfter, err := os.ReadFile(renderer)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rendererAfter, official) {
		t.Fatal("checksum validation failure changed the renderer")
	}
}

func TestIDEChecksumPatchRestoresOfficialChecksumWithRenderer(t *testing.T) {
	root := t.TempDir()
	renderer := filepath.Join(root, "resources", "app", "out", "vs", "workbench", "workbench.desktop.main.js")
	product := filepath.Join(root, "resources", "app", "product.json")
	official := []byte("official renderer")
	patched := []byte("patched renderer")
	writeIDEIntegrityFixture(t, renderer, patched)
	writeIDEIntegrityFixture(t, product, []byte(`{"checksums":{"vs/workbench/workbench.desktop.main.js":"`+windowsIDEChecksum(patched)+`"}}`))

	target := windowsTarget{root: root, kind: "ide"}
	restorePlan := &windowsPatchPlan{path: renderer, original: patched, updated: official, mode: 0o644, changed: true}
	productPlan, err := prepareWindowsIDEProductChecksumPatch(target, []*windowsPatchPlan{restorePlan})
	if err != nil {
		t.Fatal(err)
	}
	if productPlan == nil || !bytes.Contains(productPlan.updated, []byte(windowsIDEChecksum(official))) {
		t.Fatalf("restore did not prepare the official renderer checksum: %#v", productPlan)
	}
	if err := writeWindowsPlans([]*windowsPatchPlan{restorePlan, productPlan}); err != nil {
		t.Fatal(err)
	}
	if err := verifyWindowsIDEProductChecksums(target, []string{renderer}); err != nil {
		t.Fatal(err)
	}
}

func TestIDE1ProductWithoutChecksumsNeedsNoIntegrityRewrite(t *testing.T) {
	root := t.TempDir()
	renderer := filepath.Join(root, "resources", "app", "out", "jetskiAgent", "main.js")
	product := filepath.Join(root, "resources", "app", "product.json")
	official := []byte("official renderer")
	patched := []byte("patched renderer")
	writeIDEIntegrityFixture(t, renderer, official)
	productBefore := []byte("{\n  \"nameShort\": \"Antigravity 1.x\"\n}\n")
	writeIDEIntegrityFixture(t, product, productBefore)

	target := windowsTarget{root: root, kind: "ide"}
	rendererPlan := &windowsPatchPlan{path: renderer, original: official, updated: patched, mode: 0o644, changed: true}
	plan, err := prepareWindowsIDEProductChecksumPatch(target, []*windowsPatchPlan{rendererPlan})
	if err != nil || plan != nil {
		t.Fatalf("IDE 1.x without a checksum table should keep the legacy flow: plan=%#v err=%v", plan, err)
	}
	productAfter, err := os.ReadFile(product)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(productAfter, productBefore) {
		t.Fatal("IDE 1.x product.json was unexpectedly rewritten")
	}
}

func TestIDEChecksumPatchTouchesOnlyTrackedRendererKeys(t *testing.T) {
	root := t.TempDir()
	renderer := filepath.Join(root, "resources", "app", "out", "jetskiAgent", "main.js")
	outside := filepath.Join(root, "outside.js")
	product := filepath.Join(root, "resources", "app", "product.json")
	official := []byte("official renderer")
	patched := []byte("patched renderer")
	writeIDEIntegrityFixture(t, renderer, official)
	writeIDEIntegrityFixture(t, outside, official)
	otherChecksum := strings.Repeat("A", 43)
	writeIDEIntegrityFixture(t, product, []byte(`{"checksums":{"jetskiAgent/main.js":"`+windowsIDEChecksum(official)+`","unrelated.js":"`+otherChecksum+`"}}`))

	target := windowsTarget{root: root, kind: "ide"}
	plans := []*windowsPatchPlan{
		{path: renderer, original: official, updated: patched, mode: 0o644, changed: true},
		{path: outside, original: official, updated: patched, mode: 0o644, changed: true},
	}
	plan, err := prepareWindowsIDEProductChecksumPatch(target, plans)
	if err != nil {
		t.Fatal(err)
	}
	if plan == nil || !bytes.Contains(plan.updated, []byte(`"unrelated.js":"`+otherChecksum+`"`)) {
		t.Fatal("unrelated or out-of-scope checksum was changed")
	}
}

func writeIDEIntegrityFixture(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
