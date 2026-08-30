export const DEFAULT_OAUTH_PROFILE_ID = "openai-codex";

function profileID(profile) {
  return typeof profile?.id === "string" ? profile.id.trim() : "";
}

function profileAvailability(profile) {
  return typeof profile?.available === "string" ? profile.available.trim().toLowerCase() : "";
}

// Keep a deliberate user choice when it still exists. Otherwise OAuth starts
// with the reviewed OpenAI / Codex profile; no profile is fabricated when the
// backend cannot supply it.
export function chooseOAuthProfileID(profiles, {
  selectedProfileID = "",
  advancedCustomOpen = false,
  defaultProfileID = DEFAULT_OAUTH_PROFILE_ID,
} = {}) {
  if (advancedCustomOpen || !Array.isArray(profiles)) return "";
  const selectedID = typeof selectedProfileID === "string" ? selectedProfileID.trim() : "";
  if (selectedID && profiles.some((profile) => profileID(profile) === selectedID && isOAuthLoginProfileSupported(profile))) return selectedID;
  return profiles.some((profile) => profileID(profile) === defaultProfileID && isOAuthLoginProfileSupported(profile)) ? defaultProfileID : "";
}

export function usesSimplifiedOAuthLogin(type, advancedCustomOpen) {
  return type === "oauth" && advancedCustomOpen !== true;
}

export function canStartOAuthLogin(profile, advancedCustomOpen, publicClientID = "") {
  if (advancedCustomOpen === true) return true;
  const mode = oauthLoginProfileMode(profile);
  if (mode === "bring-your-own-client") return typeof publicClientID === "string" && publicClientID.trim() !== "";
  return mode === "automatic-callback" || mode === "manual-callback";
}

// OAuth providers do not all have the same user journey. Keep that distinction
// in one small, testable helper so the account editor never calls a manual or
// bring-your-own-client flow an "one-click login". An unknown availability is
// deliberately unavailable: only backend-declared, reviewed modes can start.
export function oauthLoginProfileMode(profile) {
  if (!profileID(profile)) return "unavailable";
  const availability = profileAvailability(profile);
  if (availability === "requires_client_id") return "bring-your-own-client";
  if (availability === "manual") return "manual-callback";
  if (availability !== "ready") return "unavailable";
  if (profile.requiresClientId === true || profile.requiresClientID === true) return "bring-your-own-client";
  if (profile.manualCompletionRequired === true || profile.automaticCallback === false) return "manual-callback";
  return "automatic-callback";
}

// Profiles that lack a verified completion mode are omitted from the preset
// selector altogether. Advanced Custom OAuth remains an explicit fallback;
// the UI must not turn an unavailable entry into a tempting login button.
export function isOAuthLoginProfileSupported(profile) {
  return oauthLoginProfileMode(profile) !== "unavailable";
}
