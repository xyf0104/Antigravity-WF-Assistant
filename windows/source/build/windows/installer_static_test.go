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

func TestInstallerAndUninstallerFailClosedWhenMainExecutableIsLocked(t *testing.T) {
	script, err := os.ReadFile("installer.nsi")
	if err != nil {
		t.Fatalf("read NSIS installer: %v", err)
	}
	contents := string(script)

	main := nsisSection(t, contents, `Section "安装 ${APP_NAME}" MainSection`)
	for _, required := range []string{
		`ClearErrors`,
		`File "/oname=${APP_EXE}" "..\bin\${APP_SOURCE_EXE}"`,
		`IfErrors 0 install_payload_ready`,
		`请先从右下角通知区域的 XIASS Tools 图标选择“退出 XIASS Tools”`,
		`Abort`,
	} {
		if !strings.Contains(main, required) {
			t.Fatalf("main install section is missing locked-process guard %q", required)
		}
	}

	uninstall := nsisSection(t, contents, `Section "Uninstall"`)
	for _, required := range []string{
		`IfFileExists "$INSTDIR\${APP_EXE}" 0 uninstall_payload_removed`,
		`Delete "$INSTDIR\${APP_EXE}"`,
		`IfErrors 0 uninstall_payload_removed`,
		`尚未卸载`,
		`Abort`,
	} {
		if !strings.Contains(uninstall, required) {
			t.Fatalf("uninstall section is missing locked-process guard %q", required)
		}
	}

	if strings.Contains(uninstall, "taskkill") || strings.Contains(uninstall, "Stop-Process") {
		t.Fatal("uninstaller must not force-terminate XIASS Tools")
	}
}

func TestInstallerEnsuresOfficialEvergreenWebView2RuntimeBeforeCopyingApp(t *testing.T) {
	script, err := os.ReadFile("installer.nsi")
	if err != nil {
		t.Fatalf("read NSIS installer: %v", err)
	}
	contents := string(script)
	for _, required := range []string{
		`!define WEBVIEW2_CLIENT_KEY "SOFTWARE\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}"`,
		`Function DetectWebView2Runtime`,
		`SetRegView 32`,
		`ReadRegStr $R0 HKLM "${WEBVIEW2_CLIENT_KEY}" "pv"`,
		`ReadRegStr $R0 HKCU "${WEBVIEW2_CLIENT_KEY}" "pv"`,
		`${VersionCompare} "$R0" "0.0.0.0" $R1`,
		`Function EnsureWebView2Runtime`,
		`File "/oname=download-webview2-bootstrapper.ps1" "download-webview2-bootstrapper.ps1"`,
		`MicrosoftEdgeWebview2Setup.exe`,
		`/silent /install`,
		`webview2_verify_loop:`,
		`Call DetectWebView2Runtime`,
		`SetErrorLevel 2`,
		`Abort`,
	} {
		if !strings.Contains(contents, required) {
			t.Fatalf("installer WebView2 prerequisite flow is missing %q", required)
		}
	}

	main := nsisSection(t, contents, `Section "安装 ${APP_NAME}" MainSection`)
	ensureIndex := strings.Index(main, "Call EnsureWebView2Runtime")
	payloadIndex := strings.Index(main, `File "/oname=${APP_EXE}"`)
	if ensureIndex < 0 || payloadIndex < 0 || ensureIndex >= payloadIndex {
		t.Fatal("WebView2 Runtime must be verified before the XIASS Tools payload is copied")
	}

	downloadScript, err := os.ReadFile("download-webview2-bootstrapper.ps1")
	if err != nil {
		t.Fatalf("read WebView2 downloader: %v", err)
	}
	downloader := string(downloadScript)
	for _, required := range []string{
		"https://go.microsoft.com/fwlink/p/?LinkId=2124703",
		"Invoke-WebRequest",
		"[IO.Path]::GetTempPath()",
		"MicrosoftEdgeWebview2Setup.exe",
		"Get-AuthenticodeSignature",
		"SignatureStatus]::Valid",
		"CN=Microsoft Corporation",
	} {
		if !strings.Contains(downloader, required) {
			t.Fatalf("WebView2 downloader is missing official download validation %q", required)
		}
	}
	if strings.Contains(downloader, "http://") {
		t.Fatal("WebView2 downloader must never use a cleartext transport")
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
		"System.Management.Automation.Language.Parser",
		"Get-UninstallEntries",
		"Registry64",
		"Registry32",
		"HashSet[string]",
		"$seen.Add($identity)",
		"Assert-ShortcutTarget",
		"Trim([char]'\"')",
		"Get-Item -LiteralPath $targetPath",
		"StringComparison]::OrdinalIgnoreCase",
		"Start-Process -FilePath $setupPath",
		"Start-Process -FilePath $startMenuUninstallShortcut",
		"Test-InstallerStateAbsent",
		"Get-WebView2RuntimeVersions",
		"WebView2 Runtime was not available after installation",
		"Windows Installer Lifecycle Smoke Test passed.",
	} {
		if !strings.Contains(contents, required) {
			t.Fatalf("installer lifecycle smoke script is missing %q", required)
		}
	}
	for _, forbidden := range []string{"Remove-Item", "taskkill", "Stop-Process", "Start-Process -FilePath $mainExecutable", "Start-Process -FilePath $uninstaller"} {
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
