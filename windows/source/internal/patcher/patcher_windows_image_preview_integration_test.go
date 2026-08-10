//go:build windows

package patcher

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsUnpackedImagePreviewPatchApplyAndRestore(t *testing.T) {
	root := t.TempDir()
	appRoot := filepath.Join(root, "resources", "app")
	mainPath := filepath.Join(appRoot, "out", "main.js")
	extensionPath := filepath.Join(appRoot, "extensions", "antigravity", "dist", "extension.js")
	languagePath := filepath.Join(appRoot, "extensions", "antigravity", "bin", "language_server_windows_x64.exe")
	executable := filepath.Join(root, "Antigravity.exe")
	originals := map[string][]byte{
		executable:    []byte("MZ"),
		mainPath:      []byte(`"use strict";const endpoint=` + windowsIDECloudCodeSetting + `;` + authEligibilityOriginal + imagePreviewOriginalRendererFixture()),
		extensionPath: []byte("/*! For license information please see extension.js.LICENSE.txt */\nconst endpoint=await service.getCloudCodeUrl();args.push(\"--cloud_code_endpoint\",endpoint);"),
		languagePath:  []byte("MZ\x00" + windowsProductionEndpoint + "\x00tail"),
	}
	for index, relative := range imagePreviewRendererRelativePaths[1:] {
		path := filepath.Join(appRoot, filepath.FromSlash(relative))
		if index == 0 {
			originals[path] = []byte(imagePreviewOriginalRendererFixture())
		} else {
			originals[path] = []byte(`const futureRenderer={generatedMedia:"different-shape"};`)
		}
	}
	for path, data := range originals {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0o644)
		if path == executable || path == languagePath {
			mode = 0o755
		}
		if err := os.WriteFile(path, data, mode); err != nil {
			t.Fatal(err)
		}
	}

	t.Setenv("ANTIGRAVITY_BYOK_BACKUP_DIR", t.TempDir())
	target := windowsTarget{
		root: root, name: "Antigravity", kind: "ide", executable: executable,
		main: mainPath, extensionEntry: extensionPath, language: languagePath,
	}
	if _, err := applyWindowsLegacyTarget(target); err != nil {
		t.Fatal(err)
	}
	for _, rendererPath := range windowsImagePreviewRendererPaths(target) {
		data, err := os.ReadFile(rendererPath)
		if err != nil {
			t.Fatal(err)
		}
		knownRenderer := rendererPath == mainPath || strings.Contains(filepath.ToSlash(rendererPath), "/jetskiAgent/main.js")
		if knownRenderer && !bytes.Contains(data, []byte(imagePreviewPatchMarker)) {
			t.Fatalf("renderer was not patched: %s", rendererPath)
		}
		if !knownRenderer && bytes.Contains(data, []byte(imagePreviewPatchMarker)) {
			t.Fatalf("unknown renderer was unexpectedly rewritten: %s", rendererPath)
		}
		if knownRenderer {
			if _, err := os.Stat(windowsBackupPath(rendererPath)); err != nil {
				t.Fatalf("renderer backup is missing for %s: %v", rendererPath, err)
			}
		}
	}
	if _, err := restoreWindowsLegacyTargets([]windowsTarget{target}); err != nil {
		t.Fatal(err)
	}
	for path, original := range originals {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, original) {
			t.Fatalf("restore did not reproduce %s byte-for-byte", path)
		}
	}
}

func TestWindowsStatusReflectsPendingImagePreviewFallback(t *testing.T) {
	root := t.TempDir()
	main := filepath.Join(root, "resources", "app", "out", "main.js")
	if err := os.MkdirAll(filepath.Dir(main), 0o755); err != nil {
		t.Fatal(err)
	}
	endpointPatched := "// " + windowsMainMarker + "\n" + windowsBaseProxyEndpoint + "\n"
	if err := os.WriteFile(main, []byte(endpointPatched+imagePreviewOriginalRendererFixture()), 0o644); err != nil {
		t.Fatal(err)
	}
	target := windowsTarget{root: root, kind: "ide", main: main}
	status := buildWindowsStatus([]windowsTarget{target})
	if status.IDEPatched == nil || *status.IDEPatched || len(status.Targets) != 1 || status.Targets[0].Patched {
		t.Fatalf("pending preview fallback was not reflected in Windows status: %+v", status)
	}

	updated, result := patchImagePreviewRenderer(endpointPatched + imagePreviewOriginalRendererFixture())
	if !result.Changed {
		t.Fatal("fixture should produce the current v4 renderer")
	}
	if err := os.WriteFile(main, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	status = buildWindowsStatus([]windowsTarget{target})
	if status.IDEPatched == nil || !*status.IDEPatched || !status.Targets[0].Patched {
		t.Fatalf("v4 renderer did not restore Windows status: %+v", status)
	}
}

// TestWindowsASARUnpackedV3MigrationPreservesCanonicalBackupsAndRestore
// exercises the release-upgrade path that affected packaged Antigravity
// builds: a clean app.asar S0 and its external unpacked renderer are backed
// up, an older WF patch S1 leaves the renderer at v3, and v4 is then applied.
//
// An ASAR node marked unpacked must remain unpacked. Putting its replacement
// in archive.write's replacement map would silently pack it back into the
// archive, so this test checks both the manifest and the external file before
// proving that the public restore flow returns *both* files to exact S0 bytes.
func TestWindowsASARUnpackedV3MigrationPreservesCanonicalBackupsAndRestore(t *testing.T) {
	root := t.TempDir()
	asarPath := filepath.Join(root, "resources", "app.asar")
	unpackedRelative := "out/jetskiAgent/main.js"
	unpackedPath := filepath.Join(asarPath+".unpacked", filepath.FromSlash(unpackedRelative))

	cleanMain := []byte(`"use strict";const endpoint="` + windowsProductionEndpoint + `";` + authEligibilityOriginal)
	cleanLauncher := []byte(`const args=["--cloud_code_endpoint","` + windowsProductionEndpoint + `"];`)
	cleanRenderer := []byte(imagePreviewOriginalRendererFixture())
	fixture := &asarArchive{root: &asarNode{Files: map[string]*asarNode{}}}
	if err := fixture.putFile("package.json", []byte(`{"version":"2.0.4"}`)); err != nil {
		t.Fatal(err)
	}
	if err := fixture.putFile("dist/main.js", cleanMain); err != nil {
		t.Fatal(err)
	}
	if err := fixture.putFile("dist/languageServer.js", cleanLauncher); err != nil {
		t.Fatal(err)
	}
	if err := fixture.putFile(unpackedRelative, nil); err != nil {
		t.Fatal(err)
	}
	unpackedNode, err := fixture.node(unpackedRelative)
	if err != nil {
		t.Fatal(err)
	}
	// A real unpacked entry has no bytes in app.asar. Keep the fixture's
	// manifest equally strict so any accidental repacking is observable.
	unpackedNode.Unpacked = true
	unpackedNode.Size = nil
	unpackedNode.Offset = ""
	unpackedNode.Integrity = nil
	if err := os.MkdirAll(filepath.Dir(asarPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := fixture.write(asarPath, map[string][]byte{
		"package.json":           []byte(`{"version":"2.0.4"}`),
		"dist/main.js":           cleanMain,
		"dist/languageServer.js": cleanLauncher,
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(unpackedPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unpackedPath, cleanRenderer, 0o644); err != nil {
		t.Fatal(err)
	}

	cleanASAR, err := os.ReadFile(asarPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTIGRAVITY_BYOK_BACKUP_DIR", t.TempDir())
	if err := saveWindowsBackup(asarPath, cleanASAR); err != nil {
		t.Fatalf("save clean ASAR backup: %v", err)
	}
	if err := saveWindowsBackup(unpackedPath, cleanRenderer); err != nil {
		t.Fatalf("save clean unpacked renderer backup: %v", err)
	}

	// Construct S1 with the old endpoint patch in app.asar and the actual v3
	// preview fallback in app.asar.unpacked. This is the important Windows
	// migration case: the external v3 renderer receives an encoded C:/ path.
	cleanArchive, err := readASAR(asarPath)
	if err != nil {
		t.Fatal(err)
	}
	legacyMain := addWindowsSourceMarker(
		strings.ReplaceAll(patchWindowsCloudCodeSource(string(cleanMain)), authEligibilityOriginal, authEligibilityPatched),
		windowsASARMarker,
	)
	legacyCandidate := filepath.Join(root, "legacy-s1.app.asar")
	if err := cleanArchive.write(legacyCandidate, map[string][]byte{
		"dist/main.js":           []byte(legacyMain),
		"dist/languageServer.js": []byte(patchWindowsCloudCodeSource(string(cleanLauncher))),
	}); err != nil {
		t.Fatal(err)
	}
	if !windowsASARPatched(legacyCandidate) {
		t.Fatal("legacy S1 ASAR fixture did not satisfy the existing endpoint patch state")
	}
	if err := windowsReplaceFile(legacyCandidate, asarPath); err != nil {
		t.Fatalf("activate legacy S1 ASAR: %v", err)
	}
	legacyRenderer := []byte(imagePreviewV3RendererFixture())
	if err := windowsWriteFileAtomic(unpackedPath, legacyRenderer, 0o644); err != nil {
		t.Fatalf("activate legacy S1 unpacked renderer: %v", err)
	}
	legacyASAR, err := os.ReadFile(asarPath)
	if err != nil {
		t.Fatalf("read active legacy S1 ASAR: %v", err)
	}

	// Both patch sources must resolve to their canonical clean backups. This
	// is the regression guard for an upgrade that accidentally turns S1 into
	// the new "original" and makes Restore reinstall the old helper patch.
	if source, err := windowsPatchSource(asarPath); err != nil || source != windowsBackupPath(asarPath) {
		t.Fatalf("ASAR migration source = %q, %v; want canonical backup %q", source, err, windowsBackupPath(asarPath))
	}
	if source, err := windowsPatchSource(unpackedPath); err != nil || source != windowsBackupPath(unpackedPath) {
		t.Fatalf("unpacked renderer migration source = %q, %v; want canonical backup %q", source, err, windowsBackupPath(unpackedPath))
	}

	target := windowsTarget{root: root, name: "Antigravity Agent", kind: "agent", asar: asarPath}
	if _, _, _, fullyPatched := windowsTargetPatchState(target); fullyPatched {
		t.Fatal("v3 unpacked renderer must keep the legacy target pending until v4 migration")
	}
	status := buildWindowsStatus([]windowsTarget{target})
	if status.AgentPatched == nil || *status.AgentPatched || len(status.Targets) != 1 || status.Targets[0].Patched {
		t.Fatalf("v3 unpacked renderer was not reflected as pending in Windows status: %+v", status)
	}

	if _, err := applyWindowsASARTarget(target); err != nil {
		t.Fatalf("apply v4 migration: %v", err)
	}
	archive, err := readASAR(asarPath)
	if err != nil {
		t.Fatal(err)
	}
	updatedNode, err := archive.node(unpackedRelative)
	if err != nil {
		t.Fatal(err)
	}
	if !updatedNode.Unpacked {
		t.Fatal("v4 migration repacked an ASAR node that must remain unpacked")
	}
	updatedRenderer, err := archive.readFile(unpackedRelative)
	if err != nil {
		t.Fatalf("read v4 external renderer through ASAR: %v", err)
	}
	if !bytes.Contains(updatedRenderer, []byte(imagePreviewPatchMarker)) || bytes.Contains(updatedRenderer, []byte(imagePreviewPatchV3Marker)) {
		t.Fatalf("external renderer was not upgraded to v4: %s", updatedRenderer)
	}
	if data, err := os.ReadFile(unpackedPath); err != nil || !bytes.Equal(data, updatedRenderer) {
		t.Fatalf("unpacked renderer is not the external v4 file: %q, %v", data, err)
	}
	if _, _, _, fullyPatched := windowsTargetPatchState(target); !fullyPatched {
		t.Fatal("v4 ASAR plus unpacked renderer should satisfy complete Windows target status")
	}
	status = buildWindowsStatus([]windowsTarget{target})
	if status.AgentPatched == nil || !*status.AgentPatched || !status.Targets[0].Patched {
		t.Fatalf("v4 migration did not report a patched Windows target: %+v", status)
	}

	// Canonical .bak files remain S0 after the upgrade; neither one may be
	// replaced with S1 while new candidates are generated.
	for path, want := range map[string][]byte{asarPath: cleanASAR, unpackedPath: cleanRenderer} {
		backup, err := os.ReadFile(windowsBackupPath(path))
		if err != nil {
			t.Fatalf("read canonical backup for %s: %v", path, err)
		}
		if !bytes.Equal(backup, want) {
			t.Fatalf("canonical backup for %s was overwritten during v3 -> v4 migration", path)
		}
	}

	// Force the same failure point that can occur when a post-replace target
	// validation observes an unexpected renderer structure. The transactional
	// rollback must return to S1 (the state at the start of *this* apply), not
	// to canonical S0. The latter remains reserved for the explicit Restore
	// command below.
	if err := windowsWriteFileAtomic(asarPath, legacyASAR, 0o644); err != nil {
		t.Fatalf("restore legacy S1 ASAR for failure rollback test: %v", err)
	}
	if err := windowsWriteFileAtomic(unpackedPath, legacyRenderer, 0o644); err != nil {
		t.Fatalf("restore legacy S1 renderer for failure rollback test: %v", err)
	}
	previousHook := windowsASARPostReplaceHook
	t.Cleanup(func() { windowsASARPostReplaceHook = previousHook })
	hookCalled := false
	var hookErr error
	windowsASARPostReplaceHook = func() {
		hookCalled = true
		hookErr = windowsWriteFileAtomic(unpackedPath, []byte(imagePreviewOriginalRendererFixture()), 0o644)
	}
	if _, err := applyWindowsASARTarget(target); err == nil {
		t.Fatal("forced post-replace validation failure unexpectedly succeeded")
	}
	if !hookCalled || hookErr != nil {
		t.Fatalf("post-replace failure hook did not run cleanly: called=%t err=%v", hookCalled, hookErr)
	}
	if activeASAR, err := os.ReadFile(asarPath); err != nil || !bytes.Equal(activeASAR, legacyASAR) {
		t.Fatalf("failed v3 -> v4 upgrade did not roll app.asar back to active S1: %v", err)
	}
	if activeRenderer, err := os.ReadFile(unpackedPath); err != nil || !bytes.Equal(activeRenderer, legacyRenderer) {
		t.Fatalf("failed v3 -> v4 upgrade did not roll unpacked renderer back to active S1: %v", err)
	}
	if _, _, _, fullyPatched := windowsTargetPatchState(target); fullyPatched {
		t.Fatal("rollback should restore the original pending v3 target state, not a partial v4 state")
	}
	for path, want := range map[string][]byte{asarPath: cleanASAR, unpackedPath: cleanRenderer} {
		backup, err := os.ReadFile(windowsBackupPath(path))
		if err != nil || !bytes.Equal(backup, want) {
			t.Fatalf("failed migration changed canonical S0 backup for %s: %v", path, err)
		}
	}

	if _, err := restoreWindowsLegacyTargets([]windowsTarget{target}); err != nil {
		t.Fatalf("restore migrated target: %v", err)
	}
	if restoredASAR, err := os.ReadFile(asarPath); err != nil || !bytes.Equal(restoredASAR, cleanASAR) {
		t.Fatalf("restore did not reproduce clean ASAR S0 byte-for-byte: %v", err)
	}
	if restoredRenderer, err := os.ReadFile(unpackedPath); err != nil || !bytes.Equal(restoredRenderer, cleanRenderer) {
		t.Fatalf("restore did not reproduce clean unpacked renderer S0 byte-for-byte: %v", err)
	}
	restoredArchive, err := readASAR(asarPath)
	if err != nil {
		t.Fatal(err)
	}
	restoredNode, err := restoredArchive.node(unpackedRelative)
	if err != nil || !restoredNode.Unpacked {
		t.Fatalf("restore did not preserve the original unpacked manifest node: %#v, %v", restoredNode, err)
	}
}
