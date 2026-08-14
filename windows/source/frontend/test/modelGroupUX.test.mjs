import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const source = await readFile(new URL("../src/views/Models.vue", import.meta.url), "utf8");

test("upstream cards default collapsed and expose the restored group controls", () => {
  assert.match(source, /const expandedGroupKeys = ref\(new Set\(\)\)/);
  assert.match(source, />测试连接<\/Button>/);
  assert.match(source, />编辑代理商<\/Button>/);
  assert.match(source, /'收起模型' : '展开模型'/);
  assert.match(source, /v-if="isGroupExpanded\(group\)" class="group-model-list"/);
});

test("agent editing preserves model identity while updating shared route fields", () => {
  assert.match(source, /function modelWithUpdatedUpstream\(model, config\)/);
  assert.match(source, /upstreamName: String\(config\.upstreamName \|\| ""\)\.trim\(\)/);
  assert.match(source, /capabilities: automaticCapabilities\(config\.provider, model\.externalModelName/);
  assert.match(source, /保存代理商信息/);
});

test("single model rows only expose enablement and model-specific reasoning", () => {
	assert.match(source, />编辑推理<\/Button>/);
	assert.doesNotMatch(source, /title="确认删除"/);
	assert.match(source, /pool && pool\.toLowerCase\(\) !== upstream\.toLowerCase\(\)/);
});

test("group detection keeps a configuration per selectable model", () => {
  assert.match(source, /const modelTestConfigByID = ref\(\{\}\)/);
  assert.match(source, /function openGroupModelTest\(group\)/);
  assert.match(source, /modelTestConfigByID\.value\[String\(payload\?\.modelId \|\| ""\)\]/);
});
