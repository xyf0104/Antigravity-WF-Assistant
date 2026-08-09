//go:build darwin

package patcher

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDarwinApplicationsInRootRecognizesCommonNamesAcrossCaseStyles(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{
		"Google ANTIGRAVITY 2.0.APP",
		"Agent Window.app",
		"Firefox.app",
	} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	want := []string{
		filepath.Join(root, "Agent Window.app"),
		filepath.Join(root, "Google ANTIGRAVITY 2.0.APP"),
	}
	if got := darwinApplicationsInRoot(root); !reflect.DeepEqual(got, want) {
		t.Fatalf("one-level application discovery = %#v, want %#v", got, want)
	}
}

func TestDarwinRunningAppPatternKeepsTheBundleNameBoundary(t *testing.T) {
	if matches := runningAppPattern.FindAllStringSubmatch(
		"/tmp/Antigravity backups/Other.app/Contents/MacOS/Electron", -1,
	); len(matches) != 0 {
		t.Fatalf("parent directory name unexpectedly made another app a candidate: %#v", matches)
	}
	matches := runningAppPattern.FindAllStringSubmatch(
		"/Applications/Google Antigravity 2.0.app/Contents/MacOS/Electron", -1,
	)
	if len(matches) != 1 || len(matches[0]) < 2 || matches[0][1] != "/Applications/Google Antigravity 2.0.app" {
		t.Fatalf("valid running app was not discovered: %#v", matches)
	}
}

func TestSelectDarwinInstallationsDoesNotTreatRuntimePathAsExplicit(t *testing.T) {
	root := t.TempDir()
	trusted := filepath.Join(root, "Antigravity IDE.app")
	custom := filepath.Join(root, "Antigravity copy.app")
	writeDarwinUnpackedDiscoveryFixture(t, trusted, "")
	writeDarwinUnpackedDiscoveryFixture(t, custom, "com.google.antigravity")

	trustedPath := normalizeAppBundlePath(trusted)
	trustOnlyTrusted := func(path string) bool { return path == trustedPath }
	targets := selectDarwinInstallations([]string{custom, trusted}, nil, trustOnlyTrusted)
	if len(targets) != 1 || targets[0].app != trustedPath {
		t.Fatalf("a standard install should win over a process/Spotlight custom copy: %+v", targets)
	}

	// A valid active custom installation remains discoverable when it is the
	// only installation.  It is accepted because its bundle identifier, not
	// merely its filename, confirms the product identity.
	targets = selectDarwinInstallations([]string{custom}, nil, func(string) bool { return false })
	if len(targets) != 1 || targets[0].app != normalizeAppBundlePath(custom) {
		t.Fatalf("sole valid custom install was not discovered: %+v", targets)
	}
}

func TestSelectDarwinInstallationsHonorsExplicitRecoveryPath(t *testing.T) {
	app := filepath.Join(t.TempDir(), "Renamed Recovery Build.app")
	writeDarwinUnpackedDiscoveryFixture(t, app, "")
	normalized := normalizeAppBundlePath(app)
	targets := selectDarwinInstallations(
		[]string{app},
		map[string]bool{normalized: true},
		func(string) bool { return false },
	)
	if len(targets) != 1 || targets[0].app != normalized {
		t.Fatalf("explicit recovery path was not honored: %+v", targets)
	}
}

func TestInspectDarwinUnpackedIDEFindsLanguageServerWithoutExtensionEntry(t *testing.T) {
	app := filepath.Join(t.TempDir(), "Antigravity IDE.app")
	main := filepath.Join(app, "Contents", "Resources", "app", "out", "main.js")
	language := filepath.Join(app, "Contents", "Resources", "app", "extensions", "antigravity", "bin", "language_server_macos_x64")
	for _, path := range []string{main, language} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(main, []byte(`"use strict";const endpoint="`+productionEndpoint+`";`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(language, []byte("MZ\x00"+sandboxEndpoint), 0o755); err != nil {
		t.Fatal(err)
	}

	target, ok := inspectDarwinApp(app)
	if !ok || target.kind != "ide" || target.language != language || target.extensionEntry != "" {
		t.Fatalf("IDE with standalone language server was not identified safely: %+v, ok=%t", target, ok)
	}
}

func TestNormalizeAppBundlePathHandlesQuotedProcessPath(t *testing.T) {
	app := filepath.Join(t.TempDir(), "Antigravity IDE.app")
	input := `"` + filepath.Join(app, "Contents", "MacOS", "Electron") + `"`
	if got, want := normalizeAppBundlePath(input), filepath.Clean(app); got != want {
		t.Fatalf("quoted app process path = %q, want %q", got, want)
	}
}

func writeDarwinUnpackedDiscoveryFixture(t *testing.T, app, bundleIdentifier string) {
	t.Helper()
	main := filepath.Join(app, "Contents", "Resources", "app", "out", "main.js")
	extension := filepath.Join(app, "Contents", "Resources", "app", "extensions", "antigravity", "dist", "extension.js")
	for _, path := range []string{main, extension} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(main, []byte(`"use strict";const endpoint="`+productionEndpoint+`";`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(extension, []byte(`const endpoint=await service.getCloudCodeUrl();`), 0o644); err != nil {
		t.Fatal(err)
	}
	if bundleIdentifier == "" {
		return
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>CFBundleIdentifier</key><string>` + bundleIdentifier + `</string></dict></plist>`
	path := filepath.Join(app, "Contents", "Info.plist")
	if err := os.WriteFile(path, []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
}
