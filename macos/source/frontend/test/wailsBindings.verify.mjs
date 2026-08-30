import assert from "node:assert/strict";
import test from "node:test";
import { readFile } from "node:fs/promises";

const [runtimeSource, declarationSource, appStateSource] = await Promise.all([
  readFile(new URL("../wailsjs/go/main/App.js", import.meta.url), "utf8"),
  readFile(new URL("../wailsjs/go/main/App.d.ts", import.meta.url), "utf8"),
  readFile(new URL("../src/state/appState.js", import.meta.url), "utf8"),
]);

// Literal call("Method") calls are collected from the renderer's single
// native bridge module. This keeps the generated-binding gate coupled to the
// actual installable UI instead of relying on a manually maintained subset.
const literalBridgeMethods = [...appStateSource.matchAll(/\bcall\(\s*"([A-Z][A-Za-z0-9_]*)"/g)]
  .map((match) => match[1]);

// A small set of methods is selected through compatibility alias arrays or
// a deliberate runtime choice rather than a literal call(). Keep these names
// explicit: a missing one must make the post-Wails-build release gate fail,
// not turn into an install-time empty control.
const dynamicBridgeMethods = [
  "GetAgentStatuses",
  "RefreshAgentStatuses",
  "GetPatchStatus",
  "GetQuickPatchStatus",
  "RefreshPatchStatus",
  "GetCodexDesktopControlStatus",
  "SelectCodexDesktopInstallation",
  "LaunchCodexDesktop",
  "StopCodexDesktop",
  "RestartCodexDesktop",
  "ApplyCodexConfigurationWithLifecycle",
  "ApplyCodexXIASSSelectionWithLifecycle",
  "MigrateCodexLegacyProviderWithLifecycle",
  "GetCursorMCPConfiguration",
  "ApplyCursorMCPConfiguration",
  "RemoveCursorMCPConfiguration",
  "ListCursorMCPBackups",
  "RestoreCursorMCPBackup",
  "DeleteCursorMCPBackup",
  "GetWindsurfMCPConfiguration",
  "ApplyWindsurfMCPConfiguration",
  "RemoveWindsurfMCPConfiguration",
  "ListWindsurfMCPBackups",
  "RestoreWindsurfMCPBackup",
  "DeleteWindsurfMCPBackup",
];

const requiredMethods = [...new Set([...literalBridgeMethods, ...dynamicBridgeMethods])].sort();

test("Wails bindings include every XIASS Tools renderer bridge", () => {
  assert.ok(requiredMethods.length > 0, "renderer bridge method list must not be empty");
  for (const method of requiredMethods) {
    assert.match(runtimeSource, new RegExp(`export function ${method}\\(`), `${method} is missing from App.js`);
    assert.match(declarationSource, new RegExp(`export function ${method}\\(`), `${method} is missing from App.d.ts`);
  }
});
