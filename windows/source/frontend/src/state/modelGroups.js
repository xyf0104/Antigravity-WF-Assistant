const AUTO_ENDPOINT_SUFFIXES = [
  "/chat/completions",
  "/chat/messages",
  "/responses",
  "/messages",
  "/models",
];

function text(value) {
  return String(value ?? "").trim();
}

function normalizedProvider(value) {
  return text(value).toLowerCase() || "openai";
}

function normalizedEndpointMode(model) {
  return text(model?.endpointMode).toLowerCase() || (model?.messagePathMode === "manual" ? "manual" : "auto");
}

function normalizedAccountIDs(model) {
  return [...new Set((Array.isArray(model?.accountIds) ? model.accountIds : [])
    .map((id) => text(id))
    .filter(Boolean))].sort();
}

function normalizedHeaders(headers) {
  if (!headers || typeof headers !== "object" || Array.isArray(headers)) return "";
  return Object.entries(headers)
    .map(([name, value]) => [text(name).toLowerCase(), String(value ?? "")])
    .filter(([name]) => Boolean(name))
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([name, value]) => `${name}=${value}`)
    .join("&");
}

function privateFingerprint(value) {
  // This key only needs to prevent different credentials from collapsing into
  // one card. Keeping the value out of the VDOM key prevents accidental
  // exposure through UI inspection, logs, or error reports.
  let hash = 2166136261;
  for (const character of String(value ?? "")) {
    hash ^= character.charCodeAt(0);
    hash = Math.imul(hash, 16777619);
  }
  return (hash >>> 0).toString(36);
}

// This is intentionally a renderer-only grouping key. Direct API keys remain
// in local state only and are never included in anything rendered or logged.
function authBindingKey(model) {
  const accountIDs = normalizedAccountIDs(model);
  if (accountIDs.length) return `accounts:${accountIDs.join(",")}`;
  return [
    "direct",
    text(model?.authMode).toLowerCase() || "bearer",
    text(model?.authHeader).toLowerCase(),
    privateFingerprint(normalizedHeaders(model?.headers)),
    privateFingerprint(model?.apiKey),
  ].join(":");
}

// Smart endpoint mode is defined in terms of the reusable base URL, whereas
// manual mode preserves the exact endpoint because a path/query can be part of
// a user-managed gateway contract.
export function canonicalModelAPIURL(model) {
  const raw = text(model?.apiUrl);
  if (!raw) return "";
  if (normalizedEndpointMode(model) === "manual") return `manual:${raw}`;

  try {
    const parsed = new URL(raw);
    if (parsed.protocol !== "https:" && parsed.protocol !== "http:") return raw.replace(/\/+$/, "");
    let path = parsed.pathname.replace(/\/+$/, "");
    const lowerPath = path.toLowerCase();
    for (const suffix of AUTO_ENDPOINT_SUFFIXES) {
      if (!lowerPath.endsWith(suffix)) continue;
      path = path.slice(0, -suffix.length).replace(/\/+$/, "");
      break;
    }
    if (parsed.hostname.toLowerCase() === "api.xiass.com" && (path === "" || path === "/v1")) path = "";
    return `${parsed.protocol.toLowerCase()}//${parsed.host.toLowerCase()}${path}${parsed.search}`;
  } catch {
    return raw.replace(/\/+$/, "");
  }
}

export function upstreamGroupKey(model) {
  return [
    normalizedProvider(model?.provider),
    canonicalModelAPIURL(model),
    authBindingKey(model),
  ].join("|");
}

export function groupModelsByUpstream(models) {
  const groups = new Map();
  for (const model of Array.isArray(models) ? models : []) {
    const key = upstreamGroupKey(model);
    let group = groups.get(key);
    if (!group) {
      group = {
        key,
        provider: normalizedProvider(model?.provider),
        apiUrl: canonicalModelAPIURL(model),
        upstreamName: text(model?.upstreamName),
        models: [],
      };
      groups.set(key, group);
    }
    if (!group.upstreamName) group.upstreamName = text(model?.upstreamName);
    group.models.push(model);
  }
  return [...groups.values()].map((group) => ({
    ...group,
    models: [...group.models].sort((left, right) => {
      const leftName = text(left?.displayName || left?.externalModelName || left?.name);
      const rightName = text(right?.displayName || right?.externalModelName || right?.name);
      return leftName.localeCompare(rightName, undefined, { sensitivity: "base" });
    }),
  }));
}

export function modelIsEnabled(model) {
  return model?.enabled !== false;
}
