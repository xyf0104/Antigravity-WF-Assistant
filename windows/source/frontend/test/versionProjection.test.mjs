import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const sourceRoot = fileURLToPath(new URL("../..", import.meta.url));
const repositoryRoot = path.resolve(sourceRoot, "..", "..");
const platform = path.basename(path.dirname(sourceRoot));
const peerPlatform = platform === "macos" ? "windows" : "macos";

function escapeRegExp(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

async function readProjection(root) {
  const [version, packageSource, lockSource, wailsSource, updaterSource, appSource] = await Promise.all([
    readFile(path.join(root, "VERSION"), "utf8"),
    readFile(path.join(root, "frontend", "package.json"), "utf8"),
    readFile(path.join(root, "frontend", "package-lock.json"), "utf8"),
    readFile(path.join(root, "wails.json"), "utf8"),
    readFile(path.join(root, "internal", "updater", "updater.go"), "utf8"),
    readFile(path.join(root, "frontend", "src", "App.vue"), "utf8"),
  ]);

  return {
    version: version.trim(),
    packageMeta: JSON.parse(packageSource),
    lockMeta: JSON.parse(lockSource),
    wailsMeta: JSON.parse(wailsSource),
    updaterSource,
    appSource,
  };
}

function assertProjection(label, projection) {
  assert.match(projection.version, /^\d+\.\d+\.\d+$/, `${label} VERSION must be semantic`);
  assert.equal(projection.packageMeta.version, projection.version, `${label} frontend package version`);
  assert.equal(projection.lockMeta.version, projection.version, `${label} frontend lockfile version`);
  assert.equal(
    projection.lockMeta.packages?.[""]?.version,
    projection.version,
    `${label} frontend lockfile root version`,
  );
  assert.equal(projection.wailsMeta.info?.productVersion, projection.version, `${label} Wails version`);
  assert.match(
    projection.updaterSource,
    new RegExp(`CurrentVersion\\s*=\\s*"${escapeRegExp(projection.version)}"`),
    `${label} standalone updater version`,
  );
  assert.match(projection.appSource, /const embeddedVersionPillLabel = "由宿主更新";/);
  assert.match(projection.appSource, /版本与更新由宿主 XIASS Tools 管理/);
  assert.match(
    projection.appSource,
    new RegExp(`embeddedMode \\? embeddedVersionPillLabel : "v${escapeRegExp(projection.version)}"`),
    `${label} standalone sidebar version`,
  );
  assert.doesNotMatch(projection.appSource, /v1\.6\.8/, `${label} must not leak the retired helper version`);
}

test("embedded helper version metadata remains mirrored and host-owned updates stay explicit", async () => {
  const [current, peer] = await Promise.all([
    readProjection(sourceRoot),
    readProjection(path.join(repositoryRoot, peerPlatform, "source")),
  ]);

  assertProjection(platform, current);
  assertProjection(peerPlatform, peer);
  assert.equal(current.version, peer.version, "macOS and Windows helper versions must stay aligned");
});
