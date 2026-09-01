import assert from "node:assert/strict";
import test from "node:test";

import {
  mergeCodexModelProviderCatalogOptions,
  normalizeCodexModelProviderCatalog,
  normalizeCodexModelProviderCatalogSelection,
} from "./codexModelProviderCatalog.ts";

test("normalizes a discovered provider catalog without retaining transport metadata", () => {
  assert.deepEqual(
    normalizeCodexModelProviderCatalog([" gpt-5.6-sol ", "GPT-5.6-SOL", "", "gpt-5.6-terra"]),
    ["gpt-5.6-sol", "gpt-5.6-terra"],
  );
});

test("keeps selections only when they are present in the refreshed catalog", () => {
  const catalog = ["gpt-5.6-sol", "gpt-5.6-terra"];
  assert.equal(
    normalizeCodexModelProviderCatalogSelection("GPT-5.6-SOL", catalog),
    "gpt-5.6-sol",
  );
  assert.equal(normalizeCodexModelProviderCatalogSelection("removed-model", catalog), undefined);
});

test("keeps selected manual models visible after refreshing the upstream catalog", () => {
  assert.deepEqual(
    mergeCodexModelProviderCatalogOptions(
      ["gpt-5.6-sol", "gpt-image-2"],
      ["GPT-5.6-SOL", "my-manual-model"],
    ),
    ["gpt-5.6-sol", "gpt-image-2", "my-manual-model"],
  );
});
