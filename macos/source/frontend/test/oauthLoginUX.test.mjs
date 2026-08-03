import assert from "node:assert/strict";
import test from "node:test";

import {
  DEFAULT_OAUTH_PROFILE_ID,
  canStartOAuthLogin,
  chooseOAuthProfileID,
  usesSimplifiedOAuthLogin,
} from "../src/state/oauthLoginUX.js";

const profiles = [
  { id: "claude-code" },
  { id: DEFAULT_OAUTH_PROFILE_ID },
  { id: "grok-cli" },
];

test("OAuth chooses OpenAI / Codex by default without replacing an explicit profile", () => {
  assert.equal(chooseOAuthProfileID(profiles), DEFAULT_OAUTH_PROFILE_ID);
  assert.equal(chooseOAuthProfileID(profiles, { selectedProfileID: "claude-code" }), "claude-code");
});

test("custom OAuth mode deliberately does not choose a provider preset", () => {
  assert.equal(chooseOAuthProfileID(profiles, { advancedCustomOpen: true }), "");
  assert.equal(chooseOAuthProfileID([{ id: "claude-code" }]), "");
});

test("simplified OAuth is exclusive to the standard OAuth credential type", () => {
  assert.equal(usesSimplifiedOAuthLogin("oauth", false), true);
  assert.equal(usesSimplifiedOAuthLogin("oauth", true), false);
  assert.equal(usesSimplifiedOAuthLogin("api_key", false), false);
});

test("OAuth start is available only for a loaded preset or explicit custom mode", () => {
  assert.equal(canStartOAuthLogin(null, false), false);
  assert.equal(canStartOAuthLogin({ id: "openai-codex" }, false), true);
  assert.equal(canStartOAuthLogin(null, true), true);
});
