package darwinbuild

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareAppOutputIsNarrowAndVerifiesBundleIdentity(t *testing.T) {
	script, err := os.ReadFile("prepare-app-output.sh")
	if err != nil {
		t.Fatalf("read prepare script: %v", err)
	}
	contents := string(script)
	for _, required := range []string{
		`APP_PATH="${1:-$SOURCE_DIR/build/bin/XIASS Tools.app}"`,
		`CFBundleIdentifier`,
		`com.xiass.tools`,
		`/bin/rm -rf "$APP_PATH"`,
		`Refusing to remove an unrecognised app bundle`,
	} {
		if !strings.Contains(contents, required) {
			t.Fatalf("prepare script missing %q", required)
		}
	}
	for _, forbidden := range []string{
		`rm -rf "$SOURCE_DIR/build/bin"`,
		`rm -rf /Applications`,
	} {
		if strings.Contains(contents, forbidden) {
			t.Fatalf("prepare script must not contain broad deletion %q", forbidden)
		}
	}
}

func TestPrepareAppOutputScriptLocationIsStable(t *testing.T) {
	path, err := filepath.Abs("prepare-app-output.sh")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(filepath.ToSlash(path), "/macos/source/build/darwin/prepare-app-output.sh") {
		t.Fatalf("unexpected prepare script location: %s", path)
	}
}
