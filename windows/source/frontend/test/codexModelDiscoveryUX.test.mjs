import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const modalSource = await readFile(new URL("../src/components/CodexConfigurationModal.vue", import.meta.url), "utf8");

test("Codex model discovery is described as catalog discovery, not Responses inference validation", () => {
  assert.match(modalSource, /已发现 \$\{models\.value\.length\} 个模型 ID（尚未验证 Responses 推理）。/);
  assert.match(modalSource, /只发现模型 ID，不会自动调用或验证 Responses 推理/);
  assert.doesNotMatch(modalSource, /已获取 \$\{models\.value\.length\} 个可用模型。/);
});
