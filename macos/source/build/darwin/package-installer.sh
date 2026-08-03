#!/bin/bash
# Build a signed-identity-independent macOS installer. The optional component
# creates a desktop alias without replacing any file the user already has.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SOURCE_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
VERSION="$(tr -d '\r\n' < "$SOURCE_DIR/VERSION")"
APP_PATH="${1:-$SOURCE_DIR/build/bin/Antigravity WF助手.app}"
OUTPUT_PATH="${2:-$SOURCE_DIR/build/bin/Antigravity-WF-Assistant-macOS-universal-v${VERSION}-Installer.pkg}"

if [[ ! -d "$APP_PATH" ]]; then
  echo "App bundle not found: $APP_PATH" >&2
  exit 1
fi

WORK_DIR="$(mktemp -d -t antigravity-wf-package)"
trap 'rm -rf "$WORK_DIR"' EXIT
APP_ROOT="$WORK_DIR/root"
PKG_DIR="$WORK_DIR/packages"
COMPONENT_PLIST="$WORK_DIR/components.plist"
mkdir -p "$APP_ROOT/Applications" "$PKG_DIR" "$(dirname "$OUTPUT_PATH")"
ditto "$APP_PATH" "$APP_ROOT/Applications/Antigravity WF助手.app"

# pkgbuild marks application bundles as relocatable by default. That makes
# Installer search Spotlight for another bundle with the same identifier and
# install there instead of /Applications, which also leaves a desktop link
# pointing at a missing app. Explicitly disable relocation for this product.
pkgbuild --analyze --root "$APP_ROOT" "$COMPONENT_PLIST"
/usr/libexec/PlistBuddy -c 'Set :0:BundleIsRelocatable false' "$COMPONENT_PLIST"

pkgbuild \
  --root "$APP_ROOT" \
  --component-plist "$COMPONENT_PLIST" \
  --identifier "com.wufeng.antigravity-wf-assistant" \
  --version "$VERSION" \
  --install-location "/" \
  "$PKG_DIR/wf-assistant.pkg"

pkgbuild \
  --nopayload \
  --scripts "$SCRIPT_DIR/scripts/desktop-shortcut" \
  --identifier "com.wufeng.antigravity-wf-assistant.desktop-shortcut" \
  --version "$VERSION" \
  --install-location "/" \
  "$PKG_DIR/wf-desktop-shortcut.pkg"

/usr/bin/sed "s/__VERSION__/$VERSION/g" "$SCRIPT_DIR/Distribution.xml" > "$WORK_DIR/Distribution.xml"
productbuild \
  --distribution "$WORK_DIR/Distribution.xml" \
  --package-path "$PKG_DIR" \
  "$OUTPUT_PATH"

echo "Created installer: $OUTPUT_PATH"
