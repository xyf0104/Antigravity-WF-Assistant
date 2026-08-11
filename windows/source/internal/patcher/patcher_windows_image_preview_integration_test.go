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

func TestWindowsLegacyRendererWithoutOriginalBackupUpgradesCurrentBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resources", "app", "out", "jetskiAgent", "main.js")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	current := []byte("/* third-party-prefix */\n" + imagePreviewV3RendererFixture() + "\n/* third-party-suffix */")
	if err := os.WriteFile(path, current, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTIGRAVITY_BYOK_BACKUP_DIR", t.TempDir())
	if windowsExistingFile(windowsBackupPath(path)) != "" {
		t.Fatal("fixture unexpectedly has an original backup")
	}
	source, err := windowsPatchSource(path)
	if err != nil || source != path {
		t.Fatalf("legacy active renderer was not selected directly: source=%q err=%v", source, err)
	}
	plan, err := prepareWindowsImagePreviewPatch(source)
	if err != nil {
		t.Fatal(err)
	}
	plan.path = path
	if !plan.changed || !bytes.Contains(plan.updated, []byte(imagePreviewPatchMarker)) ||
		bytes.Contains(plan.updated, []byte(imagePreviewPatchV3Marker)) {
		t.Fatalf("legacy current renderer was not upgraded: %s", plan.updated)
	}
	for _, preserved := range [][]byte{[]byte("third-party-prefix"), []byte("third-party-suffix")} {
		if !bytes.Contains(plan.updated, preserved) {
			t.Fatalf("forced upgrade removed third-party bytes %q", preserved)
		}
	}
	if err := saveWindowsPlanBackups([]*windowsPatchPlan{plan}); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(windowsBackupPath(path))
	if err != nil || !bytes.Equal(backup, current) {
		t.Fatalf("active legacy/third-party renderer was not backed up exactly: %v", err)
	}
	if err := writeWindowsPlans([]*windowsPatchPlan{plan}); err != nil {
		t.Fatal(err)
	}
	restorePlan, ok, err := prepareWindowsRestorePlan(path)
	if err != nil || !ok || restorePlan == nil {
		t.Fatalf("current-state renderer backup is not restorable: plan=%#v ok=%t err=%v", restorePlan, ok, err)
	}
	if err := writeWindowsPlans([]*windowsPatchPlan{restorePlan}); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(restored, current) {
		t.Fatalf("restore did not reproduce the exact pre-upgrade renderer: %v", err)
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

// TestWindowsASARUnpackedV3MigrationBacksUpCurrentStateAndRestore exercises
// the release-upgrade path that affected packaged Antigravity builds: an older
// WF patch S1 is active without requiring a clean S0 source, then the current
// patch is applied directly and Restore returns both files to exact S1 bytes.
//
// An ASAR node marked unpacked must remain unpacked. Putting its replacement
// in archive.write's replacement map would silently pack it back into the
// archive, so this test checks both the manifest and the external file before
// proving that the public restore flow returns *both* files to exact S1 bytes.
func TestWindowsASARUnpackedV3MigrationBacksUpCurrentStateAndRestore(t *testing.T) {
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

	// The active installation is always the forward-upgrade source, regardless
	// of an old WF marker or an existing backup from another helper version.
	if source, err := windowsPatchSource(asarPath); err != nil || source != asarPath {
		t.Fatalf("ASAR migration source = %q, %v; want active file %q", source, err, asarPath)
	}
	if source, err := windowsPatchSource(unpackedPath); err != nil || source != unpackedPath {
		t.Fatalf("unpacked renderer migration source = %q, %v; want active file %q", source, err, unpackedPath)
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

	// Persistent backups are the exact S1 state present immediately before this
	// upgrade, even when an older clean backup already exists.
	for path, want := range map[string][]byte{asarPath: legacyASAR, unpackedPath: legacyRenderer} {
		backup, err := os.ReadFile(windowsBackupPath(path))
		if err != nil {
			t.Fatalf("read pre-upgrade backup for %s: %v", path, err)
		}
		if !bytes.Equal(backup, want) {
			t.Fatalf("pre-upgrade backup for %s does not match active S1", path)
		}
	}

	// Force the same failure point that can occur when a post-replace target
	// validation observes an unexpected renderer structure. The transactional
	// rollback must return to S1 (the state at the start of *this* apply), not
	// to an unrelated clean S0 or partially written current state.
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
	for path, want := range map[string][]byte{asarPath: legacyASAR, unpackedPath: legacyRenderer} {
		backup, err := os.ReadFile(windowsBackupPath(path))
		if err != nil || !bytes.Equal(backup, want) {
			t.Fatalf("failed migration changed pre-upgrade S1 backup for %s: %v", path, err)
		}
	}

	if _, err := restoreWindowsLegacyTargets([]windowsTarget{target}); err != nil {
		t.Fatalf("restore migrated target: %v", err)
	}
	if restoredASAR, err := os.ReadFile(asarPath); err != nil || !bytes.Equal(restoredASAR, legacyASAR) {
		t.Fatalf("restore did not reproduce pre-upgrade ASAR S1 byte-for-byte: %v", err)
	}
	if restoredRenderer, err := os.ReadFile(unpackedPath); err != nil || !bytes.Equal(restoredRenderer, legacyRenderer) {
		t.Fatalf("restore did not reproduce pre-upgrade unpacked renderer S1 byte-for-byte: %v", err)
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
