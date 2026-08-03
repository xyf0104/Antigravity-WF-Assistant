import assert from "node:assert/strict";
import test from "node:test";

import {
	canManuallyCompleteOAuthSession,
  isAutomaticOAuthPendingSession,
  isTerminalOAuthAuthorizationState,
  redactOAuthAuthorizationStatus,
} from "../src/state/oauthAuthorizationStatus.js";

test("only an open automatic loopback OAuth session is polled", () => {
  const automatic = {
    sessionId: "session-1",
    automaticCallback: true,
    manualCompletionRequired: false,
  };
  assert.equal(isAutomaticOAuthPendingSession(automatic, true), true);
  assert.equal(isAutomaticOAuthPendingSession(automatic, false), false);
  assert.equal(isAutomaticOAuthPendingSession({ ...automatic, manualCompletionRequired: true }, true), false);
  assert.equal(isAutomaticOAuthPendingSession({ ...automatic, automaticCallback: false }, true), false);
});

test("every live OAuth session exposes a manual callback/code fallback", () => {
  assert.equal(canManuallyCompleteOAuthSession({ sessionId: "automatic", automaticCallback: true }), true);
  assert.equal(canManuallyCompleteOAuthSession({ sessionId: "manual", automaticCallback: false }), true);
  assert.equal(canManuallyCompleteOAuthSession({ sessionId: "  " }), false);
  assert.equal(canManuallyCompleteOAuthSession(null), false);
});

test("authorization status projects only display-safe completion fields", () => {
  const status = redactOAuthAuthorizationStatus({
    state: "completed",
    sessionId: " session-1 ",
    message: " 已完成授权 ",
    access_token: "must-not-reach-renderer",
    refresh_token: "must-not-reach-renderer",
    result: {
      ok: true,
      accountId: " account-1 ",
      access_token: "must-not-reach-renderer",
      refresh_token: "must-not-reach-renderer",
      code_verifier: "must-not-reach-renderer",
      identity: { email: " user@example.com ", plan: " Pro " },
    },
  });

  assert.deepEqual(status, {
    sessionId: "session-1",
    state: "completed",
    message: "已完成授权",
    completion: {
      sessionId: "session-1",
      ok: true,
      message: "已完成授权",
      accountId: "account-1",
      identity: { email: "user@example.com", subject: "", plan: "Pro" },
    },
  });
  assert.doesNotMatch(JSON.stringify(status), /access_token|refresh_token|code_verifier/);
});

test("terminal authorization states stop polling while pending remains active", () => {
  assert.equal(isTerminalOAuthAuthorizationState("pending"), false);
  for (const state of ["completed", "failed", "expired", "unknown"]) {
    assert.equal(isTerminalOAuthAuthorizationState(state), true);
  }
});
