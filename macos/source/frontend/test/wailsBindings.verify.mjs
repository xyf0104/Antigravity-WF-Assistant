import assert from "node:assert/strict";
import test from "node:test";
import { readFile } from "node:fs/promises";

const [runtimeSource, declarationSource] = await Promise.all([
  readFile(new URL("../wailsjs/go/main/App.js", import.meta.url), "utf8"),
  readFile(new URL("../wailsjs/go/main/App.d.ts", import.meta.url), "utf8"),
]);

const requiredMethods = [
  "GetCodexConfiguration",
  "ApplyCodexConfiguration",
  "RestoreCodexConfiguration",
  "DeleteCodexConfigurationBackup",
  "GetCodexDesktopControlStatus",
  "SelectCodexDesktopInstallation",
  "LaunchCodexDesktop",
  "StopCodexDesktop",
  "RestartCodexDesktop",
  "ApplyCodexConfigurationWithLifecycle",
  "ApplyCodexXIASSSelectionWithLifecycle",
  "GetClaudeCodeConfiguration",
  "ApplyClaudeCodeConfiguration",
  "RestoreClaudeCodeConfiguration",
  "DeleteClaudeCodeConfigurationBackup",
  "MigrateClaudeCodeLegacyBackup",
  "GetCursorMCPConfiguration",
  "ApplyCursorMCPConfiguration",
  "ListCursorMCPBackups",
  "RestoreCursorMCPBackup",
  "DeleteCursorMCPBackup",
  "GetWindsurfMCPConfiguration",
  "ApplyWindsurfMCPConfiguration",
  "ListWindsurfMCPBackups",
  "RestoreWindsurfMCPBackup",
  "DeleteWindsurfMCPBackup",
  "GetTOTPEntries",
  "AddTOTPEntry",
  "GenerateTOTPCode",
  "DeleteTOTPEntry",
  "ExportTOTPEncrypted",
];

test("Wails bindings include every public XIASS Tools configuration bridge", () => {
  for (const method of requiredMethods) {
    assert.match(runtimeSource, new RegExp(`export function ${method}\\(`), `${method} is missing from App.js`);
    assert.match(declarationSource, new RegExp(`export function ${method}\\(`), `${method} is missing from App.d.ts`);
  }
});
