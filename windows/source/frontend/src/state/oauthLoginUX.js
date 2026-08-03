export const DEFAULT_OAUTH_PROFILE_ID = "openai-codex";

function profileID(profile) {
  return typeof profile?.id === "string" ? profile.id.trim() : "";
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
  if (selectedID && profiles.some((profile) => profileID(profile) === selectedID)) return selectedID;
  return profiles.some((profile) => profileID(profile) === defaultProfileID) ? defaultProfileID : "";
}

export function usesSimplifiedOAuthLogin(type, advancedCustomOpen) {
  return type === "oauth" && advancedCustomOpen !== true;
}

export function canStartOAuthLogin(profile, advancedCustomOpen) {
  return Boolean(profile?.id) || advancedCustomOpen === true;
}
