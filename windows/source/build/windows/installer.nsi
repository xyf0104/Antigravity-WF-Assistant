Unicode True
SetCompressor /SOLID lzma

!include "MUI2.nsh"
!include "LogicLib.nsh"

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

Section "安装 ${APP_NAME}" MainSection
  SetShellVarContext current
  SetOutPath "$INSTDIR"
  SetOverwrite on
  File "/oname=${APP_EXE}" "..\bin\${APP_SOURCE_EXE}"
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
  ReadRegDWORD $0 HKCU "${APP_UNINSTALL_KEY}" "DesktopShortcut"
  ${If} $0 = 1
    Delete "$DESKTOP\${APP_NAME}.lnk"
  ${EndIf}
  Delete "$INSTDIR\${APP_EXE}"
  Delete "$INSTDIR\${APP_UNINSTALL_EXE}"
  ; Remove the pre-v1.6.6 internal filename too, so upgrades do not leave an
  ; obsolete standalone uninstaller in the application directory.
  Delete "$INSTDIR\卸载 ${APP_NAME}.exe"
  RMDir /r "$SMPROGRAMS\${APP_NAME}"
  RMDir "$INSTDIR"
  DeleteRegKey HKCU "${APP_UNINSTALL_KEY}"
SectionEnd
