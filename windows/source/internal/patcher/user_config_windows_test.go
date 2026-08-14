//go:build windows

package patcher

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsApplyUsesOfficialSettingsAndKeepsIDEChecksumsValid(t *testing.T) {
	root := t.TempDir()
	appRoot := filepath.Join(root, "resources", "app")
	mainPath := filepath.Join(appRoot, "out", "main.js")
	jetskiRenderer := filepath.Join(appRoot, "out", "jetskiAgent", "main.js")
	workbenchRenderer := filepath.Join(appRoot, "out", "vs", "workbench", "workbench.desktop.main.js")
	extensionPath := filepath.Join(appRoot, "extensions", "antigravity", "dist", "extension.js")
	productPath := filepath.Join(appRoot, "product.json")
	// Keep the fixture process name distinct from a user's running IDE so this
	// transaction test never depends on desktop application state.
	executable := filepath.Join(root, "wf-fixture-ide.exe")
	rendererOriginal := []byte(imagePreviewOriginalRendererFixture() + imageGenerationUIRendererFixture() + imageArtifactMarkdownRendererFixture())
	productOriginal := []byte(`{"nameShort":"Antigravity Test","dataFolderName":".antigravity-test","checksums":{"jetskiAgent/main.js":"` + windowsIDEChecksum(rendererOriginal) + `","vs/workbench/workbench.desktop.main.js":"` + windowsIDEChecksum(rendererOriginal) + `"}}`)
	files := map[string][]byte{
		executable:        []byte("MZ"),
		productPath:       productOriginal,
		mainPath:          []byte(`const base=configuration.getValue("jetski.cloudCodeUrl");`),
		extensionPath:     []byte(`const key="jetski.cloudCodeUrl";args.push("--cloud_code_endpoint",await service.getCloudCodeUrl());`),
		jetskiRenderer:    rendererOriginal,
		workbenchRenderer: rendererOriginal,
	}
	for path, data := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	appData := t.TempDir()
	settingsPath := filepath.Join(appData, "Antigravity Test", "User", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	settingsBefore := []byte("{\n  // preserved comment\n  \"editor.fontSize\": 14\n}\n")
	if err := os.WriteFile(settingsPath, settingsBefore, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APPDATA", appData)
	target := windowsTarget{
		root: root, name: "Antigravity IDE", kind: "ide", version: "1.107.0",
		executable: executable, main: mainPath, extensionEntry: extensionPath,
	}
	if _, err := applyWindowsTarget(target); err != nil {
		t.Fatal(err)
	}
	settingsAfter, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"preserved comment", `"editor.fontSize": 14`, `"jetski.cloudCodeUrl": "http://127.0.0.1:50999"`} {
		if !strings.Contains(string(settingsAfter), expected) {
			t.Fatalf("settings lost expected content %q: %s", expected, settingsAfter)
		}
	}
	for path, original := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		isRenderer := path == jetskiRenderer || path == workbenchRenderer
		if isRenderer {
			if bytes.Equal(data, original) || !bytes.Contains(data, []byte(imageGenerationUIPatchMarker)) {
				t.Fatalf("verified image renderer was not patched: %s", path)
			}
		} else if path == productPath {
			if bytes.Equal(data, original) {
				t.Fatal("IDE product checksums were not updated")
			}
		} else if !bytes.Equal(data, original) {
			t.Fatalf("non-renderer official bundle file changed: %s", path)
		}
	}
	if err := verifyWindowsIDEProductChecksums(target, []string{jetskiRenderer, workbenchRenderer}); err != nil {
		t.Fatal(err)
	}
	status := buildWindowsStatus([]windowsTarget{target})
	if len(status.Targets) != 1 || !status.Targets[0].Supported || !status.Targets[0].Patched || status.Targets[0].ConnectionMode != "user-settings" {
		t.Fatalf("unexpected safe configuration status: %+v", status)
	}
}

func TestWindowsV5RendererIsNotCachedAsConnected(t *testing.T) {
	invalidateWindowsStatusCache()
	t.Cleanup(invalidateWindowsStatusCache)
	root := t.TempDir()
	appRoot := filepath.Join(root, "resources", "app")
	mainPath := filepath.Join(appRoot, "out", "main.js")
	extensionPath := filepath.Join(appRoot, "extensions", "antigravity", "dist", "extension.js")
	rendererPath := filepath.Join(appRoot, "out", "jetskiAgent", "main.js")
	productPath := filepath.Join(appRoot, "product.json")
	executable := filepath.Join(root, "wf-v4-migration-fixture.exe")
	legacyRenderer := []byte(imageGenerationDedupeV5RendererFixture(t))
	files := map[string][]byte{
		executable:    []byte("MZ"),
		mainPath:      []byte(`const base=configuration.getValue("jetski.cloudCodeUrl");`),
		extensionPath: []byte(`const key="jetski.cloudCodeUrl";args.push("--cloud_code_endpoint",await service.getCloudCodeUrl());`),
		rendererPath:  legacyRenderer,
		productPath:   []byte(`{"nameShort":"Antigravity Test","checksums":{"jetskiAgent/main.js":"` + windowsIDEChecksum(legacyRenderer) + `"}}`),
	}
	for path, data := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	appData := t.TempDir()
	settingsPath := filepath.Join(appData, "Antigravity Test", "User", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte("{\n  \"jetski.cloudCodeUrl\": \"http://127.0.0.1:50999\"\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APPDATA", appData)
	t.Setenv("ANTIGRAVITY_BYOK_BACKUP_DIR", filepath.Join(t.TempDir(), "backups"))
	target := windowsTarget{
		root: root, name: "Antigravity IDE", kind: "ide", version: "2.5.5",
		executable: executable, main: mainPath, extensionEntry: extensionPath,
	}

	status := cacheWindowsStatus(buildWindowsStatus([]windowsTarget{target}))
	if len(status.Targets) != 1 || !status.Targets[0].Supported || status.Targets[0].Patched {
		t.Fatalf("configured endpoint with legacy renderer must remain pending: %+v", status)
	}
	if windowsCachedTargetConnected(target) {
		t.Fatal("legacy renderer was incorrectly accepted by the connected-state cache")
	}

	message, err := applyWindowsTargetsForKind([]windowsTarget{target}, "ide")
	if err != nil {
		t.Fatalf("migrate legacy renderer: %v", err)
	}
	if strings.Contains(message, "文件未变化") {
		t.Fatalf("first apply incorrectly skipped the v3 to v4 migration: %s", message)
	}
	active, err := os.ReadFile(rendererPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(active, []byte(imageGenerationDedupePatchMarker)) || bytes.Contains(active, []byte(imageGenerationDedupePatchV5Marker)) || !bytes.Contains(active, []byte(imageArtifactThumbnailStyle)) {
		t.Fatalf("legacy renderer was not migrated to the current marker: %s", active)
	}

	status = cacheWindowsStatus(buildWindowsStatus([]windowsTarget{target}))
	if !status.Targets[0].Patched || !windowsCachedTargetConnected(target) {
		t.Fatalf("migrated renderer was not cached as connected: %+v", status)
	}
	message, err = applyWindowsTargetsForKind([]windowsTarget{target}, "ide")
	if err != nil {
		t.Fatalf("repeat apply: %v", err)
	}
	if !strings.Contains(message, "文件未变化，无需重复扫描") {
		t.Fatalf("second apply did not use the verified no-op cache: %s", message)
	}
}

func TestWindowsUnknownOptionalImageRendererDoesNotBlockIDEConnection(t *testing.T) {
	root := t.TempDir()
	appRoot := filepath.Join(root, "resources", "app")
	mainPath := filepath.Join(appRoot, "out", "main.js")
	extensionPath := filepath.Join(appRoot, "extensions", "antigravity", "dist", "extension.js")
	rendererPath := filepath.Join(appRoot, "out", "jetskiAgent", "main.js")
	productPath := filepath.Join(appRoot, "product.json")
	unknownRenderer := []byte(`const futureImageRenderer={generatedMedia:"unknown-layout"};`)
	product := []byte(`{"nameShort":"Antigravity Test","dataFolderName":".antigravity-test","checksums":{"jetskiAgent/main.js":"` + windowsIDEChecksum(unknownRenderer) + `"}}`)
	for path, data := range map[string][]byte{
		mainPath:      []byte(`const base=configuration.getValue("jetski.cloudCodeUrl");`),
		extensionPath: []byte(`const key="jetski.cloudCodeUrl";args.push("--cloud_code_endpoint",await service.getCloudCodeUrl());`),
		rendererPath:  unknownRenderer,
		productPath:   product,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	appData := t.TempDir()
	t.Setenv("APPDATA", appData)
	t.Setenv("ANTIGRAVITY_WF_BACKUP_DIR", t.TempDir())
	target := windowsTarget{root: root, name: "Antigravity IDE", kind: "ide", main: mainPath, extensionEntry: extensionPath}
	message, err := applyWindowsTarget(target)
	if err != nil {
		t.Fatalf("optional unknown renderer blocked the IDE endpoint: %v", err)
	}
	if !strings.Contains(message, "已安全连接本地代理") || !strings.Contains(message, "已跳过可选界面增强") {
		t.Fatalf("unexpected compatibility message: %s", message)
	}
	settingsPath := filepath.Join(appData, "Antigravity Test", "User", "settings.json")
	if !windowsCloudCodeSettingIsConfigured(settingsPath, windowsBaseProxyEndpoint) {
		t.Fatal("official endpoint setting was not connected")
	}
	for path, expected := range map[string][]byte{rendererPath: unknownRenderer, productPath: product} {
		actual, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !bytes.Equal(actual, expected) {
			t.Fatalf("optional unknown image file changed: %s", path)
		}
		if windowsExistingFile(windowsBackupPath(path)) != "" {
			t.Fatalf("unchanged optional image file received a misleading backup: %s", path)
		}
	}
}

func TestWindowsRestoreNeverWritesHistoricalBackup(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "resources", "app", "out", "main.js")
	if err := os.MkdirAll(filepath.Dir(mainPath), 0o755); err != nil {
		t.Fatal(err)
	}
	active := []byte("active official bytes")
	if err := os.WriteFile(mainPath, active, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTIGRAVITY_BYOK_BACKUP_DIR", t.TempDir())
	contaminated := []byte("/*" + imagePreviewPatchMarker + "*/ stale helper bytes")
	if err := os.MkdirAll(filepath.Dir(windowsBackupPath(mainPath)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(windowsBackupPath(mainPath), contaminated, 0o600); err != nil {
		t.Fatal(err)
	}
	message, err := restoreWindowsTargets([]windowsTarget{{root: root, name: "Antigravity", kind: "ide", main: mainPath}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(message, "未修改任何用户文件或安装文件") {
		t.Fatalf("unexpected safe restore guidance: %s", message)
	}
	data, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, active) {
		t.Fatal("production restore wrote a historical backup into the active bundle")
	}
}

func TestWindowsRestoreRemovesOnlyHelperCloudCodeSetting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	original := []byte("{\n  // keep this comment\n  \"editor.fontSize\": 14,\n  \"jetski.cloudCodeUrl\": \"http://127.0.0.1:50999\",\n  \"workbench.colorTheme\": \"Dark\"\n}\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	plan, ok, err := prepareWindowsRemoveCloudCodeSetting(path, windowsBaseProxyEndpoint)
	if err != nil || !ok {
		t.Fatalf("prepare remove = ok:%t err:%v", ok, err)
	}
	if strings.Contains(string(plan.updated), windowsCloudCodeSetting) {
		t.Fatal("helper setting was not removed")
	}
	for _, expected := range []string{"keep this comment", `"editor.fontSize": 14`, `"workbench.colorTheme": "Dark"`} {
		if !strings.Contains(string(plan.updated), expected) {
			t.Fatalf("restore removed unrelated content %q: %s", expected, plan.updated)
		}
	}
	if _, err := parseWindowsJSONCObject(string(plan.updated)); err != nil {
		t.Fatalf("restored settings are invalid JSONC: %v\n%s", err, plan.updated)
	}
}

func TestWindowsRestorePreservesUserCloudCodeSetting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	original := []byte("{\n  \"jetski.cloudCodeUrl\": \"https://user.example.invalid\"\n}\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	plan, ok, err := prepareWindowsRemoveCloudCodeSetting(path, windowsBaseProxyEndpoint)
	if err != nil || ok || plan != nil {
		t.Fatalf("user setting must be preserved: plan:%#v ok:%t err:%v", plan, ok, err)
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(current, original) {
		t.Fatal("user setting was changed")
	}
}

func TestWindowsEnsureCloudCodeSettingBacksUpAndRestoresThirdPartyValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	original := []byte("{\n  // third-party setting\n  \"jetski.cloudCodeUrl\": \"https://third-party.example.invalid\",\n  \"editor.fontSize\": 15\n}\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTIGRAVITY_BYOK_BACKUP_DIR", t.TempDir())
	plan, changed, err := prepareWindowsEnsureCloudCodeSetting(path, windowsBaseProxyEndpoint)
	if err != nil || !changed || plan == nil {
		t.Fatalf("third-party setting was not accepted for forced upgrade: plan=%#v changed=%t err=%v", plan, changed, err)
	}
	if !strings.Contains(string(plan.updated), `"jetski.cloudCodeUrl": "`+windowsBaseProxyEndpoint+`"`) ||
		!strings.Contains(string(plan.updated), `"editor.fontSize": 15`) {
		t.Fatalf("forced setting update lost structure: %s", plan.updated)
	}
	if err := saveWindowsPlanBackups([]*windowsPatchPlan{plan}); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(windowsBackupPath(path))
	if err != nil || !bytes.Equal(backup, original) {
		t.Fatalf("current third-party setting was not backed up exactly: %v", err)
	}
	if err := writeWindowsPlans([]*windowsPatchPlan{plan}); err != nil {
		t.Fatal(err)
	}
	restorePlan, ok, err := prepareWindowsRestorePlan(path)
	if err != nil || !ok || restorePlan == nil {
		t.Fatalf("pre-upgrade setting snapshot was not restorable: plan=%#v ok=%t err=%v", restorePlan, ok, err)
	}
	if err := writeWindowsPlans([]*windowsPatchPlan{restorePlan}); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(restored, original) {
		t.Fatalf("restore did not reproduce the third-party setting: %v", err)
	}
}

func TestWindowsApplyRejectsUnknownStructureWithoutWriting(t *testing.T) {
	root := t.TempDir()
	mainPath := filepath.Join(root, "resources", "app", "out", "main.js")
	if err := os.MkdirAll(filepath.Dir(mainPath), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("const unknownRenderer=true;")
	if err := os.WriteFile(mainPath, original, 0o644); err != nil {
		t.Fatal(err)
	}
	target := windowsTarget{root: root, name: "Unknown", kind: "ide", version: "future", main: mainPath}
	if _, err := applyWindowsTargets([]windowsTarget{target}, false); err == nil {
		t.Fatal("unknown structure must not be connected through a guessed path")
	}
	data, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, original) {
		t.Fatal("unknown structure was modified")
	}
}

func TestWindowsApplySkipsUnsupportedTargetWhenIDEIsCompatible(t *testing.T) {
	root := t.TempDir()
	ideRoot := filepath.Join(root, "ide")
	appRoot := filepath.Join(ideRoot, "resources", "app")
	mainPath := filepath.Join(appRoot, "out", "main.js")
	jetskiRenderer := filepath.Join(appRoot, "out", "jetskiAgent", "main.js")
	workbenchRenderer := filepath.Join(appRoot, "out", "vs", "workbench", "workbench.desktop.main.js")
	extensionPath := filepath.Join(appRoot, "extensions", "antigravity", "dist", "extension.js")
	productPath := filepath.Join(appRoot, "product.json")
	// Use a fixture-only image name so a user's running official IDE cannot
	// make this unit test exercise the production process-safety guard.
	ideExecutable := filepath.Join(ideRoot, "fixture-ide.exe")
	rendererOriginal := []byte(imagePreviewOriginalRendererFixture() + imageGenerationUIRendererFixture() + imageArtifactMarkdownRendererFixture())
	files := map[string][]byte{
		ideExecutable:     []byte("MZ"),
		productPath:       []byte(`{"nameShort":"Antigravity Test","checksums":{"jetskiAgent/main.js":"` + windowsIDEChecksum(rendererOriginal) + `","vs/workbench/workbench.desktop.main.js":"` + windowsIDEChecksum(rendererOriginal) + `"}}`),
		mainPath:          []byte(`const base=configuration.getValue("jetski.cloudCodeUrl");`),
		extensionPath:     []byte(`const key="jetski.cloudCodeUrl";args.push("--cloud_code_endpoint",await service.getCloudCodeUrl());`),
		jetskiRenderer:    rendererOriginal,
		workbenchRenderer: rendererOriginal,
	}
	for path, data := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	appData := t.TempDir()
	if err := os.MkdirAll(filepath.Join(appData, "Antigravity Test", "User"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APPDATA", appData)
	agentRoot := filepath.Join(root, "agent")
	agentArchive := filepath.Join(agentRoot, "resources", "app.asar")
	if err := os.MkdirAll(filepath.Dir(agentArchive), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agentArchive, []byte("official agent fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	agent := windowsTarget{
		root: agentRoot, name: "Antigravity 2.0", kind: "agent", version: "2.6.0",
		executable: filepath.Join(agentRoot, "fixture-agent.exe"), asar: agentArchive,
	}
	ide := windowsTarget{
		root: ideRoot, name: "Antigravity IDE", kind: "ide", version: "1.107.0",
		executable: ideExecutable, main: mainPath, extensionEntry: extensionPath,
	}
	message, err := applyWindowsTargets([]windowsTarget{agent, ide}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Antigravity 2.0 未连接", "Antigravity IDE 已安全连接本地代理并启用实际生图模型标题"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("message is missing %q: %s", expected, message)
		}
	}
	if !windowsCloudCodeSettingIsConfigured(filepath.Join(appData, "Antigravity Test", "User", "settings.json"), windowsBaseProxyEndpoint) {
		t.Fatal("compatible IDE was not connected")
	}
	data, err := os.ReadFile(agentArchive)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, []byte("official agent fixture")) {
		t.Fatal("unsupported Agent bundle was modified")
	}
}

func TestWindowsApplyAgentRejectsUnknownStructureWithoutWriting(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "resources", "app.asar")
	if err := os.MkdirAll(filepath.Dir(archive), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("official agent fixture")
	if err := os.WriteFile(archive, original, 0o644); err != nil {
		t.Fatal(err)
	}
	target := windowsTarget{
		root: root, name: "Antigravity 2.0", kind: "agent", version: "2.6.0",
		executable: filepath.Join(root, "fixture-agent.exe"), asar: archive,
	}
	message, err := applyWindowsTargetsForKind([]windowsTarget{target}, "agent")
	if err == nil {
		t.Fatal("unknown Agent structure must not be connected")
	}
	if !strings.Contains(message, "Antigravity 2.0 未连接") {
		t.Fatalf("unsupported Agent was not reported: %s", message)
	}
	data, readErr := os.ReadFile(archive)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(data, original) {
		t.Fatal("Agent-only operation modified the unsupported ASAR")
	}
}

// TestOfficialIDEUserSettingsCompatibilityWhenFixturePresent prepares every
// change in memory against a downloaded official installation. It never calls
// a writer and verifies that every bundled byte remains unchanged.
func TestOfficialIDEUserSettingsCompatibilityWhenFixturePresent(t *testing.T) {
	root := os.Getenv("ANTIGRAVITY_WF_TEST_IDE_ROOT")
	if root == "" {
		t.Skip("official IDE fixture is not configured")
	}
	mainPath := filepath.Join(root, "resources", "app", "out", "main.js")
	extensionPath := filepath.Join(root, "resources", "app", "extensions", "antigravity", "dist", "extension.js")
	executable := filepath.Join(root, "Antigravity IDE.exe")
	for _, path := range []string{mainPath, extensionPath, executable} {
		if windowsExistingFile(path) == "" {
			t.Skipf("official IDE fixture is incomplete: %s", path)
		}
	}
	rendererPaths := imageGenerationUIRendererPaths(filepath.Join(root, "resources", "app"))
	if len(rendererPaths) == 0 {
		t.Skip("official IDE fixture has no known chat renderers")
	}
	productPath := filepath.Join(root, "resources", "app", "product.json")
	trackedPaths := append([]string{mainPath, extensionPath, productPath}, rendererPaths...)
	before := map[string][]byte{}
	for _, path := range trackedPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = data
	}
	t.Setenv("APPDATA", t.TempDir())
	target := windowsTarget{
		root: root, name: "Antigravity IDE", kind: "ide", version: windowsVersionFromTarget(windowsTarget{root: root, kind: "ide"}),
		executable: executable, main: mainPath, extensionEntry: extensionPath,
	}
	settingsPath := windowsSettingsPathForStatus(target)
	if settingsPath == "" {
		t.Fatal("official IDE fixture did not resolve a user settings path")
	}
	if plan, changed, err := prepareWindowsEnsureCloudCodeSetting(settingsPath, windowsBaseProxyEndpoint); err != nil || !changed || plan == nil {
		t.Fatalf("official IDE fixture did not accept an in-memory user setting plan: plan=%#v changed=%t err=%v", plan, changed, err)
	}
	rendererPlans := make([]*windowsPatchPlan, 0, len(rendererPaths))
	for _, path := range rendererPaths {
		original := before[path]
		updated, result := patchImagePreviewRenderer(string(original))
		if !result.Recognized || !result.Changed || !strings.Contains(updated, imageGenerationUIPatchMarker) || !strings.Contains(updated, imageGenerationDedupePatchMarker) {
			t.Fatalf("official renderer did not produce all required in-memory patches: %s %#v", path, result)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		rendererPlans = append(rendererPlans, &windowsPatchPlan{path: path, original: original, updated: []byte(updated), mode: info.Mode(), changed: true})
	}
	if _, err := prepareWindowsIDEProductChecksumPatch(target, rendererPlans); err != nil {
		t.Fatalf("official IDE checksum table rejected verified renderer candidates: %v", err)
	}
	for path, original := range before {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, original) {
			t.Fatalf("official fixture was modified: %s", path)
		}
	}
}

// TestMutableOfficialIDEApplyAndRestoreWhenFixturePresent performs the complete
// transaction on a disposable copy only. The caller must point the environment
// variable at an isolated fixture, never at the installed application.
func TestMutableOfficialIDEApplyAndRestoreWhenFixturePresent(t *testing.T) {
	root := os.Getenv("ANTIGRAVITY_WF_TEST_MUTABLE_IDE_ROOT")
	if root == "" {
		t.Skip("mutable official IDE fixture is not configured")
	}
	mainPath := filepath.Join(root, "resources", "app", "out", "main.js")
	extensionPath := filepath.Join(root, "resources", "app", "extensions", "antigravity", "dist", "extension.js")
	productPath := filepath.Join(root, "resources", "app", "product.json")
	rendererPaths := imageGenerationUIRendererPaths(filepath.Join(root, "resources", "app"))
	if len(rendererPaths) != 2 {
		t.Fatalf("mutable IDE fixture must contain both verified renderers: %v", rendererPaths)
	}
	for _, path := range append([]string{mainPath, extensionPath, productPath}, rendererPaths...) {
		if windowsExistingFile(path) == "" {
			t.Fatalf("mutable IDE fixture is incomplete: %s", path)
		}
	}
	executable := filepath.Join(root, "fixture-ide.exe")
	if err := os.WriteFile(executable, []byte("MZ fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := map[string][]byte{}
	for _, path := range append([]string{productPath}, rendererPaths...) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = data
	}
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("ANTIGRAVITY_BYOK_BACKUP_DIR", filepath.Join(t.TempDir(), "backups"))
	target := windowsTarget{
		root: root, name: "Antigravity IDE", kind: "ide", version: windowsVersionFromTarget(windowsTarget{root: root, kind: "ide"}),
		executable: executable, main: mainPath, extensionEntry: extensionPath,
	}
	if _, err := applyWindowsTarget(target); err != nil {
		t.Fatal(err)
	}
	for _, path := range rendererPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, marker := range []string{imagePreviewPatchMarker, imageGenerationUIPatchMarker, imageGenerationDedupePatchMarker} {
			if !bytes.Contains(data, []byte(marker)) {
				t.Fatalf("applied renderer is missing %s: %s", marker, path)
			}
		}
		if node, err := exec.LookPath("node"); err == nil {
			candidate := filepath.Join(t.TempDir(), filepath.Base(path))
			if err := os.WriteFile(candidate, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if output, err := exec.Command(node, "--check", candidate).CombinedOutput(); err != nil {
				t.Fatalf("applied official renderer failed node --check: %s: %v", output, err)
			}
		}
	}
	if err := verifyWindowsIDEProductChecksums(target, rendererPaths); err != nil {
		t.Fatal(err)
	}
	if _, err := restoreWindowsTargets([]windowsTarget{target}); err != nil {
		t.Fatal(err)
	}
	for path, original := range before {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, original) {
			t.Fatalf("apply/restore round trip did not recover original bytes: %s", path)
		}
	}
	if err := verifyWindowsIDEProductChecksums(target, rendererPaths); err != nil {
		t.Fatal(err)
	}
}

// TestMutablePatchedIDEMigrationWhenFixturePresent models the exact upgrade
// from v1.4.24's renderer markers with an unsynchronised official checksum.
// The disposable fixture must also contain clean .orig renderer backups.
func TestMutablePatchedIDEMigrationWhenFixturePresent(t *testing.T) {
	root := os.Getenv("ANTIGRAVITY_WF_TEST_MUTABLE_PATCHED_IDE_ROOT")
	if root == "" {
		t.Skip("mutable patched IDE fixture is not configured")
	}
	mainPath := filepath.Join(root, "resources", "app", "out", "main.js")
	extensionPath := filepath.Join(root, "resources", "app", "extensions", "antigravity", "dist", "extension.js")
	rendererPaths := imageGenerationUIRendererPaths(filepath.Join(root, "resources", "app"))
	if len(rendererPaths) != 2 {
		t.Fatalf("mutable patched IDE fixture must contain both verified renderers: %v", rendererPaths)
	}
	for _, path := range rendererPaths {
		active, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(active, []byte(imagePreviewPatchMarker)) || !bytes.Contains(active, []byte(imageGenerationUIPatchMarker)) || bytes.Contains(active, []byte(imageGenerationDedupePatchMarker)) {
			t.Fatalf("migration fixture is not the expected v1.4.24 pre-dedupe state: %s", path)
		}
		if windowsExistingFile(path+".orig") == "" {
			t.Fatalf("migration fixture is missing its clean renderer backup: %s.orig", path)
		}
	}
	executable := filepath.Join(root, "fixture-ide.exe")
	if err := os.WriteFile(executable, []byte("MZ fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("ANTIGRAVITY_BYOK_BACKUP_DIR", filepath.Join(t.TempDir(), "backups"))
	target := windowsTarget{
		root: root, name: "Antigravity IDE", kind: "ide", version: windowsVersionFromTarget(windowsTarget{root: root, kind: "ide"}),
		executable: executable, main: mainPath, extensionEntry: extensionPath,
	}
	if _, err := applyWindowsTarget(target); err != nil {
		t.Fatal(err)
	}
	for _, path := range rendererPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(data, []byte(imageGenerationDedupePatchMarker)) {
			t.Fatalf("pre-dedupe renderer was not migrated: %s", path)
		}
		if node, err := exec.LookPath("node"); err == nil {
			candidate := filepath.Join(t.TempDir(), filepath.Base(path))
			if err := os.WriteFile(candidate, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if output, err := exec.Command(node, "--check", candidate).CombinedOutput(); err != nil {
				t.Fatalf("migrated renderer failed node --check: %s: %v", output, err)
			}
		}
	}
	if err := verifyWindowsIDEProductChecksums(target, rendererPaths); err != nil {
		t.Fatalf("migrated v1.4.24 fixture still has an invalid product checksum: %v", err)
	}
}
