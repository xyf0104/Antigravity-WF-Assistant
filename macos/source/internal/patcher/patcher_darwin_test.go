//go:build darwin

package patcher

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDarwinPatchApplyStatusAndRestore(t *testing.T) {
	appPath := filepath.Join(t.TempDir(), "Antigravity.app")
	mainPath := filepath.Join(appPath, "Contents", "Resources", "app", "out", "main.js")
	extensionPath := filepath.Join(appPath, "Contents", "Resources", "app", "extensions", "antigravity")
	extensionEntry := filepath.Join(extensionPath, "dist", "extension.js")
	languagePath := filepath.Join(extensionPath, "bin", "language_server_macos_x64")
	for _, dir := range []string{filepath.Dir(mainPath), filepath.Dir(extensionEntry), filepath.Dir(languagePath)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	originalMain := []byte("before " + productionEndpoint + " " + authEligibilityOriginal + " after")
	originalLanguage := []byte("binary\x00" + sandboxEndpoint + "\x00tail")
	if err := os.WriteFile(mainPath, originalMain, 0o644); err != nil {
		t.Fatal(err)
	}
	originalExtension := []byte("/*! For license information please see extension.js.LICENSE.txt */\nconst a=await x.getCloudCodeUrl(),b=await y.getCloudCodeUrl();")
	if err := os.WriteFile(extensionEntry, originalExtension, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(languagePath, originalLanguage, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("ANTIGRAVITY_APP_PATH", appPath)
	t.Setenv("ANTIGRAVITY_BYOK_BACKUP_DIR", t.TempDir())
	t.Setenv("ANTIGRAVITY_BYOK_SKIP_CODESIGN", "1")

	targets := locateDarwinTargets()
	if targets.main != mainPath || targets.language != languagePath {
		t.Fatalf("unexpected targets: %+v", targets)
	}
	if out := darwinStatus(targets); strings.Contains(out, "ide_patched=true") {
		t.Fatalf("fresh fixture unexpectedly patched: %s", out)
	}
	if _, err := applyDarwinPatch(targets); err != nil {
		t.Fatal(err)
	}
	if out := darwinStatus(targets); !strings.Contains(out, "ide_patched=true") {
		t.Fatalf("patched fixture not detected: %s", out)
	}

	patchedLanguage, err := os.ReadFile(languagePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(patchedLanguage) != len(originalLanguage) {
		t.Fatalf("binary patch changed file size: %d != %d", len(patchedLanguage), len(originalLanguage))
	}
	if !bytes.Contains(patchedLanguage, []byte(binarySandboxProxyEndpoint)) {
		t.Fatal("sandbox endpoint was not replaced")
	}
	patchedMain, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(patchedMain, []byte(authEligibilityPatched)) || bytes.Contains(patchedMain, []byte(authEligibilityOriginal)) {
		t.Fatal("local credential auth-state patch was not applied")
	}
	patchedExtension, _ := os.ReadFile(extensionEntry)
	if !bytes.Contains(patchedExtension, []byte(darwinExtensionMarker)) || bytes.Contains(patchedExtension, []byte("getCloudCodeUrl()")) {
		t.Fatal("extension cloud code calls were not patched")
	}

	if _, err := restoreDarwinPatch(targets); err != nil {
		t.Fatal(err)
	}
	restoredMain, _ := os.ReadFile(mainPath)
	restoredExtension, _ := os.ReadFile(extensionEntry)
	restoredLanguage, _ := os.ReadFile(languagePath)
	if !bytes.Equal(restoredMain, originalMain) || !bytes.Equal(restoredExtension, originalExtension) || !bytes.Equal(restoredLanguage, originalLanguage) {
		t.Fatal("restore did not reproduce the original files exactly")
	}
}

func TestDarwinCodesignRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang is unavailable")
	}
	if _, err := exec.LookPath("codesign"); err != nil {
		t.Skip("codesign is unavailable")
	}

	appPath := filepath.Join(t.TempDir(), "Antigravity.app")
	contentsPath := filepath.Join(appPath, "Contents")
	mainPath := filepath.Join(contentsPath, "Resources", "app", "out", "main.js")
	extensionPath := filepath.Join(contentsPath, "Resources", "app", "extensions", "antigravity")
	extensionEntry := filepath.Join(extensionPath, "dist", "extension.js")
	languagePath := filepath.Join(extensionPath, "bin", "language_server_macos_x64")
	electronPath := filepath.Join(contentsPath, "MacOS", "Electron")
	for _, dir := range []string{filepath.Dir(mainPath), filepath.Dir(extensionEntry), filepath.Dir(languagePath), filepath.Dir(electronPath)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	originalMain := []byte("const endpoint = '" + productionEndpoint + "';" + authEligibilityOriginal)
	if err := os.WriteFile(mainPath, originalMain, 0o644); err != nil {
		t.Fatal(err)
	}
	originalExtension := []byte("/*! For license information please see extension.js.LICENSE.txt */\nconst a=await x.getCloudCodeUrl(),b=await y.getCloudCodeUrl();")
	if err := os.WriteFile(extensionEntry, originalExtension, 0o644); err != nil {
		t.Fatal(err)
	}
	compileFixtureBinary(t, electronPath, "fixture")
	compileFixtureBinary(t, languagePath, sandboxEndpoint)
	infoPlist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleExecutable</key><string>Electron</string>
<key>CFBundleIdentifier</key><string>test.antigravity</string>
<key>CFBundlePackageType</key><string>APPL</string>
</dict></plist>`
	if err := os.WriteFile(filepath.Join(contentsPath, "Info.plist"), []byte(infoPlist), 0o644); err != nil {
		t.Fatal(err)
	}
	runTestCommand(t, "codesign", "--force", "--sign", "-", languagePath)
	runTestCommand(t, "codesign", "--force", "--deep", "--sign", "-", appPath)
	runTestCommand(t, "codesign", "--verify", "--deep", "--strict", appPath)

	originalLanguage, _ := os.ReadFile(languagePath)
	originalElectron, _ := os.ReadFile(electronPath)
	codeResourcesPath := filepath.Join(contentsPath, "_CodeSignature", "CodeResources")
	originalCodeResources, _ := os.ReadFile(codeResourcesPath)

	t.Setenv("ANTIGRAVITY_APP_PATH", appPath)
	t.Setenv("ANTIGRAVITY_BYOK_BACKUP_DIR", t.TempDir())
	t.Setenv("ANTIGRAVITY_BYOK_SKIP_CODESIGN", "")
	targets := locateDarwinTargets()
	if _, err := applyDarwinPatch(targets); err != nil {
		t.Fatal(err)
	}
	// The bundle resource seal is intentionally stale while patched so the
	// top-level executable keeps its original code identity and Keychain access.
	runTestCommand(t, "codesign", "--verify", "--strict", languagePath)
	assertFileEquals(t, electronPath, originalElectron)
	assertFileEquals(t, codeResourcesPath, originalCodeResources)

	if _, err := restoreDarwinPatch(targets); err != nil {
		t.Fatal(err)
	}
	runTestCommand(t, "codesign", "--verify", "--deep", "--strict", appPath)

	assertFileEquals(t, mainPath, originalMain)
	assertFileEquals(t, extensionEntry, originalExtension)
	assertFileEquals(t, languagePath, originalLanguage)
	assertFileEquals(t, electronPath, originalElectron)
	assertFileEquals(t, codeResourcesPath, originalCodeResources)
}

func compileFixtureBinary(t *testing.T, outputPath, marker string) {
	t.Helper()
	source := fmt.Sprintf("#include <stdio.h>\nint main(void) { puts(%q); return 0; }\n", marker)
	cmd := exec.Command("clang", "-x", "c", "-", "-o", outputPath)
	cmd.Stdin = strings.NewReader(source)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clang failed: %s: %v", out, err)
	}
}

func runTestCommand(t *testing.T, name string, args ...string) {
	t.Helper()
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		t.Fatalf("%s %v failed: %s: %v", name, args, out, err)
	}
}

func assertFileEquals(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s was not restored byte-for-byte", path)
	}
}

func TestWriteBackupPreservesFirstOriginal(t *testing.T) {
	t.Setenv("ANTIGRAVITY_BYOK_BACKUP_DIR", t.TempDir())
	sourcePath := filepath.Join(t.TempDir(), "main.js")
	first := []byte("first-original")
	if err := writeBackup(sourcePath, first); err != nil {
		t.Fatal(err)
	}
	if err := writeBackup(sourcePath, []byte("later-patched-version")); err != nil {
		t.Fatal(err)
	}
	assertFileEquals(t, backupPath(sourcePath), first)
}

func TestCurrentBackupRotatesAfterApplicationUpdate(t *testing.T) {
	backupDir := t.TempDir()
	t.Setenv("ANTIGRAVITY_BYOK_BACKUP_DIR", backupDir)
	sourcePath := filepath.Join(t.TempDir(), "main.js")
	first := []byte("version-one-original")
	second := []byte("version-two-original")
	if err := os.WriteFile(sourcePath, first, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeCurrentBackup(sourcePath, first); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, second, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeCurrentBackup(sourcePath, second); err != nil {
		t.Fatal(err)
	}
	assertFileEquals(t, backupPath(sourcePath), second)
	matches, _ := filepath.Glob(filepath.Join(backupDir, "main.js-*.previous-*.bak"))
	if len(matches) != 1 {
		t.Fatalf("previous backup was not archived: %v", matches)
	}
	assertFileEquals(t, matches[0], first)
}

func TestFinishingPartialPatchPreservesCleanRestorePoint(t *testing.T) {
	t.Setenv("ANTIGRAVITY_BYOK_BACKUP_DIR", t.TempDir())
	sourcePath := filepath.Join(t.TempDir(), "main.js")
	clean := []byte("const endpoint='" + productionEndpoint + "';")
	partial := []byte("const endpoint='" + textProxyEndpoint + "'; dynamic-app-data")
	if err := os.WriteFile(sourcePath, partial, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeBackup(sourcePath, clean); err != nil {
		t.Fatal(err)
	}
	plan := &patchPlan{path: sourcePath, original: partial, updated: append([]byte(nil), partial...), mode: 0o644, changed: true}
	if err := saveApplyBackups([]*patchPlan{plan}, nil); err != nil {
		t.Fatal(err)
	}
	assertFileEquals(t, backupPath(sourcePath), clean)
}

func TestDarwinExplicitPathNeverFallsBack(t *testing.T) {
	t.Setenv("ANTIGRAVITY_APP_PATH", filepath.Join(t.TempDir(), "Missing.app"))
	t.Setenv("ANTIGRAVITY_APP_PATHS", "")
	if targets := locateDarwinInstallations(); len(targets) != 0 {
		t.Fatalf("explicit missing path fell back to another install: %+v", targets)
	}
}

func TestDarwinASARApplyStatusAndRestore(t *testing.T) {
	appPath := filepath.Join(t.TempDir(), "Antigravity 2.0.app")
	resources := filepath.Join(appPath, "Contents", "Resources")
	asarPath := filepath.Join(resources, "app.asar")
	languagePath := filepath.Join(resources, "bin", "language_server_macos_x64")
	if err := os.MkdirAll(filepath.Dir(languagePath), 0o755); err != nil {
		t.Fatal(err)
	}
	fixture := &asarArchive{root: &asarNode{Files: map[string]*asarNode{}}}
	originalMain := []byte("\"use strict\";\nconst endpoint=\"" + productionEndpoint + "\";")
	originalLauncher := []byte("args.push('--cloud_code_endpoint','" + productionEndpoint + "')")
	if err := fixture.write(asarPath, map[string][]byte{
		"dist/main.js": originalMain, "dist/languageServer.js": originalLauncher,
	}); err != nil {
		t.Fatal(err)
	}
	originalASAR, _ := os.ReadFile(asarPath)
	originalLanguage := []byte("binary\x00" + sandboxEndpoint + "\x00tail")
	if err := os.WriteFile(languagePath, originalLanguage, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("ANTIGRAVITY_APP_PATH", appPath)
	t.Setenv("ANTIGRAVITY_APP_PATHS", "")
	t.Setenv("ANTIGRAVITY_BYOK_BACKUP_DIR", t.TempDir())
	t.Setenv("ANTIGRAVITY_BYOK_SKIP_CODESIGN", "1")
	targets := locateDarwinInstallations()
	if len(targets) != 1 || targets[0].kind != "agent" || targets[0].asar != asarPath {
		t.Fatalf("unexpected ASAR target: %+v", targets)
	}
	if _, err := applyDarwinPatch(targets[0]); err != nil {
		t.Fatal(err)
	}
	if !darwinASARPatched(asarPath) {
		t.Fatal("patched ASAR was not detected")
	}
	if _, _, _, patched := darwinTargetPatchState(targets[0]); !patched {
		t.Fatal("ASAR target was not fully patched")
	}
	if _, err := restoreDarwinPatch(targets[0]); err != nil {
		t.Fatal(err)
	}
	restoredASAR, _ := os.ReadFile(asarPath)
	restoredLanguage, _ := os.ReadFile(languagePath)
	if !bytes.Equal(restoredASAR, originalASAR) || !bytes.Equal(restoredLanguage, originalLanguage) {
		t.Fatal("ASAR installation was not restored byte-for-byte")
	}
}

func TestMergeDarwinHistoryBacksUpAndNeverOverwrites(t *testing.T) {
	base := t.TempDir()
	t.Setenv("ANTIGRAVITY_BYOK_GEMINI_DIR", base)
	source := filepath.Join(base, "antigravity-ide")
	target := filepath.Join(base, "antigravity")
	for _, dir := range []string{filepath.Join(source, "conversations"), filepath.Join(target, "conversations")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(source, "conversations", "new.json"), []byte("source-new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "conversations", "same.json"), []byte("source-value"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "conversations", "same.json"), []byte("target-value"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := mergeDarwinHistory(); err != nil {
		t.Fatal(err)
	}
	assertFileEquals(t, filepath.Join(target, "conversations", "new.json"), []byte("source-new"))
	assertFileEquals(t, filepath.Join(target, "conversations", "same.json"), []byte("target-value"))
	assertFileEquals(t, filepath.Join(base, "antigravity-ide.antigravity-byok-backup", "conversations", "same.json"), []byte("source-value"))
}

func TestSyncDarwinHistoryDiscoversAllLegacyDirectories(t *testing.T) {
	base := t.TempDir()
	t.Setenv("ANTIGRAVITY_BYOK_GEMINI_DIR", base)
	target := filepath.Join(base, "antigravity")
	sources := []string{
		filepath.Join(base, "antigravity-ide"),
		filepath.Join(base, "antigravity-2.0"),
	}
	for _, dir := range append(sources, target) {
		if err := os.MkdirAll(filepath.Join(dir, "conversations"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(sources[0], "conversations", "ide.pb"), []byte("ide"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sources[1], "conversations", "agent2.pb"), []byte("agent2"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sources[0], "conversations", "active.db-wal"), []byte("transient"), 0o600); err != nil {
		t.Fatal(err)
	}

	summary, err := syncDarwinHistory()
	if err != nil {
		t.Fatal(err)
	}
	if summary.sources != 2 || summary.copied != 2 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	assertFileEquals(t, filepath.Join(target, "conversations", "ide.pb"), []byte("ide"))
	assertFileEquals(t, filepath.Join(target, "conversations", "agent2.pb"), []byte("agent2"))
	if _, err := os.Stat(filepath.Join(target, "conversations", "active.db-wal")); !os.IsNotExist(err) {
		t.Fatalf("transient SQLite file should not be restored: %v", err)
	}
	for _, source := range sources {
		if info, err := os.Stat(source + ".antigravity-byok-backup"); err != nil || !info.IsDir() {
			t.Fatalf("backup missing for %s: %v", source, err)
		}
	}

	second, err := syncDarwinHistory()
	if err != nil {
		t.Fatal(err)
	}
	if second.copied != 0 {
		t.Fatalf("history sync must be idempotent: %+v", second)
	}
}

func TestRunDarwinSyncHistoryDoesNotRequireInstalledApplication(t *testing.T) {
	base := t.TempDir()
	t.Setenv("ANTIGRAVITY_BYOK_GEMINI_DIR", base)
	message, err := runDarwin("sync-history")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(message, "历史会话检查完成") {
		t.Fatalf("unexpected message: %q", message)
	}
}

func TestDarwinPatchAgainstInstalledVersionFixture(t *testing.T) {
	if os.Getenv("ANTIGRAVITY_BYOK_TEST_INSTALLED") != "1" {
		t.Skip("set ANTIGRAVITY_BYOK_TEST_INSTALLED=1 for the installed-version fixture")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	installedExtension := "/Applications/Antigravity.app/Contents/Resources/app/extensions/antigravity/dist/extension.js"
	installedMainBackup := filepath.Join(home, ".antigravity-byok", "backups", "main.js-5b315d1067653b1a.bak")
	installedLanguageBackup := filepath.Join(home, ".antigravity-byok", "backups", "language_server_macos_x64-40bf34d68e27fffb.bak")
	for _, path := range []string{installedExtension, installedMainBackup, installedLanguageBackup} {
		if existingFile(path) == "" {
			t.Skipf("installed fixture source is unavailable: %s", path)
		}
	}

	appPath := filepath.Join(t.TempDir(), "Antigravity.app")
	mainPath := filepath.Join(appPath, "Contents", "Resources", "app", "out", "main.js")
	extensionPath := filepath.Join(appPath, "Contents", "Resources", "app", "extensions", "antigravity", "dist", "extension.js")
	languagePath := filepath.Join(appPath, "Contents", "Resources", "app", "extensions", "antigravity", "bin", "language_server_macos_x64")
	for _, pair := range [][2]string{
		{installedMainBackup, mainPath}, {installedExtension, extensionPath}, {installedLanguageBackup, languagePath},
	} {
		info, err := os.Stat(pair[0])
		if err != nil {
			t.Fatal(err)
		}
		if err := copyFileAtomic(pair[0], pair[1], info.Mode()); err != nil {
			t.Fatal(err)
		}
	}
	originalMain, _ := fileSHA256(mainPath)
	originalExtension, _ := fileSHA256(extensionPath)
	originalLanguage, _ := fileSHA256(languagePath)

	t.Setenv("ANTIGRAVITY_APP_PATH", appPath)
	t.Setenv("ANTIGRAVITY_APP_PATHS", "")
	t.Setenv("ANTIGRAVITY_BYOK_BACKUP_DIR", t.TempDir())
	t.Setenv("ANTIGRAVITY_BYOK_GEMINI_DIR", t.TempDir())
	t.Setenv("ANTIGRAVITY_BYOK_SKIP_CODESIGN", "1")
	targets := locateDarwinInstallations()
	if len(targets) != 1 {
		t.Fatalf("fixture discovery returned %+v", targets)
	}
	if _, err := applyDarwinPatch(targets[0]); err != nil {
		t.Fatal(err)
	}
	if _, _, _, patched := darwinTargetPatchState(targets[0]); !patched {
		t.Fatal("installed-version fixture was not fully patched")
	}
	if _, err := restoreDarwinPatch(targets[0]); err != nil {
		t.Fatal(err)
	}
	restoredMain, _ := fileSHA256(mainPath)
	restoredExtension, _ := fileSHA256(extensionPath)
	restoredLanguage, _ := fileSHA256(languagePath)
	if restoredMain != originalMain || restoredExtension != originalExtension || restoredLanguage != originalLanguage {
		t.Fatal("installed-version fixture was not restored byte-for-byte")
	}
}
