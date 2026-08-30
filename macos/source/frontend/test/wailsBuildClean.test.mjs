import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const nativeMacBuild = /wails@v2\.13\.0 build -clean -platform darwin\/universal/;
const staleBundlePreparation = /bash \.\/build\/darwin\/prepare-app-output\.sh/;

test("macOS workflows clean generated output before packaging an installer", async () => {
  const [buildWorkflow, releaseWorkflow] = await Promise.all([
    readFile(new URL("../../../../.github/workflows/build-macos.yml", import.meta.url), "utf8"),
    readFile(new URL("../../../../.github/workflows/release.yml", import.meta.url), "utf8"),
  ]);

  assert.match(buildWorkflow, nativeMacBuild, "macOS build workflow must clean build/bin before Wails packaging");
  assert.match(buildWorkflow, staleBundlePreparation, "macOS build workflow must remove only the verified stale app bundle before Wails packaging");
  const macOSJob = releaseWorkflow.slice(0, releaseWorkflow.indexOf("\n  windows:"));
  assert.match(macOSJob, nativeMacBuild, "release workflow macOS job must clean build/bin before Wails packaging");
  assert.match(macOSJob, staleBundlePreparation, "release workflow macOS job must remove only the verified stale app bundle before Wails packaging");
});
