import assert from "node:assert/strict";
import test from "node:test";
import { readFile } from "node:fs/promises";

const preNativeSuite = "Run pre-native-build frontend regression suite";
const nativeBuild = "Build Windows x64";
const generatedBindingGate = "Verify generated Wails bindings";

function windowsJob(source) {
  // GitHub's Windows checkout normally uses CRLF while local macOS checks use
  // LF. The workflow is data, not a platform-specific contract, so the gate
  // must inspect both byte representations instead of reporting a false
  // missing-job failure on Windows.
  const match = /\r?\n  windows:\r?\n/.exec(source);
  assert.ok(match, "release workflow must have a Windows job");
  return source.slice(match.index);
}

function assertBindingGateOrder(source, label) {
  const preNativeIndex = source.indexOf(preNativeSuite);
  const nativeBuildIndex = source.indexOf(nativeBuild, preNativeIndex + 1);
  const bindingGateIndex = source.indexOf(generatedBindingGate, nativeBuildIndex + 1);

  assert.notEqual(preNativeIndex, -1, `${label} must run the pre-native frontend suite`);
  assert.notEqual(nativeBuildIndex, -1, `${label} must build the native Windows app`);
  assert.notEqual(bindingGateIndex, -1, `${label} must verify generated Wails bindings`);
  assert.ok(preNativeIndex < nativeBuildIndex, `${label} must build only after pre-native frontend tests pass`);
  assert.ok(nativeBuildIndex < bindingGateIndex, `${label} must verify bindings after the native Wails build`);

  const preNativeStep = source.slice(preNativeIndex, nativeBuildIndex);
  const nativeBuildStep = source.slice(nativeBuildIndex, bindingGateIndex);
  const bindingGateStep = source.slice(bindingGateIndex);
  assert.match(preNativeStep, /Get-ChildItem -Path test -Filter '\*\.test\.mjs' -File/, `${label} must enumerate only source-level regression tests before native generation`);
  assert.doesNotMatch(preNativeStep, /wailsBindings\.verify\.mjs/, `${label} must not rely on generated bindings before native generation`);
  assert.match(nativeBuildStep, /wails@v2\.13\.0 build -clean -platform windows\/amd64/, `${label} must clean generated Windows output before creating a release artifact`);
  assert.match(bindingGateStep, /node --test test\/wailsBindings\.verify\.mjs/, `${label} must run the real generated-binding verifier after native generation`);
}

test("Windows workflows keep the real Wails binding gate after native generation", async () => {
  const [buildWorkflow, releaseWorkflow] = await Promise.all([
    readFile(new URL("../../../../.github/workflows/build-windows.yml", import.meta.url), "utf8"),
    readFile(new URL("../../../../.github/workflows/release.yml", import.meta.url), "utf8"),
  ]);

  assertBindingGateOrder(buildWorkflow, "build-windows workflow");
  assertBindingGateOrder(windowsJob(releaseWorkflow), "release workflow Windows job");
  const crlfReleaseWorkflow = releaseWorkflow
    .replaceAll("\r\n", "\n")
    .replaceAll("\n", "\r\n");
  assertBindingGateOrder(
    windowsJob(crlfReleaseWorkflow),
    "release workflow Windows job with CRLF",
  );
});
