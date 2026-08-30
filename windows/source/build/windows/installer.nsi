Unicode True
SetCompressor /SOLID lzma

!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "WordFunc.nsh"

!define APP_NAME "XIASS Tools"
!define APP_EXE "XIASS Tools.exe"
!define APP_UNINSTALL_EXE "Uninstall XIASS Tools.exe"
; Keep the installed executable name stable while packaging a versioned build
; artifact. This lets local and CI builds coexist without overwriting a
; previous executable before NSIS has produced a verified installer.
; Every build must pass APP_VERSION from the source-root VERSION file.  A
; silent fallback here could package a future executable under an old Setup
; filename and metadata, so fail the local build rather than guessing.
!ifndef APP_VERSION
!error "APP_VERSION is required. Run makensis with /DAPP_VERSION=<VERSION>."
!endif
!ifndef APP_SOURCE_EXE
!define APP_SOURCE_EXE "XIASS Tools-v${APP_VERSION}.exe"
!endif
!ifndef APP_SETUP_EXE
!define APP_SETUP_EXE "XIASS-Tools-Windows-x64-v${APP_VERSION}-Setup.exe"
!endif
!define APP_PUBLISHER "XIASS Tools"
!define WEBVIEW2_CLIENT_KEY "SOFTWARE\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}"
!define WEBVIEW2_BOOTSTRAPPER "MicrosoftEdgeWebview2Setup.exe"
; This legacy upgrade-compatibility key is intentionally retained so an
; in-place upgrade keeps the existing uninstall entry and optional
; desktop-shortcut ownership marker. It is not product branding.
!define APP_UNINSTALL_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\AntigravityWFAssistant"

Name "${APP_NAME}"
OutFile "..\bin\${APP_SETUP_EXE}"
InstallDir "$LOCALAPPDATA\Programs\${APP_NAME}"
InstallDirRegKey HKCU "${APP_UNINSTALL_KEY}" "InstallLocation"
RequestExecutionLevel user
ShowInstDetails show
ShowUninstDetails show

VIProductVersion "${APP_VERSION}.0"
VIAddVersionKey /LANG=2052 "ProductName" "${APP_NAME}"
VIAddVersionKey /LANG=2052 "FileDescription" "${APP_NAME} 安装程序"
VIAddVersionKey /LANG=2052 "CompanyName" "${APP_PUBLISHER}"
VIAddVersionKey /LANG=2052 "LegalCopyright" "Copyright (c) 2026 XIASS Tools"
VIAddVersionKey /LANG=2052 "FileVersion" "${APP_VERSION}"
VIAddVersionKey /LANG=2052 "ProductVersion" "${APP_VERSION}"

!define MUI_ICON "icon.ico"
!define MUI_UNICON "icon.ico"
!define MUI_ABORTWARNING
!define MUI_FINISHPAGE_RUN "$INSTDIR\${APP_EXE}"
!define MUI_FINISHPAGE_RUN_TEXT "立即运行 ${APP_NAME}"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_COMPONENTS
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "SimpChinese"

Var WebView2Version

; Microsoft documents the Evergreen Runtime's 32-bit registry view for x64
; Windows. A non-empty pv value strictly newer than 0.0.0.0 in either HKLM or
; HKCU is the supported installer-time presence test.
Function DetectWebView2Runtime
  StrCpy $WebView2Version ""
  SetRegView 32

  ClearErrors
  ReadRegStr $R0 HKLM "${WEBVIEW2_CLIENT_KEY}" "pv"
  IfErrors webview2_detect_current_user
  ${VersionCompare} "$R0" "0.0.0.0" $R1
  StrCmp $R1 "1" webview2_detect_machine_found

webview2_detect_current_user:
  ClearErrors
  ReadRegStr $R0 HKCU "${WEBVIEW2_CLIENT_KEY}" "pv"
  IfErrors webview2_detect_done
  ${VersionCompare} "$R0" "0.0.0.0" $R1
  StrCmp $R1 "1" webview2_detect_user_found webview2_detect_done

webview2_detect_machine_found:
  StrCpy $WebView2Version "$R0"
  Goto webview2_detect_done

webview2_detect_user_found:
  StrCpy $WebView2Version "$R0"

webview2_detect_done:
FunctionEnd

; Install the small architecture-selecting Evergreen Bootstrapper only when
; the supported registry test says the Runtime is absent. The downloaded file
; is accepted only after Windows validates Microsoft's Authenticode signature.
; A final registry poll is mandatory because bootstrapper child processes can
; finish shortly after the first process exits.
Function EnsureWebView2Runtime
  Call DetectWebView2Runtime
  StrCmp $WebView2Version "" webview2_install_required webview2_install_present

webview2_install_present:
  DetailPrint "已检测到 Microsoft Edge WebView2 Runtime $WebView2Version。"
  Return

webview2_install_required:
  DetailPrint "未检测到 Microsoft Edge WebView2 Runtime，正在从 Microsoft 安全下载 Evergreen Bootstrapper…"
  SetOutPath "$PLUGINSDIR"
  File "/oname=download-webview2-bootstrapper.ps1" "download-webview2-bootstrapper.ps1"
  nsExec::ExecToStack /TIMEOUT=180000 '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$PLUGINSDIR\download-webview2-bootstrapper.ps1" -Destination "$PLUGINSDIR\${WEBVIEW2_BOOTSTRAPPER}"'
  Pop $R0
  Pop $R1
  Delete "$PLUGINSDIR\download-webview2-bootstrapper.ps1"
  StrCmp $R0 "0" webview2_download_ready webview2_download_failed

webview2_download_ready:
  IfFileExists "$PLUGINSDIR\${WEBVIEW2_BOOTSTRAPPER}" 0 webview2_download_failed
  DetailPrint "正在静默安装 Microsoft Edge WebView2 Runtime…"
  nsExec::ExecToStack /TIMEOUT=900000 '"$PLUGINSDIR\${WEBVIEW2_BOOTSTRAPPER}" /silent /install'
  Pop $R2
  Pop $R3

  ; Do not trust the process exit code alone. Re-read the Microsoft-documented
  ; pv keys for up to five minutes so a still-finishing child installer cannot
  ; leave XIASS Tools installed with a blank WebView window.
  StrCpy $R4 0
webview2_verify_loop:
  Call DetectWebView2Runtime
  StrCmp $WebView2Version "" 0 webview2_install_verified
  IntCmp $R4 300 webview2_install_failed webview2_verify_wait webview2_install_failed
webview2_verify_wait:
  Sleep 1000
  IntOp $R4 $R4 + 1
  Goto webview2_verify_loop

webview2_install_verified:
  Delete "$PLUGINSDIR\${WEBVIEW2_BOOTSTRAPPER}"
  DetailPrint "Microsoft Edge WebView2 Runtime $WebView2Version 已就绪。"
  Return

webview2_download_failed:
  DetailPrint "WebView2 Runtime 下载失败。PowerShell 返回：$R0"
  Goto webview2_prerequisite_failed

webview2_install_failed:
  Delete "$PLUGINSDIR\${WEBVIEW2_BOOTSTRAPPER}"
  DetailPrint "WebView2 Runtime 安装或复检失败。Bootstrapper 返回：$R2"

webview2_prerequisite_failed:
  Delete "$PLUGINSDIR\download-webview2-bootstrapper.ps1"
  Delete "$PLUGINSDIR\${WEBVIEW2_BOOTSTRAPPER}"
  IfSilent webview2_prerequisite_abort
  MessageBox MB_ICONSTOP|MB_OK "无法安装 Microsoft Edge WebView2 Runtime，因此未安装 ${APP_NAME}，以避免首次启动白屏。请检查网络、代理或企业安全策略后重试。"
webview2_prerequisite_abort:
  SetErrorLevel 2
  Abort
FunctionEnd

Section "安装 ${APP_NAME}" MainSection
  SetShellVarContext current
  Call EnsureWebView2Runtime
  SetOutPath "$INSTDIR"
  SetOverwrite on
  ; A running XIASS Tools process keeps its executable locked. Do not silently
  ; continue an upgrade with a partial payload: the user can exit from the
  ; notification-area menu and rerun this installer without any force kill.
  ClearErrors
  File "/oname=${APP_EXE}" "..\bin\${APP_SOURCE_EXE}"
  IfErrors 0 install_payload_ready
  MessageBox MB_ICONEXCLAMATION|MB_OK "无法替换正在使用的 ${APP_NAME}。请先从右下角通知区域的 XIASS Tools 图标选择“退出 XIASS Tools”，再重新运行安装程序。"
  Abort
install_payload_ready:
  ; Use a stable ASCII executable filename so Windows Shell shortcuts and
  ; silent upgrades do not depend on Unicode executable-path handling. The
  ; user-facing Start Menu label remains Chinese below.
  Delete "$INSTDIR\卸载 ${APP_NAME}.exe"
  WriteUninstaller "$INSTDIR\${APP_UNINSTALL_EXE}"

  CreateDirectory "$SMPROGRAMS\${APP_NAME}"
  CreateShortcut "$SMPROGRAMS\${APP_NAME}\${APP_NAME}.lnk" "$INSTDIR\${APP_EXE}" "" "$INSTDIR\${APP_EXE}" 0
  CreateShortcut "$SMPROGRAMS\${APP_NAME}\卸载 ${APP_NAME}.lnk" "$INSTDIR\${APP_UNINSTALL_EXE}"

  ; The optional shortcut section records whether this installer created the
  ; desktop link, so uninstall does not touch a user-created link. Preserve
  ; that marker on an upgrade when the optional section is left unchecked:
  ; the existing link remains valid and must still be removed on uninstall.
  WriteRegStr HKCU "${APP_UNINSTALL_KEY}" "DisplayName" "${APP_NAME}"
  WriteRegStr HKCU "${APP_UNINSTALL_KEY}" "DisplayVersion" "${APP_VERSION}"
  WriteRegStr HKCU "${APP_UNINSTALL_KEY}" "Publisher" "${APP_PUBLISHER}"
  WriteRegStr HKCU "${APP_UNINSTALL_KEY}" "DisplayIcon" "$INSTDIR\${APP_EXE}"
  WriteRegStr HKCU "${APP_UNINSTALL_KEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${APP_UNINSTALL_KEY}" "UninstallString" '"$INSTDIR\${APP_UNINSTALL_EXE}"'
  WriteRegDWORD HKCU "${APP_UNINSTALL_KEY}" "NoModify" 1
  WriteRegDWORD HKCU "${APP_UNINSTALL_KEY}" "NoRepair" 1
SectionEnd

Section /o "在桌面创建快捷方式" DesktopShortcut
  SetShellVarContext current
  CreateShortcut "$DESKTOP\${APP_NAME}.lnk" "$INSTDIR\${APP_EXE}" "" "$INSTDIR\${APP_EXE}" 0
  WriteRegDWORD HKCU "${APP_UNINSTALL_KEY}" "DesktopShortcut" 1
SectionEnd

Section "Uninstall"
  SetShellVarContext current
  ; Never remove shortcuts/registry metadata after a failed executable delete.
  ; This gives a user with the app still running a safe retry path instead of
  ; an apparently successful uninstall that leaves an orphaned process/file.
  IfFileExists "$INSTDIR\${APP_EXE}" 0 uninstall_payload_removed
  ClearErrors
  Delete "$INSTDIR\${APP_EXE}"
  IfErrors 0 uninstall_payload_removed
  MessageBox MB_ICONEXCLAMATION|MB_OK "${APP_NAME} 仍在运行，尚未卸载。请先从右下角通知区域的 XIASS Tools 图标选择“退出 XIASS Tools”，再重新运行卸载程序。"
  Abort
uninstall_payload_removed:
  ReadRegDWORD $0 HKCU "${APP_UNINSTALL_KEY}" "DesktopShortcut"
  ${If} $0 = 1
    Delete "$DESKTOP\${APP_NAME}.lnk"
  ${EndIf}
  Delete "$INSTDIR\${APP_UNINSTALL_EXE}"
  ; Remove the pre-v1.6.6 internal filename too, so upgrades do not leave an
  ; obsolete standalone uninstaller in the application directory.
  Delete "$INSTDIR\卸载 ${APP_NAME}.exe"
  RMDir /r "$SMPROGRAMS\${APP_NAME}"
  RMDir "$INSTDIR"
  DeleteRegKey HKCU "${APP_UNINSTALL_KEY}"
SectionEnd
