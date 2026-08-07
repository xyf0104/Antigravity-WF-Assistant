import assert from "node:assert/strict";
import test from "node:test";
import {
  normalizeReasoningEffort,
  resolveReasoningProfile,
} from "../src/state/reasoningCapabilities.js";

function values(input) {
  return resolveReasoningProfile(input).options.map((option) => option.value);
}

test("GPT-5.6 exposes its published discrete reasoning levels", () => {
  assert.deepEqual(values({ provider: "openai", model: "gpt-5.6-sol" }), [
    "auto", "none", "low", "medium", "high", "xhigh", "max",
  ]);
});

test("DeepSeek V4 only exposes its native high and max efforts", () => {
  assert.deepEqual(values({ provider: "openai", model: "deepseek-v4-pro" }), ["auto", "high", "max"]);
});

test("Claude effort choices remain model-family specific", () => {
  assert.deepEqual(values({ provider: "anthropic", model: "claude-opus-5" }), [
    "auto", "low", "medium", "high", "xhigh", "max",
  ]);
  assert.deepEqual(values({ provider: "anthropic", model: "claude-opus-4.6" }), [
    "auto", "low", "medium", "high", "max",
  ]);
  assert.deepEqual(values({ provider: "anthropic", model: "claude-opus-4.5" }), [
    "auto", "low", "medium", "high",
  ]);
  assert.deepEqual(values({ provider: "anthropic", model: "claude-sonnet-4.5" }), ["auto"]);
});

test("unknown models are automatic only and invalid saved values are downgraded safely", () => {
  const profile = resolveReasoningProfile({ provider: "custom", model: "gateway-private-vision" });
  assert.deepEqual(profile.options.map((option) => option.value), ["auto"]);
  assert.equal(normalizeReasoningEffort("xhigh", profile), "auto");
});

test("provider and API style match the native resolver instead of raw discovery metadata", () => {
  assert.deepEqual(values({ provider: "anthropic", model: "gpt-5.6-sol" }), ["auto"]);
  assert.deepEqual(values({ provider: "openai", apiStyle: "messages", model: "gpt-5.6-sol" }), ["auto"]);
  assert.deepEqual(values({ provider: "openai", apiStyle: "responses", model: "deepseek-v4-pro" }), ["auto"]);
  assert.deepEqual(values({
    provider: "custom",
    model: "gateway-private-reasoner",
    metadata: { supportedReasoningEfforts: ["high", "max"] },
  }), ["auto"]);
  assert.equal(
    normalizeReasoningEffort("xhigh", resolveReasoningProfile({ provider: "openai", model: "deepseek-v4-pro" })),
    "max"
  );
});
