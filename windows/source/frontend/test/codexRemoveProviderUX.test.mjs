import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const modalSource = await readFile(new URL("../src/components/CodexConfigurationModal.vue", import.meta.url), "utf8");
const appStateSource = await readFile(new URL("../src/state/appState.js", import.meta.url), "utf8");

function sourceSlice(source, startText, endText) {
  const start = source.indexOf(startText);
  assert.notEqual(start, -1, `missing ${startText}`);
  const end = source.indexOf(endText, start);
  assert.notEqual(end, -1, `missing ${endText}`);
  return source.slice(start, end);
}

test("Codex disconnect bridge is parameterless and fixed to the native XIASS provider action", () => {
  const bridge = sourceSlice(appStateSource, "export async function removeCodexXIASSProvider", "export async function discoverCodexModels");
  assert.match(bridge, /removeCodexXIASSProvider\(\)/);
  assert.match(bridge, /call\("RemoveCodexXIASSProvider"\)/);
  assert.doesNotMatch(bridge, /providerID|provider_id|path|localStorage|console\.(?:log|error|warn)/i);
});

test("Codex disconnect UI is opt-in, scoped, and does not trigger Desktop lifecycle controls", () => {
  const action = sourceSlice(modalSource, "async function removeXIASSProvider", "function confirmRemoveXIASSProvider");
  assert.match(action, /await removeCodexXIASSProvider\(\)/);
  assert.match(action, /cancelXIASSKeySelection\(\)/);
  assert.match(action, /draft\.value = \{ \.\.\.next, api_key: "" \}/);
  assert.match(action, /restartRequired\.value = wasActive/);
  assert.doesNotMatch(action, /getCodexDesktop|selectCodexDesktop|launchCodexDesktop|stopCodexDesktop|restartCodexDesktop|repairCodexHistory/);
  assert.doesNotMatch(action, /result\?\.message|result\.message|console\.(?:log|error|warn)/);

  const confirmation = sourceSlice(modalSource, "function confirmRemoveXIASSProvider", "function confirmLifecycleApply");
  assert.match(confirmation, /window\.confirm/);
  assert.match(confirmation, /xiass_tools/);
  assert.match(confirmation, /不会退出、重启或打开 Codex/);

  const start = modalSource.indexOf('<section v-if="canRemoveXIASSProvider"');
  assert.notEqual(start, -1, "remove provider section is missing");
  const end = modalSource.indexOf("</section>", start);
  const template = modalSource.slice(start, end + "</section>".length);
  assert.match(template, /仅移除 <code>xiass_tools<\/code>/);
  assert.match(template, /其他 Provider、MCP、Desktop、未知设置或历史会话/);
  assert.match(template, /移除 XIASS Tools Provider/);
  assert.doesNotMatch(template, /api_key|experimental_bearer_token|auth\.json|snapshot\.location/);
});

test("Codex disconnect UI is readable in theme variants and blocks competing modal actions", () => {
  assert.match(modalSource, /removingXIASSProvider/);
  assert.match(modalSource, /:closable="!saving && !removingXIASSProvider/);
  assert.match(modalSource, /\.remove-provider-actions \{[^}]*var\(--red\)[^}]*var\(--bg-inset\)/);
  assert.match(modalSource, /\.remove-provider-actions p \{[^}]*var\(--text-secondary\)/);
  assert.match(modalSource, /\.remove-provider-actions :deep\(\.btn:focus-visible\)/);
  assert.match(modalSource, /@media \(max-width: 620px\)/);
});
