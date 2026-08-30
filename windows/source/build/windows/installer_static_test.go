package windowsinstaller

import (
	"os"
	"strings"
	"testing"
)

func TestDesktopShortcutOwnershipSurvivesUpgrade(t *testing.T) {
	script, err := os.ReadFile("installer.nsi")
	if err != nil {
		t.Fatalf("read NSIS installer: %v", err)
	}
	contents := string(script)

	main := nsisSection(t, contents, `Section "安装 ${APP_NAME}" MainSection`)
	if strings.Contains(main, `DeleteRegValue HKCU "${APP_UNINSTALL_KEY}" "DesktopShortcut"`) {
		t.Fatal("main install section clears the shortcut-ownership marker during an upgrade")
	}

	desktop := nsisSection(t, contents, `Section /o "在桌面创建快捷方式" DesktopShortcut`)
	for _, required := range []string{
		`CreateShortcut "$DESKTOP\${APP_NAME}.lnk" "$INSTDIR\${APP_EXE}"`,
		`WriteRegDWORD HKCU "${APP_UNINSTALL_KEY}" "DesktopShortcut" 1`,
	} {
		if !strings.Contains(desktop, required) {
			t.Fatalf("optional desktop shortcut section is missing %q", required)
		}
	}

	uninstall := nsisSection(t, contents, `Section "Uninstall"`)
	for _, required := range []string{
		`ReadRegDWORD $0 HKCU "${APP_UNINSTALL_KEY}" "DesktopShortcut"`,
		`Delete "$DESKTOP\${APP_NAME}.lnk"`,
	} {
		if !strings.Contains(uninstall, required) {
			t.Fatalf("uninstall section is missing %q", required)
		}
	}
}

func TestVersionedArtifactsDeriveFromBuildVersion(t *testing.T) {
	script, err := os.ReadFile("installer.nsi")
	if err != nil {
		t.Fatalf("read NSIS installer: %v", err)
	}
	contents := string(script)

	for _, required := range []string{
		"!ifndef APP_VERSION",
		`!error "APP_VERSION is required. Run makensis with /DAPP_VERSION=<VERSION>."`,
		`!define APP_SOURCE_EXE "XIASS Tools-v${APP_VERSION}.exe"`,
		`!define APP_SETUP_EXE "XIASS-Tools-Windows-x64-v${APP_VERSION}-Setup.exe"`,
		`!define APP_UNINSTALL_EXE "Uninstall XIASS Tools.exe"`,
		`WriteUninstaller "$INSTDIR\${APP_UNINSTALL_EXE}"`,
		`CreateShortcut "$SMPROGRAMS\${APP_NAME}\卸载 ${APP_NAME}.lnk" "$INSTDIR\${APP_UNINSTALL_EXE}"`,
		`Delete "$INSTDIR\卸载 ${APP_NAME}.exe"`,
		`OutFile "..\bin\${APP_SETUP_EXE}"`,
		`VIProductVersion "${APP_VERSION}.0"`,
	} {
		if !strings.Contains(contents, required) {
			t.Fatalf("installer versioning no longer derives from APP_VERSION: missing %q", required)
		}
	}
	guardStart := strings.Index(contents, "!ifndef APP_VERSION")
	guardEnd := strings.Index(contents[guardStart:], "!endif")
	if guardEnd < 0 {
		t.Fatal("APP_VERSION guard has no closing !endif")
	}
	versionGuard := contents[guardStart : guardStart+guardEnd+len("!endif")]
	if strings.Contains(versionGuard, "!define APP_VERSION") {
		t.Fatal("installer must not silently fall back to any checked-in APP_VERSION")
	}
}

func TestInstallerLifecycleSmokeScriptUsesRealSetupWithoutCleanupShortcuts(t *testing.T) {
	script, err := os.ReadFile("smoke-installer.ps1")
	if err != nil {
		t.Fatalf("read installer lifecycle smoke script: %v", err)
	}
	contents := string(script)
	for _, required := range []string{
		"GITHUB_ACTIONS",
		"Get-UninstallEntries",
		"Registry64",
		"Registry32",
		"HashSet[string]",
		"$seen.Add($identity)",
		"Assert-ShortcutTarget",
		"Get-NativeShortcutTarget",
		"IShellLinkW",
		"IPersistFile",
		"NativeShortcut",
		"Trim([char]'\"')",
		"Get-Item -LiteralPath $targetPath",
		"StringComparison]::OrdinalIgnoreCase",
		"Start-Process -FilePath $setupPath",
		"Start-Process -FilePath $uninstaller",
		"Test-InstallerStateAbsent",
		"Windows Installer Lifecycle Smoke Test passed.",
	} {
		if !strings.Contains(contents, required) {
			t.Fatalf("installer lifecycle smoke script is missing %q", required)
		}
	}
	for _, forbidden := range []string{"Remove-Item", "taskkill", "Stop-Process", "Start-Process -FilePath $mainExecutable"} {
		if strings.Contains(contents, forbidden) {
			t.Fatalf("installer lifecycle smoke script must not use %q", forbidden)
		}
	}
}

func nsisSection(t *testing.T, script, header string) string {
	t.Helper()
	start := strings.Index(script, header)
	if start < 0 {
		t.Fatalf("NSIS section %q not found", header)
	}
	section := script[start:]
	end := strings.Index(section, "SectionEnd")
	if end < 0 {
		t.Fatalf("NSIS section %q has no SectionEnd", header)
	}
	return section[:end]
}
