import assert from "node:assert/strict";
import test from "node:test";
import { readFile } from "node:fs/promises";

function windowsJob(source) {
  const match = /\r?\n  windows:\r?\n/.exec(source);
  assert.ok(match, "release workflow must have a Windows job");
  return source.slice(match.index);
}

function assertTauriBuildOrder(source, label, buildStepName) {
  const preflightIndex = source.indexOf("Run release preflight");
  const buildIndex = source.indexOf(buildStepName, preflightIndex + 1);
  assert.notEqual(preflightIndex, -1, `${label} must run the release preflight`);
  assert.notEqual(buildIndex, -1, `${label} must build the Tauri installer`);
  assert.ok(preflightIndex < buildIndex, `${label} must build only after preflight passes`);

  const preflightStep = source.slice(preflightIndex, buildIndex);
  assert.match(preflightStep, /npm run release:preflight/);
  assert.match(preflightStep, /\$LASTEXITCODE/);
  const buildStep = source.slice(buildIndex);
  assert.match(buildStep, /npx tauri build[^\r\n]+--target x86_64-pc-windows-msvc/);
}

test("Windows workflows rebuild embedded assets before verified Tauri packaging", async () => {
  const [buildWorkflow, releaseWorkflow, tauriConfigSource, prepareSource, preflightSource] = await Promise.all([
    readFile(new URL("../../../../.github/workflows/build-windows.yml", import.meta.url), "utf8"),
    readFile(new URL("../../../../.github/workflows/release.yml", import.meta.url), "utf8"),
    readFile(new URL("../../../../nextgen/src-tauri/tauri.conf.json", import.meta.url), "utf8"),
    readFile(new URL("../../../../nextgen/scripts/prepare-wf-bridge-frontend.cjs", import.meta.url), "utf8"),
    readFile(new URL("../../../../nextgen/scripts/release/preflight.cjs", import.meta.url), "utf8"),
  ]);

  assertTauriBuildOrder(
    buildWorkflow,
    "build-windows workflow",
    "Build unsigned Windows x64 installers",
  );
  assertTauriBuildOrder(
    windowsJob(releaseWorkflow),
    "release workflow Windows job",
    "Build signed-updater Windows x64 installers",
  );

  const tauriConfig = JSON.parse(tauriConfigSource);
  assert.match(tauriConfig.build.beforeBuildCommand, /prepare-wf-bridge-frontend\.cjs/);
  assert.match(prepareSource, /platform === 'win32' \|\| platform === 'windows'/);
  assert.match(prepareSource, /shell: process\.platform === 'win32'/);
  assert.match(prepareSource, /run\(npmCommand, \['run', 'build'\]\)/);
  assert.match(prepareSource, /verifyDist\(\)/);
  assert.ok(
    preflightSource.indexOf("name: 'WF bridge frontend build'") <
      preflightSource.indexOf("name: 'WF bridge frontend regression tests'"),
    "embedded dependencies and assets must be prepared before regression tests run",
  );
});
