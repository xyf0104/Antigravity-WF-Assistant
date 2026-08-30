import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const [source, nativeViewSource] = await Promise.all([
  readFile(new URL("../src/views/Accounts.vue", import.meta.url), "utf8"),
  readFile(new URL("../../app.go", import.meta.url), "utf8"),
]);

test("account editor never renders stored additional headers and preserves them until explicitly changed", () => {
  assert.match(source, /const headersTouched = ref\(false\);/);
  assert.match(source, /hasPrivateHeaders: account\.hasPrivateHeaders === true/);
  assert.match(source, /headersText: ""/);
  assert.doesNotMatch(source, /headersText:\s*JSON\.stringify\(account\.headers/);
  assert.match(source, /if \(!form\.value\.id \|\| headersTouched\.value\) \{/);
  assert.match(source, /payload\.headers = parseHeaders\(\);/);
  assert.match(source, /delete payload\.hasPrivateHeaders;/);
  assert.match(source, /已保存的附加请求头不会显示；不编辑此框时保存会保留原值/);
  assert.match(source, /@input="headersTouched = true"/);
  assert.match(nativeViewSource, /type UpstreamAccountView struct \{[\s\S]*HasPrivateHeaders bool\s+`json:"hasPrivateHeaders"`/);
});
