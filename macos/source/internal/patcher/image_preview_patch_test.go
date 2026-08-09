package patcher

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func imagePreviewOriginalRendererFixture() string {
	return `prefix;a=e.generatedMedia,i;a?.uri?i=n?.(a.uri)||void 0:a?.payload.case==="inlineData"&&(i=a?YI(a):void 0);let s=Ia(t)&&!i;suffix`
}

func imagePreviewV2RendererFixture() string {
	return `prefix;/*antigravity-wf:image-preview-fallback:v2*/a=e.generatedMedia||e.generatedImage,i;a?.uri?(i=n?.(a.uri),i=(i&&typeof i.getState==="function"?i.getState():i||void 0)):a?.payload?.case==="inlineData"&&(i=a?YI(a):void 0),!i&&a?.base64Data&&(i="data:"+(a.mimeType||"image/png")+";base64,"+(typeof a.base64Data==="string"?a.base64Data:btoa(Array.from(a.base64Data).map(i=>String.fromCharCode(i)).join(""))));let s=Ia(t)&&!i;suffix`
}

// imagePreviewV3RendererFixture is the compact v3 shape observed in a real
// Antigravity 1.23.2 bundle. It deliberately retains the old file URI logic
// so migration tests prove installed users receive the drive-letter fix.
func imagePreviewV3RendererFixture() string {
	return `prefix;/*antigravity-wf:image-preview-fallback:v3*/a=e.generatedMedia||e.generatedImage,i;a?.uri?(i=n?.(a.uri),i=(i&&typeof i.getState==="function"?i.getState():i||void 0)):a?.payload?.case==="inlineData"&&(i=a?YI(a):void 0),!i&&a?.base64Data&&(i="data:"+(a.mimeType||"image/png")+";base64,"+(typeof a.base64Data==="string"?a.base64Data:btoa(Array.from(a.base64Data).map(i=>String.fromCharCode(i)).join("")))),!i&&a?.uri&&typeof a.uri==="string"&&a.uri.startsWith("file://")&&(i=decodeURIComponent(a.uri.replace(/^file:\/\//,"")));let s=Ia(t)&&!i;suffix`
}

func TestPatchImagePreviewRendererAddsV4Fallback(t *testing.T) {
	updated, result := patchImagePreviewRenderer(imagePreviewOriginalRendererFixture())
	if !result.Recognized || !result.Changed {
		t.Fatalf("unmodified 1.23.2 renderer was not patched: %#v", result)
	}
	for _, required := range []string{
		imagePreviewPatchMarker,
		`a=e.generatedMedia||e.generatedImage`,
		`typeof i.getState==="function"`,
		`.base64Data`,
		`startsWith("file://")`,
		`e.generatedImage&&e.generatedImage!==a`,
		`u.replace(/^file:\/\/\/([A-Za-z]:\/)/,"$1")`,
		`catch{return p}`,
	} {
		if !strings.Contains(updated, required) {
			t.Fatalf("v4 renderer is missing %q: %s", required, updated)
		}
	}
	second, secondResult := patchImagePreviewRenderer(updated)
	if !secondResult.Recognized || secondResult.Changed || second != updated {
		t.Fatalf("v4 renderer must be idempotent: result=%#v", secondResult)
	}
	assertImagePreviewJavaScriptSyntax(t, updated)
}

func TestPatchImagePreviewRendererUpgradesV2(t *testing.T) {
	updated, result := patchImagePreviewRenderer(imagePreviewV2RendererFixture())
	if !result.Recognized || !result.Changed {
		t.Fatalf("v2 renderer was not upgraded: %#v", result)
	}
	if strings.Contains(updated, imagePreviewPatchV2Marker) || !strings.Contains(updated, imagePreviewPatchMarker) || !strings.Contains(updated, `startsWith("file://")`) {
		t.Fatalf("v2 upgrade did not produce the full v4 fallback: %s", updated)
	}
	assertImagePreviewJavaScriptSyntax(t, updated)
}

func TestPatchImagePreviewRendererUpgradesRealV3(t *testing.T) {
	updated, result := patchImagePreviewRenderer(imagePreviewV3RendererFixture())
	if !result.Recognized || !result.Changed {
		t.Fatalf("real v3 renderer was not upgraded: %#v", result)
	}
	if strings.Contains(updated, imagePreviewPatchV3Marker) || !strings.Contains(updated, imagePreviewPatchMarker) || !strings.Contains(updated, `u.replace(/^file:\/\/\/([A-Za-z]:\/)/,"$1")`) {
		t.Fatalf("v3 upgrade did not produce the Windows-safe v4 fallback: %s", updated)
	}
	if _, result := patchImagePreviewRenderer(updated); !result.Recognized || result.Changed {
		t.Fatalf("migrated v4 renderer was not idempotent: %#v", result)
	}
	assertImagePreviewJavaScriptSyntax(t, updated)
}

func TestPatchImagePreviewRendererPatchesEveryKnownOccurrence(t *testing.T) {
	source := imagePreviewOriginalRendererFixture() + `between;` + imagePreviewOriginalRendererFixture()
	updated, result := patchImagePreviewRenderer(source)
	if !result.Recognized || !result.Changed || strings.Count(updated, imagePreviewPatchMarker) != 2 {
		t.Fatalf("all known renderer occurrences must be patched: result=%#v source=%s", result, updated)
	}
	if _, result := patchImagePreviewRenderer(updated); !result.Recognized || result.Changed {
		t.Fatalf("multi-renderer v4 source was not idempotent: %#v", result)
	}
}

// Set ANTIGRAVITY_WF_TEST_RENDERERS to a path-list of real renderer bundles
// to verify an installed Antigravity version without mutating it. CI leaves it
// unset; release validation can point it at a freshly installed target.
func TestPatchImagePreviewRendererUpgradesOptionalInstalledRenderers(t *testing.T) {
	paths := filepath.SplitList(strings.TrimSpace(os.Getenv("ANTIGRAVITY_WF_TEST_RENDERERS")))
	if len(paths) == 0 || paths[0] == "" {
		t.Skip("ANTIGRAVITY_WF_TEST_RENDERERS is not set")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable; installed-renderer syntax check skipped")
	}
	for _, rendererPath := range paths {
		rendererPath := rendererPath
		t.Run(filepath.Base(rendererPath), func(t *testing.T) {
			original, err := os.ReadFile(rendererPath)
			if err != nil {
				t.Fatal(err)
			}
			updated, result := patchImagePreviewRenderer(string(original))
			if !result.Recognized || !result.Changed || strings.Contains(updated, imagePreviewPatchV3Marker) || !strings.Contains(updated, imagePreviewPatchMarker) {
				t.Fatalf("installed renderer was not safely migrated: %#v", result)
			}
			candidate := filepath.Join(t.TempDir(), filepath.Base(rendererPath))
			if err := os.WriteFile(candidate, []byte(updated), 0o600); err != nil {
				t.Fatal(err)
			}
			if output, err := exec.Command(node, "--check", candidate).CombinedOutput(); err != nil {
				t.Fatalf("installed renderer migration failed node --check: %s: %v", output, err)
			}
		})
	}
}

func TestImagePreviewV4FallbackNormalizesWindowsAndMacFileURIs(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable; JavaScript fallback execution check skipped")
	}
	renderer := imagePreviewV4Renderer("a", "e", "i", "n", "YI")
	for _, test := range []struct {
		name string
		uri  string
		want string
	}{
		{name: "windows drive", uri: "file:///C:/Users/%E6%97%A0%E9%A3%8E/image.png", want: "C:/Users/无风/image.png"},
		{name: "mac absolute path", uri: "file:///Users/wufeng/image.png", want: "/Users/wufeng/image.png"},
		{name: "literal percent remains renderable", uri: "file:///C:/Users/100%/image.png", want: "C:/Users/100%/image.png"},
		{name: "windows UNC path", uri: "file://server/share/My%20Image.png", want: "//server/share/My Image.png"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "preview-uri.js")
			source := `"use strict";const e={generatedImage:{uri:` + strconv.Quote(test.uri) + `}};let a,i;const n=()=>undefined;const YI=()=>undefined;` + renderer + `;process.stdout.write(String(i));`
			if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
				t.Fatal(err)
			}
			if output, err := exec.Command(node, "--check", path).CombinedOutput(); err != nil {
				t.Fatalf("transformed renderer failed node --check: %s: %v", output, err)
			}
			output, err := exec.Command(node, path).Output()
			if err != nil {
				t.Fatal(err)
			}
			if got := string(output); got != test.want {
				t.Fatalf("normalized URI = %q, want %q", got, test.want)
			}
		})
	}
}

// TestPatchedImagePreviewRendererResolvesAllSupportedSourcesAtRuntime runs the
// exact renderer fragment produced by patchImagePreviewRenderer in Node. The
// structural tests above catch accidental changes to the emitted source; this
// test catches a more important class of regressions where syntactically valid
// fallback JavaScript still cannot provide the chat renderer with a usable
// image src for one of the media shapes emitted by different Antigravity
// versions.
func TestPatchedImagePreviewRendererResolvesAllSupportedSourcesAtRuntime(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable; patched renderer runtime check skipped")
	}

	patched, result := patchImagePreviewRenderer(imagePreviewOriginalRendererFixture())
	if !result.Recognized || !result.Changed {
		t.Fatalf("fixture renderer was not patched: %#v", result)
	}

	for _, test := range []struct {
		name     string
		step     string
		resolver string
		want     string
	}{
		{
			name:     "generatedImage file URI with Windows Chinese user directory",
			step:     `{generatedImage:{uri:"file:///C:/Users/%E6%97%A0%E9%A3%8E/image.png"}}`,
			resolver: `()=>undefined`,
			want:     "C:/Users/无风/image.png",
		},
		{
			name:     "generatedMedia payload inline data",
			step:     `{generatedMedia:{payload:{case:"inlineData",inlineData:{mimeType:"image/webp",data:"aGVsbG8="}}}}`,
			resolver: `()=>undefined`,
			want:     "data:image/webp;base64,aGVsbG8=",
		},
		{
			name:     "generatedMedia string base64 data",
			step:     `{generatedMedia:{mimeType:"image/jpeg",base64Data:"c3RyaW5nLWJ5dGVz"}}`,
			resolver: `()=>undefined`,
			want:     "data:image/jpeg;base64,c3RyaW5nLWJ5dGVz",
		},
		{
			name:     "generatedMedia byte array base64 data",
			step:     `{generatedMedia:{mimeType:"image/png",base64Data:[72,105]}}`,
			resolver: `()=>undefined`,
			want:     "data:image/png;base64,SGk=",
		},
		{
			name:     "artifact resolver Store getState",
			step:     `{generatedMedia:{uri:"artifact://conversation/image-1"}}`,
			resolver: `uri=>({getState:()=>"blob:preview-from-store:"+uri})`,
			want:     "blob:preview-from-store:artifact://conversation/image-1",
		},
		{
			name:     "generatedImage is tried after metadata-only generatedMedia",
			step:     `{generatedMedia:{mimeType:"image/png"},generatedImage:{uri:"file:///C:/Users/%E6%97%A0%E9%A3%8E/image.png"}}`,
			resolver: `()=>undefined`,
			want:     "C:/Users/无风/image.png",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "patched-preview-runtime.js")
			// Keep the surrounding declarations deliberately close to the known
			// minified renderer: patchImagePreviewRenderer changes only the
			// expression between prefix and let s, so this executes the patched
			// source rather than a separately reconstructed fallback.
			source := `"use strict";const e=` + test.step + `;let a,i;const n=` + test.resolver + `;const YI=media=>{const data=media?.payload?.inlineData;return data?"data:"+(data.mimeType||"image/png")+";base64,"+data.data:void 0};const Ia=()=>false;const t={};const prefix=0,suffix=0;` + patched + `;process.stdout.write(JSON.stringify(i));`
			if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
				t.Fatal(err)
			}
			if output, err := exec.Command(node, "--check", path).CombinedOutput(); err != nil {
				t.Fatalf("patched renderer failed node --check: %s: %v", output, err)
			}
			output, err := exec.Command(node, path).CombinedOutput()
			if err != nil {
				t.Fatalf("patched renderer failed at runtime: %s: %v", output, err)
			}
			var got string
			if err := json.Unmarshal(output, &got); err != nil {
				t.Fatalf("patched renderer did not return a JSON image src %q: %v", output, err)
			}
			if got != test.want {
				t.Fatalf("image src = %q, want %q", got, test.want)
			}
		})
	}
}

func TestPatchImagePreviewRendererSkipsUnknownRenderer(t *testing.T) {
	original := `const futureRenderer={generatedMedia:"different-shape"};`
	updated, result := patchImagePreviewRenderer(original)
	if result.Recognized || result.Changed || updated != original {
		t.Fatalf("unknown renderer must remain untouched: result=%#v updated=%q", result, updated)
	}
}

func TestImagePreviewRendererPathsOnlyIncludeKnownExistingBundles(t *testing.T) {
	root := t.TempDir()
	for _, relative := range imagePreviewRendererRelativePaths[:2] {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(imagePreviewOriginalRendererFixture()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "out", "unrelated.js"), []byte("ignored"), 0o644); err != nil {
		t.Fatal(err)
	}
	paths := imagePreviewRendererPaths(root)
	if len(paths) != 2 {
		t.Fatalf("unexpected renderer paths: %v", paths)
	}
}

func TestImagePreviewRenderersNeedPatchOnlyForKnownOutdatedBundles(t *testing.T) {
	root := t.TempDir()
	unknown := filepath.Join(root, "future-renderer.js")
	known := filepath.Join(root, "known-renderer.js")
	missing := filepath.Join(root, "missing-renderer.js")
	if err := os.WriteFile(unknown, []byte(`const futureRenderer={generatedMedia:"different-shape"};`), 0o644); err != nil {
		t.Fatal(err)
	}
	if imagePreviewRenderersNeedPatch([]string{missing, unknown}) {
		t.Fatal("missing or unknown renderers must not make the target pending")
	}
	if err := os.WriteFile(known, []byte(imagePreviewOriginalRendererFixture()), 0o644); err != nil {
		t.Fatal(err)
	}
	if !imagePreviewRenderersNeedPatch([]string{missing, unknown, known}) {
		t.Fatal("recognized unpatched 1.23.2 renderer must make the target pending")
	}
	updated, result := patchImagePreviewRenderer(imagePreviewOriginalRendererFixture())
	if !result.Changed {
		t.Fatal("fixture should produce a v4 renderer")
	}
	if err := os.WriteFile(known, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	if imagePreviewRenderersNeedPatch([]string{missing, unknown, known}) {
		t.Fatal("v4, missing, and unknown renderers must not make the target pending")
	}
}

func TestDarwinTargetPatchStateRequiresKnownPendingPreviewFallback(t *testing.T) {
	app := filepath.Join(t.TempDir(), "Antigravity.app")
	main := filepath.Join(app, "Contents", "Resources", "app", "out", "main.js")
	language := filepath.Join(app, "Contents", "Resources", "app", "extensions", "antigravity", "bin", "language_server_macos_x64")
	for _, path := range []string{main, language} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	endpointPatched := textProxyEndpoint + "\n" + authEligibilityPatched + "\n"
	if err := os.WriteFile(main, []byte(endpointPatched+imagePreviewOriginalRendererFixture()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(language, []byte(binaryProxyEndpoint), 0o755); err != nil {
		t.Fatal(err)
	}
	target := darwinTargets{app: app, kind: "ide", main: main, language: language}
	mainPatched, extensionPatched, languagePatched, fullyPatched := darwinTargetPatchState(target)
	if !mainPatched || !extensionPatched || !languagePatched || fullyPatched {
		t.Fatalf("known pending preview fallback must keep an otherwise patched target pending: main=%t extension=%t language=%t fully=%t", mainPatched, extensionPatched, languagePatched, fullyPatched)
	}
	status := buildDarwinStatus([]darwinTargets{target})
	if status.IDEPatched == nil || *status.IDEPatched || len(status.Targets) != 1 || status.Targets[0].Patched {
		t.Fatalf("pending preview fallback was not reflected in status: %+v", status)
	}

	updated, result := patchImagePreviewRenderer(endpointPatched + imagePreviewOriginalRendererFixture())
	if !result.Changed {
		t.Fatal("fixture should produce the current v4 renderer")
	}
	if err := os.WriteFile(main, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, fullyPatched = darwinTargetPatchState(target); !fullyPatched {
		t.Fatal("current v4 renderer should restore the complete target status")
	}

	if err := os.WriteFile(main, []byte(endpointPatched+`const futureRenderer={generatedMedia:"different-shape"};`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, fullyPatched = darwinTargetPatchState(target); !fullyPatched {
		t.Fatal("unknown or absent optional renderers must not block normal endpoint status")
	}
}

func TestDarwinAgentTargetPatchStateRequiresKnownPendingPreviewFallback(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "app.asar")
	language := filepath.Join(root, "language_server_macos_x64")
	fixture := &asarArchive{root: &asarNode{Files: map[string]*asarNode{}}}
	if err := fixture.write(path, map[string][]byte{
		"dist/main.js":            []byte(`"use strict";` + "\n// " + darwinASARMarker),
		"dist/languageServer.js":  []byte(`const args=["--cloud_code_endpoint","` + baseProxyEndpoint + `"];`),
		"out/jetskiAgent/main.js": []byte(imagePreviewOriginalRendererFixture()),
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(language, []byte(binaryProxyEndpoint), 0o755); err != nil {
		t.Fatal(err)
	}
	target := darwinTargets{app: root, kind: "agent", asar: path, language: language}
	mainPatched, extensionPatched, languagePatched, fullyPatched := darwinTargetPatchState(target)
	if !mainPatched || !extensionPatched || !languagePatched || fullyPatched {
		t.Fatalf("known pending Agent renderer must keep an otherwise patched target pending: main=%t extension=%t language=%t fully=%t", mainPatched, extensionPatched, languagePatched, fullyPatched)
	}
	status := buildDarwinStatus([]darwinTargets{target})
	if status.AgentPatched == nil || *status.AgentPatched || len(status.Targets) != 1 || status.Targets[0].Patched {
		t.Fatalf("pending Agent preview fallback was not reflected in macOS status: %+v", status)
	}

	archive, err := readASAR(path)
	if err != nil {
		t.Fatal(err)
	}
	replacements := map[string][]byte{}
	if !patchImagePreviewASARRenderers(archive, replacements) {
		t.Fatal("fixture ASAR should require the current renderer fallback")
	}
	patchedPath := filepath.Join(filepath.Dir(path), "patched.app.asar")
	if err := archive.write(patchedPath, replacements); err != nil {
		t.Fatal(err)
	}
	target.asar = patchedPath
	if _, _, _, fullyPatched = darwinTargetPatchState(target); !fullyPatched {
		t.Fatal("current Agent renderer should restore the complete target status")
	}
}

func TestPatchImagePreviewASARRenderersUpdatesKnownEntriesOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.asar")
	fixture := &asarArchive{root: &asarNode{Files: map[string]*asarNode{}}}
	if err := fixture.write(path, map[string][]byte{
		"dist/main.js":                               []byte(imagePreviewOriginalRendererFixture()),
		"out/jetskiAgent/main.js":                    []byte(imagePreviewOriginalRendererFixture()),
		"out/vs/workbench/workbench.desktop.main.js": []byte(`const unrelated=true;`),
		"dist/unrelated.js":                          []byte(imagePreviewOriginalRendererFixture()),
	}); err != nil {
		t.Fatal(err)
	}
	archive, err := readASAR(path)
	if err != nil {
		t.Fatal(err)
	}
	replacements := map[string][]byte{}
	if !patchImagePreviewASARRenderers(archive, replacements) {
		t.Fatal("known ASAR renderer entries were not patched")
	}
	if len(replacements) != 2 {
		t.Fatalf("only known matching renderer entries should change: %v", replacements)
	}
	for name, data := range replacements {
		if !strings.Contains(string(data), imagePreviewPatchMarker) {
			t.Fatalf("%s did not receive the v4 preview fallback", name)
		}
	}
}

func assertImagePreviewJavaScriptSyntax(t *testing.T, renderer string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable; JavaScript syntax check skipped")
	}
	path := filepath.Join(t.TempDir(), "preview-renderer.js")
	// The wrapper supplies the minified identifiers used by the known renderer
	// shape while leaving the transformed source otherwise unchanged.
	source := `"use strict";const e={generatedMedia:{uri:"file:///Users/test.png"}};const n=()=>undefined;const YI=()=>undefined;let a,i;` + renderer
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(node, "--check", path).CombinedOutput(); err != nil {
		t.Fatalf("transformed renderer failed node --check: %s: %v", output, err)
	}
}
