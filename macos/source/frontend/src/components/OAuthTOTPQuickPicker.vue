<script setup>
import { computed, onBeforeUnmount, ref, watch } from "vue";
import Button from "@/components/ui/Button.vue";
import { generateTOTPCode, getTOTPEntries } from "@/state/appState";

const props = defineProps({
  // The picker is rendered only while a user-initiated OAuth session is
  // visible. Closing that session immediately clears generated codes from the
  // WebView; the Base32 secret remains in the OS credential vault.
  open: Boolean,
});

const entries = ref([]);
const codes = ref({});
const loading = ref(false);
const actionID = ref("");
const notice = ref("");
const error = ref("");
const now = ref(Date.now());
let clock = null;

const hasEntries = computed(() => entries.value.length > 0);

function resetFeedback() {
  notice.value = "";
  error.value = "";
}

function clearCodes() {
  codes.value = {};
}

function entrySubtitle(entry) {
  return [entry?.issuer, entry?.account].filter(Boolean).join(" · ") || entry?.label || "本地验证器";
}

function currentCode(entryID) {
  return codes.value[entryID] || null;
}

function remainingSeconds(entryID) {
  const expiresAt = Date.parse(currentCode(entryID)?.validUntil || "");
  if (!Number.isFinite(expiresAt)) return 0;
  return Math.max(0, Math.ceil((expiresAt - now.value) / 1000));
}

function codeActive(entryID) {
  return Boolean(currentCode(entryID)?.value) && remainingSeconds(entryID) > 0;
}

async function loadEntries() {
  if (!props.open || loading.value) return;
  loading.value = true;
  resetFeedback();
  try {
    const result = await getTOTPEntries();
    if (!result?.ok) {
      error.value = "无法读取本机验证器；请在设置中确认系统凭据库权限。";
      return;
    }
    entries.value = Array.isArray(result.entries) ? result.entries : [];
    const allowed = new Set(entries.value.map((entry) => entry.id));
    codes.value = Object.fromEntries(Object.entries(codes.value).filter(([id]) => allowed.has(id)));
  } catch {
    error.value = "无法读取本机验证器；请在设置中确认系统凭据库权限。";
  } finally {
    loading.value = false;
  }
}

async function copyText(value) {
  if (!navigator.clipboard?.writeText) return false;
  try {
    await navigator.clipboard.writeText(value);
    return true;
  } catch {
    return false;
  }
}

async function revealAndCopy(entry) {
  if (!entry?.id || actionID.value) return;
  resetFeedback();
  actionID.value = entry.id;
  try {
    const result = await generateTOTPCode(entry.id);
    const code = typeof result?.code?.value === "string" ? result.code.value.trim() : "";
    if (!result?.ok || !code) {
      error.value = "无法生成动态验证码。请确认该验证器仍存在且系统凭据库可用。";
      return;
    }
    codes.value = { ...codes.value, [entry.id]: result.code };
    notice.value = await copyText(code)
      ? "验证码已显示并复制到剪贴板。"
      : "验证码已显示；浏览器未允许自动复制，请手动复制。";
  } catch {
    error.value = "无法生成动态验证码。请确认该验证器仍存在且系统凭据库可用。";
  } finally {
    actionID.value = "";
  }
}

async function copyVisibleCode(entry) {
  const code = currentCode(entry?.id)?.value;
  if (!code || !codeActive(entry.id)) {
    await revealAndCopy(entry);
    return;
  }
  resetFeedback();
  if (await copyText(code)) {
    notice.value = "验证码已复制到剪贴板。";
  } else {
    error.value = "浏览器未允许写入剪贴板，请手动复制验证码。";
  }
}

function clearSensitiveViewState() {
  entries.value = [];
  clearCodes();
  resetFeedback();
}

watch(() => props.open, (open) => {
  if (open) {
    void loadEntries();
    return;
  }
  clearSensitiveViewState();
}, { immediate: true });

clock = window.setInterval(() => { now.value = Date.now(); }, 1000);

onBeforeUnmount(() => {
  if (clock) window.clearInterval(clock);
  clearSensitiveViewState();
});
</script>

<template>
  <details v-if="open" class="oauth-totp-picker">
    <summary>
      <span class="totp-dot" aria-hidden="true"></span>
      <span>需要二次验证？使用本机验证器</span>
      <small>{{ loading ? '正在读取' : hasEntries ? `${entries.length} 个` : '未添加' }}</small>
    </summary>
    <div class="totp-panel">
      <p>仅在当前授权窗口按需显示和复制验证码。密钥不进入授权链接、账户、日志或诊断文件。</p>
      <p v-if="notice" class="feedback notice" role="status">{{ notice }}</p>
      <p v-if="error" class="feedback error" role="alert">{{ error }}</p>
      <p v-if="loading" class="empty">正在读取本机验证器…</p>
      <p v-else-if="!hasEntries" class="empty">尚未添加验证器。可在“设置 → 验证器（2FA）”中添加标准 TOTP。</p>
      <div v-else class="entry-list">
        <article v-for="entry in entries" :key="entry.id" class="entry-row">
          <div class="entry-copy">
            <strong>{{ entry.label }}</strong>
            <span>{{ entrySubtitle(entry) }}</span>
          </div>
          <div class="entry-code" :class="{ active: codeActive(entry.id) }">
            <template v-if="codeActive(entry.id)">
              <output :aria-label="`${entry.label} 的动态验证码`">{{ currentCode(entry.id).value }}</output>
              <small>{{ remainingSeconds(entry.id) }} 秒</small>
            </template>
            <span v-else>未显示</span>
          </div>
          <div class="entry-actions">
            <Button variant="tinted" size="sm" :loading="actionID === entry.id" :disabled="Boolean(actionID) && actionID !== entry.id" @click="revealAndCopy(entry)">取码并复制</Button>
            <Button variant="plain" size="sm" :disabled="!codeActive(entry.id) || Boolean(actionID)" @click="copyVisibleCode(entry)">复制</Button>
          </div>
        </article>
      </div>
      <Button variant="plain" size="sm" :disabled="loading || Boolean(actionID)" @click="loadEntries">刷新验证器</Button>
    </div>
  </details>
</template>

<style scoped>
.oauth-totp-picker { border: 1px solid color-mix(in srgb, var(--teal) 38%, var(--separator)); border-left: 3px solid var(--teal); border-radius: var(--r-sm); background: color-mix(in srgb, var(--teal) 6%, var(--bg-card)); overflow: clip; }
.oauth-totp-picker summary { display: flex; align-items: center; gap: 7px; cursor: pointer; list-style: none; min-height: 37px; color: var(--text-primary); font-size: 11px; font-weight: 700; padding: 0 10px; }.oauth-totp-picker summary::-webkit-details-marker { display: none; }.oauth-totp-picker summary small { margin-left: auto; color: var(--teal); font-family: var(--font-num); font-size: 10px; font-weight: 700; }.oauth-totp-picker summary:focus-visible, .entry-actions :deep(.btn:focus-visible) { outline: 2px solid var(--accent-strong); outline-offset: -2px; }
.totp-dot { width: 7px; height: 7px; flex: 0 0 auto; border-radius: 50%; background: var(--teal); box-shadow: 0 0 0 3px color-mix(in srgb, var(--teal) 16%, transparent); }.totp-panel { display: grid; gap: 8px; border-top: 1px solid var(--separator); padding: 9px 10px 10px; }.totp-panel > p { margin: 0; color: var(--text-secondary); font-size: 10px; line-height: 1.55; }.feedback { border-radius: 7px; padding: 7px 8px; }.feedback.notice { border: 1px solid color-mix(in srgb, var(--green) 34%, var(--separator)); background: color-mix(in srgb, var(--green) 7%, transparent); color: var(--green); }.feedback.error { border: 1px solid color-mix(in srgb, var(--red) 40%, var(--separator)); background: color-mix(in srgb, var(--red) 8%, transparent); color: var(--red); }.empty { border: 1px dashed var(--separator-strong); border-radius: 7px; color: var(--text-tertiary) !important; padding: 8px 9px; }
.entry-list { display: grid; border-top: 1px solid var(--separator); }.entry-row { display: grid; grid-template-columns: minmax(0, 1fr) auto auto; align-items: center; gap: 8px; border-bottom: 1px solid var(--separator); padding: 8px 0; }.entry-row:last-child { border-bottom: 0; }.entry-copy { display: grid; min-width: 0; gap: 2px; }.entry-copy strong { overflow: hidden; color: var(--text-primary); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }.entry-copy span { overflow: hidden; color: var(--text-tertiary); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }.entry-code { display: grid; min-width: 56px; gap: 1px; color: var(--text-tertiary); font-size: 10px; text-align: right; }.entry-code.active { color: var(--teal); }.entry-code output { color: var(--text-primary); font-family: var(--font-num); font-size: 13px; font-weight: 780; letter-spacing: .08em; }.entry-code small { color: var(--text-tertiary); font-family: var(--font-num); font-size: 9px; }.entry-actions { display: flex; gap: 5px; }
@media (max-width: 560px) { .entry-row { grid-template-columns: minmax(0, 1fr) auto; }.entry-actions { grid-column: 1 / -1; }.entry-actions :deep(.btn) { flex: 1; justify-content: center; } }
</style>
