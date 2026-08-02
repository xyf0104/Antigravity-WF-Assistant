package patcher

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsLanguageServerWithoutEmbeddedEndpointIsSupported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "language_server_windows_x64.exe")
	if err := os.WriteFile(path, []byte("MZ\x00new endpoint-free layout"), 0o755); err != nil {
		t.Fatal(err)
	}
	plan, embedded, err := prepareWindowsLanguagePatch(path)
	if err != nil {
		t.Fatal(err)
	}
	if embedded || plan == nil || plan.changed {
		t.Fatalf("endpoint-free binary should be an unchanged optional plan: embedded=%t plan=%#v", embedded, plan)
	}
	patched, hasEmbedded := windowsLanguagePatchState(path)
	if !patched || hasEmbedded {
		t.Fatalf("endpoint-free binary should rely on launcher: patched=%t embedded=%t", patched, hasEmbedded)
	}
}

func TestWindowsLanguageServerEmbeddedEndpointsArePatched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "language_server.exe")
	original := []byte("prefix " + windowsProductionEndpoint + " middle " + windowsSandboxEndpoint +
		" public " + windowsPublicEndpoint + " autopush " + windowsAutopushEndpoint + " suffix")
	if err := os.WriteFile(path, original, 0o755); err != nil {
		t.Fatal(err)
	}
	plan, embedded, err := prepareWindowsLanguagePatch(path)
	if err != nil {
		t.Fatal(err)
	}
	if !embedded || !plan.changed {
		t.Fatalf("expected embedded endpoints to be patched: embedded=%t changed=%t", embedded, plan.changed)
	}
	if windowsCloudCodeURLPattern.Match(plan.updated) ||
		!bytes.Contains(plan.updated, []byte(windowsBinaryProxyEndpoint)) {
		t.Fatalf("unexpected patched binary: %q", plan.updated)
	}
	if len(plan.updated) != len(original) {
		t.Fatalf("binary patch changed file length: %d != %d", len(plan.updated), len(original))
	}
}

func TestWindowsExtensionPatchHandlesNestedAndOptionalCloudCodeCalls(t *testing.T) {
	path := filepath.Join(t.TempDir(), "extension.js")
	source := "/*! For license information please see extension.js.LICENSE.txt */\n" +
		`const endpoint=await service.client?.getCloudCodeUrl();args.push("--cloud_code_endpoint",endpoint);` +
		`args.push("--app_data_dir",g.$.getInstance().appDataDirectoryName);` +
		`const fallback="` + windowsAutopushEndpoint + `";`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err := prepareWindowsExtensionPatch(path)
	if err != nil {
		t.Fatal(err)
	}
	updated := string(plan.updated)
	for _, required := range []string{windowsExtensionMarker, windowsBaseProxyEndpoint, windowsSharedDataArgument} {
		if !strings.Contains(updated, required) {
			t.Fatalf("patched extension is missing %q", required)
		}
	}
	if windowsFlexibleCloudCodeCallPattern.MatchString(updated) || windowsCloudCodeURLPattern.MatchString(updated) {
		t.Fatalf("patched extension retained a remote endpoint: %s", updated)
	}
	if !windowsLauncherHasProxyEndpoint(updated) {
		t.Fatal("patched extension launcher does not point to the local proxy")
	}
}

func TestWindowsMainPatchUsesLocalCredentialsAndSharedHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.js")
	source := `"use strict";const u=` + windowsIDECloudCodeSetting + `;const a=["--app_data_dir",x.ideName];` + authEligibilityOriginal
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err := prepareWindowsMainPatch(path)
	if err != nil {
		t.Fatal(err)
	}
	updated := string(plan.updated)
	for _, required := range []string{windowsMainMarker, windowsBaseProxyEndpoint, windowsSharedDataArgument, authEligibilityPatched} {
		if !strings.Contains(updated, required) {
			t.Fatalf("patched main is missing %q", required)
		}
	}
	for _, forbidden := range []string{windowsIDECloudCodeSetting, authEligibilityOriginal} {
		if strings.Contains(updated, forbidden) {
			t.Fatalf("patched main still contains %q", forbidden)
		}
	}
}

func TestWindowsASARLauncherPatchDoesNotRequireBinaryEndpoint(t *testing.T) {
	root := &asarNode{Files: map[string]*asarNode{}}
	fixture := &asarArchive{root: root}
	path := filepath.Join(t.TempDir(), "app.asar")
	if err := fixture.write(path, map[string][]byte{
		"package.json":           []byte(`{"version":"2.0.0"}`),
		"dist/main.js":           []byte(`"use strict";` + authEligibilityOriginal),
		"dist/languageServer.js": []byte(`const args=["--cloud_code_endpoint","` + windowsProductionEndpoint + `"]`),
	}); err != nil {
		t.Fatal(err)
	}
	candidate, err := prepareWindowsASARCandidate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(candidate)
	if !windowsASARPatched(candidate) {
		t.Fatal("candidate was not recognized as a complete Windows ASAR patch")
	}
	archive, err := readASAR(candidate)
	if err != nil {
		t.Fatal(err)
	}
	launcher, _ := archive.readFile("dist/languageServer.js")
	main, _ := archive.readFile("dist/main.js")
	if !bytes.Contains(launcher, []byte(windowsBaseProxyEndpoint)) || bytes.Contains(launcher, []byte(windowsProductionEndpoint)) {
		t.Fatalf("launcher endpoint was not patched: %s", launcher)
	}
	if !bytes.Contains(main, []byte(authEligibilityPatched)) || bytes.Contains(main, []byte(authEligibilityOriginal)) {
		t.Fatal("local credential eligibility branch was not patched")
	}
}
