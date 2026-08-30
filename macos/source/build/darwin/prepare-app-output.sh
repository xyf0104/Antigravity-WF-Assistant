#!/bin/bash
# Remove only the previous generated XIASS Tools app bundle before a Wails
# build. Wails v2 preserves sibling files inside an existing .app bundle, so
# relying on its -clean flag alone can leave stale test executables that must
# never reach an installer.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SOURCE_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
APP_PATH="${1:-$SOURCE_DIR/build/bin/XIASS Tools.app}"

if [[ ! -e "$APP_PATH" && ! -L "$APP_PATH" ]]; then
  exit 0
fi

if [[ ! -d "$APP_PATH" ]]; then
  echo "Refusing to remove a non-directory build target: $APP_PATH" >&2
  exit 1
fi

INFO_PLIST="$APP_PATH/Contents/Info.plist"
if [[ ! -f "$INFO_PLIST" ]]; then
  echo "Refusing to remove an unrecognised app bundle without Info.plist: $APP_PATH" >&2
  exit 1
fi

BUNDLE_ID="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$INFO_PLIST" 2>/dev/null || true)"
if [[ "$BUNDLE_ID" != "com.xiass.tools" ]]; then
  echo "Refusing to remove an unrecognised app bundle: $APP_PATH" >&2
  exit 1
fi

# The target is an explicitly verified, generated XIASS Tools bundle under the
# source build output. Do not broaden this deletion to build/bin, /Applications
# or any caller-supplied parent directory.
/bin/rm -rf "$APP_PATH"
echo "Removed previous generated XIASS Tools app bundle."
