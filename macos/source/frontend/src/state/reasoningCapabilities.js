// Model reasoning capabilities are deliberately resolved in one small,
// side-effect-free module. The current /models bridge returns only id/name,
// so it mirrors the native storage resolver's verified provider + API-style +
// model-family rules exactly. An unknown model must remain "auto" instead of
// advertising a value that could turn into an upstream 400 response.

export const AUTO_REASONING_EFFORT = "auto";

const EFFORT_ORDER = ["none", "minimal", "low", "medium", "high", "xhigh", "max"];

const EFFORT_LABELS = Object.freeze({
	auto: "auto",
	none: "none",
	minimal: "minimal",
	low: "low",
	medium: "medium",
	high: "high",
	xhigh: "xhigh",
	max: "max",
});

function text(value) {
  return String(value ?? "").trim();
}

function normalizedModelSlug(value) {
  return text(value)
    .toLowerCase()
    .replace(/^models\//, "")
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

function normalizedEffort(value) {
  const candidate = text(value)
    .toLowerCase()
    .replace(/[\s_.-]+/g, "");
  if (candidate === "default") return AUTO_REASONING_EFFORT;
  if (candidate === "off") return "none";
  if (candidate === "extra" || candidate === "extrahigh") return "xhigh";
  return [AUTO_REASONING_EFFORT, ...EFFORT_ORDER].includes(candidate) ? candidate : "";
}

function profileOptions(values) {
  const allowed = new Set([AUTO_REASONING_EFFORT]);
  for (const value of values || []) {
    const normalized = normalizedEffort(value);
    if (normalized) allowed.add(normalized);
  }
  return [AUTO_REASONING_EFFORT, ...EFFORT_ORDER]
    .filter((value) => allowed.has(value))
    .map((value) => ({ value, label: EFFORT_LABELS[value] }));
}

function profile({
  family,
  strategy,
  efforts = [],
  note = "",
  source = "built-in",
  supportsThinkingToggle = false,
  supportsBudget = false,
  mappings = [],
}) {
  return Object.freeze({
    family,
    strategy,
    source,
    note,
    supportsThinkingToggle,
    supportsBudget,
    mappings,
    options: profileOptions(efforts),
  });
}

function sourceModelName(input) {
  if (typeof input === "string") return text(input);
  return text(
    input?.model
      ?? input?.externalModelName
      ?? input?.external_model_name
      ?? input?.modelId
      ?? input?.model_id
      ?? input?.id
      ?? input?.name
  );
}

function sourceProvider(input) {
  if (typeof input === "string") return "openai";
  switch (text(input?.provider).toLowerCase()) {
    case "anthropic":
      return "anthropic";
    case "grok":
      return "grok";
    case "custom":
      return "custom";
    default:
      return "openai";
  }
}

function sourceAPIStyle(input) {
  const value = text(typeof input === "string" ? "" : input?.apiStyle).toLowerCase();
  return ["auto", "chat_completions", "responses", "messages"].includes(value) ? value : "auto";
}

function modelTokens(value) {
  return text(value)
    .replace(/^models\//i, "")
    .toLowerCase()
    .split(/[^a-z0-9]+/)
    .filter(Boolean);
}

function isGPT56(tokens) {
  return tokens.some((token, index) =>
    (token === "gpt" && tokens[index + 1] === "5" && tokens[index + 2] === "6")
    || (token === "gpt5" && tokens[index + 1] === "6")
  );
}

function isDeepSeekV4(tokens) {
  return tokens.some((token, index) => token === "deepseek" && (
    tokens[index + 1] === "v4"
    || (tokens[index + 1] === "v" && tokens[index + 2] === "4")
    || tokens[index + 1] === "4"
  ));
}

function claudeSeries(tokens) {
  if (!tokens.includes("claude")) return null;
  const families = new Set(["fable", "mythos", "opus", "sonnet", "haiku"]);
  for (let index = 0; index < tokens.length; index += 1) {
    const family = tokens[index];
    if (!families.has(family)) continue;
    const nextMajor = Number(tokens[index + 1]);
    if (Number.isInteger(nextMajor) && nextMajor <= 9) {
      const nextMinor = Number(tokens[index + 2]);
      return { family, major: nextMajor, minor: Number.isInteger(nextMinor) && nextMinor <= 9 ? nextMinor : 0 };
    }
    const beforeMajor = Number(tokens[index - 2]);
    const beforeMinor = Number(tokens[index - 1]);
    if (Number.isInteger(beforeMajor) && beforeMajor <= 9 && Number.isInteger(beforeMinor) && beforeMinor <= 9) {
      return { family, major: beforeMajor, minor: beforeMinor };
    }
    const previousMajor = Number(tokens[index - 1]);
    if (Number.isInteger(previousMajor) && previousMajor <= 9) return { family, major: previousMajor, minor: 0 };
  }
  return null;
}

/**
 * Resolve the discrete reasoning choices safe to render for one model.
 *
 * `input` may be a saved CustomModel, a /models discovery item augmented with
 * `provider`, or simply { provider, model }. It must stay in lockstep with
 * storage.ResolveReasoningProfile until a full native capability binding is
 * implemented through discovery, validation, persistence, and conversion.
 */
export function resolveReasoningProfile(input = {}) {
  const modelName = sourceModelName(input);
  const slug = normalizedModelSlug(modelName);
  const tokens = modelTokens(modelName);
  const provider = sourceProvider(input);
  const apiStyle = sourceAPIStyle(input);
  if (!slug || /(?:^|-)(?:embedding|whisper|tts)(?:-|$)/.test(slug)) {
    return profile({
      family: "",
      strategy: "unsupported",
      note: "该模型未声明可设置的离散推理等级，将使用上游默认行为。",
    });
  }

  if (provider === "anthropic") {
    const series = claudeSeries(tokens);
    if (series) {
      const { family, major, minor } = series;
      const modernClaude5 = major === 5 && ["fable", "mythos", "opus", "sonnet"].includes(family);
      const fullClaude = modernClaude5 || (family === "opus" && major === 4 && [8, 7].includes(minor));
      const maxWithoutXHigh = (family === "opus" || family === "sonnet") && major === 4 && minor === 6;
      const opus45 = family === "opus" && major === 4 && minor === 5;
      const legacy = (family === "sonnet" && major === 4 && minor <= 5)
        || (family === "opus" && major === 4 && minor <= 5)
        || (family === "haiku" && major === 4 && minor <= 5)
        || (family === "sonnet" && major === 3 && [5, 7].includes(minor));
      if (fullClaude) {
        return profile({
          family: `claude-${family}-${major}${minor ? `.${minor}` : ""}`,
          strategy: "anthropic_effort",
          efforts: ["low", "medium", "high", "xhigh", "max"],
          note: "该 Claude 系列支持 low、medium、high、xhigh 与 max。",
        });
      }
      if (maxWithoutXHigh) {
        return profile({
          family: `claude-${family}-4.6`,
          strategy: "anthropic_effort",
          efforts: ["low", "medium", "high", "max"],
          note: "该 Claude 系列支持 low、medium、high 与 max。",
        });
      }
      if (opus45) {
        return profile({
          family: "claude-opus-4.5",
          strategy: "anthropic_effort",
          efforts: ["low", "medium", "high"],
          supportsThinkingToggle: true,
          supportsBudget: true,
          note: "Claude Opus 4.5 支持 low、medium 与 high；兼容上游可使用传统思考预算。",
        });
      }
      if (legacy) {
        return profile({
          family: `claude-${family}-${major}.${minor}`,
          strategy: "anthropic_legacy_budget",
          supportsThinkingToggle: true,
          supportsBudget: true,
          note: "该 Claude 系列使用传统思考预算，不支持离散 effort 选择。",
        });
      }
    }
    if (isDeepSeekV4(tokens)) {
      return profile({
        family: "deepseek-v4",
        strategy: "deepseek_anthropic",
        efforts: ["high", "max"],
        mappings: [{ from: "low", to: "high" }, { from: "medium", to: "high" }, { from: "xhigh", to: "max" }],
        note: "DeepSeek V4 原生只接受 high 或 max；自动会保留上游默认思考模式。",
      });
    }
    return profile({ family: "", strategy: "unsupported", note: "该模型未声明可设置的离散推理等级，将使用上游默认行为。" });
  }

  if (apiStyle === "messages") {
    return profile({ family: "", strategy: "unsupported", note: "当前协议不支持此模型的离散推理等级。" });
  }
  if (isGPT56(tokens)) {
    const strategy = apiStyle === "chat_completions"
      ? "openai_chat_completions"
      : apiStyle === "responses"
        ? "openai_responses"
        : "openai_auto";
    return profile({
      family: "gpt-5.6",
      strategy,
      efforts: ["none", "low", "medium", "high", "xhigh", "max"],
      note: "GPT-5.6 支持 none、low、medium、high、xhigh 与 max。",
    });
  }
  if (isDeepSeekV4(tokens) && apiStyle !== "responses") {
    return profile({
      family: "deepseek-v4",
      strategy: "deepseek_openai",
      efforts: ["high", "max"],
      supportsThinkingToggle: true,
      mappings: [{ from: "low", to: "high" }, { from: "medium", to: "high" }, { from: "xhigh", to: "max" }],
      note: "DeepSeek V4 原生只接受 high 或 max；自动会保留上游默认思考模式。",
    });
  }

  return profile({
    family: "",
    strategy: "unsupported",
    note: "该模型未声明可设置的离散推理等级，将使用上游默认行为。",
  });
}

export function normalizeReasoningEffort(value, profileOrInput = {}) {
  const profile = Array.isArray(profileOrInput?.options)
    ? profileOrInput
    : resolveReasoningProfile(profileOrInput);
  const candidate = normalizedEffort(value);
  if (profile.options.some((option) => option.value === candidate)) return candidate;
  const mapping = Array.isArray(profile.mappings) ? profile.mappings.find((item) => item?.from === candidate) : null;
  return profile.options.some((option) => option.value === mapping?.to) ? mapping.to : AUTO_REASONING_EFFORT;
}

export function reasoningEffortLabel(value, profileOrInput = {}) {
  const selected = normalizeReasoningEffort(value, profileOrInput);
  return profileOrInput?.options?.find?.((option) => option.value === selected)?.label
    || EFFORT_LABELS[selected]
    || EFFORT_LABELS.auto;
}
