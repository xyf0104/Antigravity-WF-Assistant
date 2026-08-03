const OAUTH_AUTHORIZATION_STATES = new Set([
  "pending",
  "completed",
  "failed",
  "expired",
  "unknown",
]);

function readText(value) {
  return typeof value === "string" ? value.trim() : "";
}

function redactIdentity(identity) {
  if (!identity || typeof identity !== "object") return null;
  const safe = {
    email: readText(identity.email),
    subject: readText(identity.subject ?? identity.sub),
    plan: readText(identity.plan ?? identity.planType ?? identity.plan_type),
  };
  return Object.values(safe).some(Boolean) ? safe : null;
}

// Only loopback flows should receive polling. Manual copy-and-paste OAuth
// keeps its explicit completion UI and never emits repeated bridge calls.
export function isAutomaticOAuthPendingSession(session, editorOpen) {
  return Boolean(
    editorOpen
    && session?.sessionId
    && session.manualCompletionRequired !== true
    && session.automaticCallback === true,
  );
}

// Automatic loopback remains the preferred completion path, but every live
// PKCE session must also offer copy-and-paste recovery. Browsers/extensions can
// block a localhost redirect even when the authorization itself succeeded.
export function canManuallyCompleteOAuthSession(session) {
  return Boolean(readText(session?.sessionId));
}

// Project the native response instead of retaining it wholesale. The backend
// already redacts this API, but this client-side allowlist prevents token or
// verifier fields from ever entering view state if that contract changes.
export function redactOAuthAuthorizationStatus(payload) {
  if (!payload || typeof payload !== "object") return null;
  const sessionId = readText(payload.sessionId ?? payload.sessionID);
  const state = readText(payload.state).toLowerCase();
  if (!sessionId || !OAUTH_AUTHORIZATION_STATES.has(state)) return null;

  const result = payload.result && typeof payload.result === "object" ? payload.result : {};
  const message = readText(payload.message ?? result.message);
  const completion = (state === "completed" || state === "failed")
    ? {
      sessionId,
      ok: state === "completed",
      message,
      accountId: readText(result.accountId ?? result.accountID),
      identity: redactIdentity(result.identity),
    }
    : null;

  return { sessionId, state, message, completion };
}

export function isTerminalOAuthAuthorizationState(state) {
  return state === "completed" || state === "failed" || state === "expired" || state === "unknown";
}
