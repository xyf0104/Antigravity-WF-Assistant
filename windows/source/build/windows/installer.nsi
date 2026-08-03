Unicode True
SetCompressor /SOLID lzma

!include "MUI2.nsh"
!include "LogicLib.nsh"

!define APP_NAME "Antigravity WF助手"
!define APP_EXE "Antigravity WF助手.exe"
!define APP_VERSION "1.3.4"
!define APP_PUBLISHER "WF"
!define APP_UNINSTALL_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\AntigravityWFAssistant"

Name "${APP_NAME}"
OutFile "..\bin\Antigravity-WF-Assistant-Windows-x64-v1.3.4-Setup.exe"
InstallDir "$LOCALAPPDATA\Programs\${APP_NAME}"
InstallDirRegKey HKCU "${APP_UNINSTALL_KEY}" "InstallLocation"
RequestExecutionLevel user
ShowInstDetails show
ShowUninstDetails show

VIProductVersion "1.3.4.0"
VIAddVersionKey /LANG=2052 "ProductName" "${APP_NAME}"
VIAddVersionKey /LANG=2052 "FileDescription" "${APP_NAME} 安装程序"
VIAddVersionKey /LANG=2052 "CompanyName" "${APP_PUBLISHER}"
VIAddVersionKey /LANG=2052 "LegalCopyright" "Copyright (c) 2026 WF"
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
  File "/oname=${APP_EXE}" "..\bin\${APP_EXE}"
  WriteUninstaller "$INSTDIR\卸载 ${APP_NAME}.exe"

  CreateDirectory "$SMPROGRAMS\${APP_NAME}"
  CreateShortcut "$SMPROGRAMS\${APP_NAME}\${APP_NAME}.lnk" "$INSTDIR\${APP_EXE}" "" "$INSTDIR\${APP_EXE}" 0
  CreateShortcut "$SMPROGRAMS\${APP_NAME}\卸载 ${APP_NAME}.lnk" "$INSTDIR\卸载 ${APP_NAME}.exe"

  ; The optional shortcut section below records whether this installer created
  ; the desktop link, so uninstall does not touch a user-created link.
  DeleteRegValue HKCU "${APP_UNINSTALL_KEY}" "DesktopShortcut"
  WriteRegStr HKCU "${APP_UNINSTALL_KEY}" "DisplayName" "${APP_NAME}"
  WriteRegStr HKCU "${APP_UNINSTALL_KEY}" "DisplayVersion" "${APP_VERSION}"
  WriteRegStr HKCU "${APP_UNINSTALL_KEY}" "Publisher" "${APP_PUBLISHER}"
  WriteRegStr HKCU "${APP_UNINSTALL_KEY}" "DisplayIcon" "$INSTDIR\${APP_EXE}"
  WriteRegStr HKCU "${APP_UNINSTALL_KEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${APP_UNINSTALL_KEY}" "UninstallString" '"$INSTDIR\卸载 ${APP_NAME}.exe"'
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
  Delete "$INSTDIR\卸载 ${APP_NAME}.exe"
  RMDir /r "$SMPROGRAMS\${APP_NAME}"
  RMDir "$INSTDIR"
  DeleteRegKey HKCU "${APP_UNINSTALL_KEY}"
SectionEnd
