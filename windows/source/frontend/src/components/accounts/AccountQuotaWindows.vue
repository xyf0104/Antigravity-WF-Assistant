<script setup>
import { computed } from "vue";

const props = defineProps({
  account: { type: Object, default: null },
  // `quota` may be the native WF snapshot or an XIASS-style quota response.
  // It is intentionally display-only; this component never derives a quota
  // from missing data or treats a missing value as zero.
  quota: { type: Object, default: null },
  windows: { type: Array, default: () => [] },
  // Optional explicit display inputs. They allow the host to render exactly
  // the model badges and request counters returned by an XIASS-style account
  // detail query without coupling the component to a particular bridge type.
  models: { type: Array, default: () => [] },
  usage: { type: Object, default: null },
  loading: Boolean,
  error: { type: String, default: "" },
  showIdentity: { type: Boolean, default: true },
});

const emit = defineEmits(["refresh", "view-requests"]);

const accountQuota = computed(() => props.quota || props.account?.quota || {});
const identity = computed(() => props.account?.identity || {});
const providerLabel = computed(() => {
  const raw = String(props.account?.provider ?? props.account?.platform ?? "openai").toLowerCase();
  if (raw.includes("anthropic") || raw.includes("claude")) return "Claude";
  if (raw.includes("grok")) return "Grok";
  if (raw.includes("gemini")) return "Gemini";
  return "OpenAI";
});
const accountPlan = computed(() => String(identity.value?.plan ?? props.account?.plan ?? "").trim());
const accountType = computed(() => String(props.account?.type ?? "").toLowerCase());
const assignedModels = computed(() => {
  const source = props.models.length ? props.models : (props.account?.models ?? props.account?.modelIds ?? props.account?.model_ids ?? []);
  return asArray(source).map((model) => {
    if (typeof model === "string") return model;
    return asString(model?.displayName ?? model?.display_name ?? model?.name ?? model?.id).trim();
  }).filter(Boolean);
});
const usageValues = computed(() => {
  const usage = props.usage || props.account?.localUsage || props.account?.local_usage || {};
  const values = [
    [usage.requests ?? usage.requestCount ?? usage.request_count, "请求"],
    [usage.totalTokens ?? usage.total_tokens ?? usage.tokens, "Token"],
    [usage.inputTokens ?? usage.input_tokens, "输入"],
    [usage.outputTokens ?? usage.output_tokens, "输出"],
  ];
  return values.filter(([value]) => value !== undefined && value !== null && value !== "").map(([value, label]) => `${value} ${label}`);
});

function asString(value) {
  if (value === undefined || value === null || value === "") return "";
  return String(value);
}

function asArray(value) {
  return Array.isArray(value) ? value : [];
}

function percent(value) {
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) return null;
  const normalised = parsed >= 0 && parsed <= 1 ? parsed * 100 : parsed;
  return Math.max(0, Math.min(100, normalised));
}

function formatReset(value) {
  const text = asString(value).trim();
  if (!text) return "";
  const millis = Date.parse(text);
  if (!Number.isFinite(millis)) return text;
  const remaining = millis - Date.now();
  if (remaining <= 0) return "现在";
  const minutes = Math.ceil(remaining / 60000);
  if (minutes < 60) return `${minutes} 分钟`;
  const hours = Math.floor(minutes / 60);
  const minutesLeft = minutes % 60;
  if (hours < 48) return `${hours}h${minutesLeft ? ` ${minutesLeft}m` : ""}`;
  const days = Math.floor(hours / 24);
  return `${days}d ${hours % 24}h`;
}

function normalizeWindow(raw, fallbackLabel) {
  if (!raw || typeof raw !== "object") return null;
  const maximum = Number(raw.limit ?? raw.max ?? raw.total ?? raw.capacity);
  const used = Number(raw.used ?? raw.consumed ?? raw.current);
  const remaining = raw.remaining ?? raw.requestsRemaining ?? raw.requests_remaining ?? raw.left;
  let usedPercent = percent(raw.usedPercent ?? raw.used_percent ?? raw.percentage ?? raw.percent ?? raw.utilization);
  if (usedPercent === null && Number.isFinite(maximum) && maximum > 0 && Number.isFinite(used)) usedPercent = (used / maximum) * 100;
  if (usedPercent === null && Number.isFinite(maximum) && maximum > 0 && remaining !== undefined) {
    const remainingNumber = Number(remaining);
    if (Number.isFinite(remainingNumber)) usedPercent = ((maximum - remainingNumber) / maximum) * 100;
  }
  const resetRaw = raw.resetAt ?? raw.reset_at ?? raw.resetsAt ?? raw.reset ?? raw.resetAfter ?? raw.reset_after ?? raw.requestsReset ?? raw.requests_reset;
  const label = asString(raw.label ?? raw.window ?? raw.name ?? raw.period ?? fallbackLabel).trim();
  if (!label && usedPercent === null && !asString(remaining) && !asString(resetRaw)) return null;
  return {
    label: label || "额度",
    usedPercent,
    remaining: asString(remaining),
    reset: formatReset(resetRaw),
    resetRaw: asString(resetRaw),
    tone: usedPercent !== null && usedPercent >= 95 ? "danger" : usedPercent !== null && usedPercent >= 75 ? "warning" : "ok",
  };
}

const quotaWindows = computed(() => {
  if (props.windows.length) return props.windows.map((window, index) => normalizeWindow(window, index === 0 ? "5h" : "7d")).filter(Boolean);
  const quota = accountQuota.value || {};
  const direct = asArray(quota.windows ?? quota.quotaWindows ?? quota.quota_windows ?? quota.rateLimits ?? quota.rate_limits ?? quota.limits);
  if (direct.length) return direct.map((window, index) => normalizeWindow(window, index === 0 ? "5h" : "7d")).filter(Boolean);
  const candidates = [
    [quota.fiveHour ?? quota.five_hour ?? quota["5h"] ?? quota.primaryWindow ?? quota.primary_window, "5h"],
    [quota.sevenDay ?? quota.seven_day ?? quota["7d"] ?? quota.secondaryWindow ?? quota.secondary_window, "7d"],
  ];
  const structured = candidates.map(([window, label]) => normalizeWindow(window, label)).filter(Boolean);
  if (structured.length) return structured;
  // Native snapshot fallback: this is a response-header observation, not a
  // fabricated subscription window. It remains visibly labelled as such.
  const snapshot = normalizeWindow({
    label: quota.source ? "上游响应" : "最近响应",
    remaining: quota.requestsRemaining ?? quota.requests_remaining,
    resetAfter: quota.requestsReset ?? quota.requests_reset ?? quota.retryAfter ?? quota.retry_after,
  }, "最近响应");
  return snapshot ? [snapshot] : [];
});

const quotaMessage = computed(() => asString(accountQuota.value?.message ?? props.error).trim());
const updatedAt = computed(() => {
  const raw = asString(accountQuota.value?.updatedAt ?? accountQuota.value?.updated_at).trim();
  if (!raw) return "";
  const millis = Date.parse(raw);
  if (!Number.isFinite(millis)) return raw;
  return new Intl.DateTimeFormat("zh-CN", { hour: "2-digit", minute: "2-digit" }).format(millis);
});
</script>

<template>
  <section class="quota-panel" aria-label="账户额度窗口">
    <div v-if="showIdentity && account" class="identity-row">
      <div class="identity-copy">
        <div class="identity-name truncate">{{ account.name || identity.email || "未命名账户" }}</div>
        <div class="identity-tags">
          <span class="provider-tag">◎ {{ providerLabel }}</span>
          <span v-if="accountType === 'oauth'" class="oauth-tag">⌕ OAuth</span>
          <span v-if="accountPlan" class="plan-tag">{{ accountPlan }}</span>
          <span v-if="identity.privacyMode || identity.privacy_mode" class="privacy-tag">♢ {{ identity.privacyMode || identity.privacy_mode }}</span>
        </div>
      </div>
      <span v-if="identity.subscriptionExpiresAt || identity.subscription_expires_at" class="expiry">到期 {{ String(identity.subscriptionExpiresAt || identity.subscription_expires_at).slice(0, 10) }}</span>
    </div>

    <div v-if="assignedModels.length || usageValues.length" class="quota-context">
      <div v-if="assignedModels.length" class="model-tags" aria-label="已关联模型">
        <span v-for="model in assignedModels" :key="model">◎ {{ model }}</span>
      </div>
      <div v-if="usageValues.length" class="usage-values" aria-label="账户调用统计">
        <span v-for="value in usageValues" :key="value">{{ value }}</span>
      </div>
    </div>

    <div class="quota-heading">
      <div>
        <div class="quota-title">用量窗口 <span class="info" title="只显示上游实际返回或响应头观测到的数据；未知不会显示为零。">i</span></div>
        <div v-if="updatedAt" class="quota-updated">上次查询 {{ updatedAt }}</div>
      </div>
      <button class="quota-link" type="button" :disabled="loading" @click="emit('refresh')"><span :class="{ spin: loading }">↻</span> {{ loading ? "查询中" : "查询" }}</button>
    </div>

    <div v-if="quotaWindows.length" class="quota-windows">
      <div v-for="window in quotaWindows" :key="`${window.label}-${window.resetRaw}`" class="quota-window">
        <span class="window-label" :class="window.tone">{{ window.label }}</span>
        <div class="window-progress" :aria-label="`${window.label} 用量`">
          <span :class="window.tone" :style="{ width: `${window.usedPercent ?? 0}%` }" />
        </div>
        <span class="window-percent">{{ window.usedPercent === null ? "—" : `${Math.round(window.usedPercent)}%` }}</span>
        <span class="window-reset">{{ window.reset || "上游未返回重置时间" }}</span>
        <span v-if="window.remaining" class="window-remaining">剩余 {{ window.remaining }}</span>
      </div>
    </div>
    <p v-else class="quota-empty">暂无上游返回的额度窗口；可点击“查询”获取实际限额。</p>
    <p v-if="quotaMessage" class="quota-message">{{ quotaMessage }}</p>

    <div v-if="usageValues.length" class="quota-actions">
      <button type="button" @click="emit('view-requests')">◌ 本机统计</button>
    </div>
  </section>
</template>

<style scoped>
.quota-panel { display: grid; gap: 12px; padding: 14px 15px; border: 1px solid var(--separator); border-radius: var(--r-md); background: rgba(13, 29, 49, .28); }
.identity-row { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }.identity-copy { min-width: 0; }.identity-name { color: var(--text-primary); font-size: 15px; font-weight: 690; }
.identity-tags { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 7px; }.identity-tags span { padding: 3px 7px; border-radius: 7px; font-size: 11px; font-weight: 680; }.provider-tag { color: #4feb9b; background: rgba(20, 117, 83, .25); }.oauth-tag { color: #4feb9b; background: rgba(15, 99, 74, .19); }.plan-tag { color: #4feb9b; background: rgba(14, 107, 72, .32); }.privacy-tag { color: #9fd7be; background: rgba(31, 103, 72, .16); }.expiry { color: var(--text-tertiary); font-size: 11px; white-space: nowrap; }
.quota-context { display: flex; align-items: flex-start; justify-content: space-between; gap: 10px; }.model-tags, .usage-values { display: flex; flex-wrap: wrap; gap: 6px; }.model-tags span { max-width: 180px; overflow: hidden; padding: 4px 8px; border-radius: 8px; color: #51e79a; background: rgba(13, 103, 74, .2); font-size: 11px; font-weight: 650; text-overflow: ellipsis; white-space: nowrap; }.usage-values { justify-content: flex-end; }.usage-values span { color: var(--text-tertiary); font: 11px var(--font-num); white-space: nowrap; }
.quota-heading { display: flex; align-items: center; justify-content: space-between; gap: 10px; }.quota-title { color: var(--text-primary); font-size: 14px; font-weight: 700; }.info { display: inline-grid; width: 15px; height: 15px; place-items: center; margin-left: 3px; border: 1px solid var(--text-tertiary); border-radius: 50%; color: var(--text-secondary); font-family: serif; font-size: 10px; }.quota-updated { margin-top: 2px; color: var(--text-tertiary); font-size: 11px; }.quota-link { display: inline-flex; align-items: center; gap: 5px; padding: 4px 0; color: var(--blue); background: transparent; font-size: 12px; }.quota-link:disabled { opacity: .55; }.spin { display: inline-block; animation: quota-spin .8s linear infinite; }
.quota-windows { display: grid; gap: 10px; }.quota-window { display: grid; grid-template-columns: auto minmax(54px, 1fr) auto auto; align-items: center; gap: 9px; min-width: 0; }.window-label { min-width: 31px; padding: 4px 8px; border-radius: 6px; background: rgba(56, 215, 122, .11); color: var(--green); font: 12px var(--font-num); text-align: center; }.window-label.warning { color: #f3c248; background: rgba(243, 194, 72, .13); }.window-label.danger { color: var(--red); background: rgba(255,105,97,.13); }.window-progress { height: 10px; overflow: hidden; border-radius: 999px; background: rgba(145, 165, 196, .22); }.window-progress span { display: block; height: 100%; min-width: 0; border-radius: inherit; background: var(--green); }.window-progress span.warning { background: #f3c248; }.window-progress span.danger { background: var(--red); }.window-percent { min-width: 28px; color: var(--text-secondary); font: 12px var(--font-num); text-align: right; }.window-reset { overflow: hidden; color: var(--text-secondary); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }.window-remaining { grid-column: 2 / -1; margin-top: -5px; color: var(--text-tertiary); font: 11px var(--font-num); }
.quota-empty, .quota-message { margin: 0; color: var(--text-tertiary); font-size: 12px; line-height: 1.5; }.quota-message { color: var(--orange); }.quota-actions { display: flex; gap: 16px; }.quota-actions button { padding: 0; color: var(--blue); background: transparent; font-size: 12px; }.quota-actions button:hover { color: #87bfff; }

/* The account panel was authored for the dark glass canvas. Keep the service,
   plan, model and warning labels readable on Cockpit's light surface. */
:global(:root[data-theme="light"]) .quota-panel { background: var(--bg-inset); border-color: var(--separator-strong); }
:global(:root[data-theme="light"]) :is(.provider-tag, .oauth-tag, .plan-tag, .model-tags span) { color: #15803d; background: rgba(22, 163, 74, .12); }
:global(:root[data-theme="light"]) .privacy-tag { color: #166534; background: rgba(22, 163, 74, .1); }
:global(:root[data-theme="light"]) .window-label { color: #15803d; background: rgba(22, 163, 74, .12); }
:global(:root[data-theme="light"]) .window-label.warning { color: #b45309; background: rgba(245, 158, 11, .14); }
:global(:root[data-theme="light"]) .window-label.danger { color: #b91c1c; background: rgba(239, 68, 68, .12); }
:global(:root[data-theme="light"]) .window-progress span.warning { background: #d97706; }
:global(:root[data-theme="light"]) .window-progress span.danger { background: #dc2626; }
@keyframes quota-spin { to { transform: rotate(360deg); } } @media (max-width: 420px) { .quota-window { grid-template-columns: auto minmax(42px, 1fr) auto; }.window-reset { grid-column: 2 / -1; margin-top: -5px; }.expiry { display: none; } }
</style>
