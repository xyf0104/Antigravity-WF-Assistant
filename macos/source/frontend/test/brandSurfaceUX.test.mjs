import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const accountsSource = await readFile(new URL("../src/views/Accounts.vue", import.meta.url), "utf8");
const modelsSource = await readFile(new URL("../src/views/Models.vue", import.meta.url), "utf8");

test("visible account and model guidance use the XIASS Tools brand while legacy runtime contracts stay internal", () => {
  for (const source of [accountsSource, modelsSource]) {
    assert.match(source, /XIASS Tools 会/);
    assert.match(source, /X-Client.*XIASS Tools/);
    assert.doesNotMatch(source, /；WF 会|；WF 自动|：WF 会/);
    assert.doesNotMatch(source, /X-Client.*WF/);
  }
});
