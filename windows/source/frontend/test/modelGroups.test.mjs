import assert from "node:assert/strict";
import test from "node:test";
import { canonicalModelAPIURL, groupModelsByUpstream, modelIsEnabled } from "../src/state/modelGroups.js";

test("smart URLs group endpoint leaves under one API upstream", () => {
  const models = [
    { name: "models/a", provider: "openai", apiUrl: "https://api.xiass.com/v1/chat/completions", apiKey: "key-a", externalModelName: "gpt-a" },
    { name: "models/b", provider: "openai", apiUrl: "https://api.xiass.com", apiKey: "key-a", externalModelName: "gpt-b" },
  ];
  assert.equal(canonicalModelAPIURL(models[0]), "https://api.xiass.com");
  assert.equal(groupModelsByUpstream(models).length, 1);
});

test("non-XIASS base paths remain part of the canonical API URL", () => {
  assert.equal(
    canonicalModelAPIURL({ apiUrl: "https://gateway.example.test/v1/chat/completions" }),
    "https://gateway.example.test/v1"
  );
});

test("different direct credentials and account pools stay separate", () => {
  const base = { provider: "openai", apiUrl: "https://api.example.test/v1", externalModelName: "gpt" };
  const groups = groupModelsByUpstream([
    { ...base, name: "models/a", apiKey: "key-a" },
    { ...base, name: "models/b", apiKey: "key-b" },
    { ...base, name: "models/c", accountIds: ["account-b", "account-a"] },
    { ...base, name: "models/d", accountIds: ["account-a", "account-b"] },
  ]);
  assert.equal(groups.length, 3);
  assert.equal(groups.find((group) => group.models.some((model) => model.name === "models/c")).models.length, 2);
});

test("legacy models default to enabled while explicit false is preserved", () => {
  assert.equal(modelIsEnabled({ name: "models/legacy" }), true);
  assert.equal(modelIsEnabled({ name: "models/off", enabled: false }), false);
});

test("a shared upstream keeps the first non-empty user-managed card name", () => {
  const models = [
    { name: "models/a", provider: "openai", apiUrl: "https://api.example.test/v1", apiKey: "same", externalModelName: "gpt-a" },
    { name: "models/b", provider: "openai", apiUrl: "https://api.example.test/v1", apiKey: "same", externalModelName: "gpt-b", upstreamName: "XIASS 主线路" },
  ];
  const groups = groupModelsByUpstream(models);
  assert.equal(groups.length, 1);
  assert.equal(groups[0].upstreamName, "XIASS 主线路");
});
