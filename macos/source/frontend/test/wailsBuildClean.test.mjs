import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

function macOSJob(source) {
  const start = source.indexOf("\n  macos:\n");
  const end = source.indexOf("\n  windows:\n", start + 1);
  assert.notEqual(start, -1, "release workflow must have a macOS job");
  assert.notEqual(end, -1, "release workflow must separate macOS and Windows jobs");
  return source.slice(start, end);
}

function assertTauriBuildOrder(source, label, buildStepName, target) {
  const preflightIndex = source.indexOf("Run release preflight");
  const buildIndex = source.indexOf(buildStepName, preflightIndex + 1);
  assert.notEqual(preflightIndex, -1, `${label} must run the release preflight`);
  assert.notEqual(buildIndex, -1, `${label} must build the Tauri installer`);
  assert.ok(preflightIndex < buildIndex, `${label} must build only after preflight passes`);

  const preflightStep = source.slice(preflightIndex, buildIndex);
  assert.match(preflightStep, /npm run release:preflight/);
  const buildStep = source.slice(buildIndex);
  assert.match(buildStep, new RegExp(`npx tauri build[^\\n]+--target ${target}`));
}

test("macOS workflows rebuild and validate embedded assets before Tauri packaging", async () => {
  const [buildWorkflow, releaseWorkflow, tauriConfigSource, prepareSource, preflightSource] = await Promise.all([
    readFile(new URL("../../../../.github/workflows/build-macos.yml", import.meta.url), "utf8"),
    readFile(new URL("../../../../.github/workflows/release.yml", import.meta.url), "utf8"),
    readFile(new URL("../../../../nextgen/src-tauri/tauri.conf.json", import.meta.url), "utf8"),
    readFile(new URL("../../../../nextgen/scripts/prepare-wf-bridge-frontend.cjs", import.meta.url), "utf8"),
    readFile(new URL("../../../../nextgen/scripts/release/preflight.cjs", import.meta.url), "utf8"),
  ]);

  assertTauriBuildOrder(
    buildWorkflow,
    "build-macos workflow",
    "Build ad-hoc-signed Universal app and DMG",
    "universal-apple-darwin",
  );
  assertTauriBuildOrder(
    macOSJob(releaseWorkflow),
    "release workflow macOS job",
    "Build signed-updater Universal app and DMG",
    "universal-apple-darwin",
  );

  const tauriConfig = JSON.parse(tauriConfigSource);
  assert.match(tauriConfig.build.beforeBuildCommand, /prepare-wf-bridge-frontend\.cjs/);
  assert.match(prepareSource, /platform === 'darwin' \|\| platform === 'macos'/);
  assert.match(prepareSource, /run\(npmCommand, \['run', 'build'\]\)/);
  assert.match(prepareSource, /verifyDist\(\)/);
  assert.ok(
    preflightSource.indexOf("name: 'WF bridge frontend build'") <
      preflightSource.indexOf("name: 'WF bridge frontend regression tests'"),
    "embedded dependencies and assets must be prepared before regression tests run",
  );
});
