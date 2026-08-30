#!/bin/bash
# Exercise the newly built macOS installer on an isolated GitHub Actions
# worker. This deliberately refuses developer machines: it must never install,
# remove, or replace a user's /Applications/XIASS Tools.app during a local
# source check.
set -euo pipefail

if [[ "${GITHUB_ACTIONS:-}" != "true" ]]; then
  echo "macOS installer lifecycle smoke test may run only on a GitHub Actions runner." >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SOURCE_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
VERSION="$(tr -d '\r\n' < "$SOURCE_DIR/VERSION")"
if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "VERSION must use MAJOR.MINOR.PATCH form, got: $VERSION" >&2
  exit 1
fi

INSTALLER_PATH="$SOURCE_DIR/build/bin/XIASS-Tools-macOS-universal-v${VERSION}-Installer.pkg"
if [[ ! -f "$INSTALLER_PATH" || ! -s "$INSTALLER_PATH" ]]; then
  echo "Expected installer is missing or empty: $INSTALLER_PATH" >&2
  exit 1
fi

TEMP_PARENT="${TMPDIR:-/tmp}"
SMOKE_ROOT="$(mktemp -d "$TEMP_PARENT/xiass-tools-installer-smoke.XXXXXX")"
cleanup() {
  # The target is created by mktemp above and must retain this exact prefix
  # before a root-owned installer payload is removed. Never clean a broad
  # applications directory or a user-provided path.
  case "${SMOKE_ROOT:-}" in
    "$TEMP_PARENT"/xiass-tools-installer-smoke.*)
      /usr/bin/sudo -n /bin/rm -rf "$SMOKE_ROOT"
      ;;
  esac
}
trap cleanup EXIT

CHOICES_XML="$SMOKE_ROOT/choices.xml"
cat > "$CHOICES_XML" <<'XML'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><array>
  <dict>
    <key>choiceIdentifier</key><string>desktopShortcutChoice</string>
    <key>choiceAttribute</key><string>selected</string>
    <key>attributeSetting</key><integer>0</integer>
  </dict>
</array></plist>
XML

echo "Installing the freshly built PKG into an isolated temporary target…"
/usr/bin/sudo -n /usr/sbin/installer \
  -pkg "$INSTALLER_PATH" \
  -target "$SMOKE_ROOT" \
  -applyChoiceChangesXML "$CHOICES_XML" \
  -verboseR

APP_PATH="$SMOKE_ROOT/Applications/XIASS Tools.app"
INFO_PLIST="$APP_PATH/Contents/Info.plist"
EXECUTABLE="$APP_PATH/Contents/MacOS/XIASS Tools"
if [[ ! -f "$INFO_PLIST" || ! -f "$EXECUTABLE" || ! -x "$EXECUTABLE" ]]; then
  echo "Installed XIASS Tools application layout is incomplete." >&2
  exit 1
fi

if [[ "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$INFO_PLIST")" != "com.xiass.tools" ]]; then
  echo "Installed application bundle identifier is incorrect." >&2
  exit 1
fi
if [[ "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$INFO_PLIST")" != "$VERSION" ]]; then
  echo "Installed application version is incorrect." >&2
  exit 1
fi
if [[ "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleExecutable' "$INFO_PLIST")" != "XIASS Tools" ]]; then
  echo "Installed application executable metadata is incorrect." >&2
  exit 1
fi

ARCHITECTURES="$(/usr/bin/lipo -archs "$EXECUTABLE")"
if [[ " $ARCHITECTURES " != *" arm64 "* || " $ARCHITECTURES " != *" x86_64 "* ]]; then
  echo "Installed application is not universal: $ARCHITECTURES" >&2
  exit 1
fi
/usr/bin/codesign --verify --deep --strict "$APP_PATH"

EXECUTABLE_COUNT="$(/usr/bin/find "$APP_PATH/Contents/MacOS" -maxdepth 1 -type f | /usr/bin/wc -l | /usr/bin/tr -d ' ')"
if [[ "$EXECUTABLE_COUNT" != "1" ]]; then
  echo "Installed application contains an unexpected executable layout." >&2
  exit 1
fi

echo "macOS Installer Lifecycle Smoke Test passed."
