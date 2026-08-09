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
		`!define APP_SOURCE_EXE "Antigravity WF助手-v${APP_VERSION}.exe"`,
		`!define APP_SETUP_EXE "Antigravity-WF-Assistant-Windows-x64-v${APP_VERSION}-Setup.exe"`,
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
