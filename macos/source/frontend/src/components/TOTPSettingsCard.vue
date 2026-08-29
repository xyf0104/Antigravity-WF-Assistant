<script setup>
import { computed, onMounted, onUnmounted, ref } from "vue";
import Badge from "@/components/ui/Badge.vue";
import Button from "@/components/ui/Button.vue";
import Card from "@/components/ui/Card.vue";
import Modal from "@/components/ui/Modal.vue";
import {
  addTOTPEntry,
  deleteTOTPEntry,
  exportTOTPEncrypted,
  generateTOTPCode,
  getTOTPEntries,
} from "@/state/appState";

const entries = ref([]);
const loading = ref(false);
const adding = ref(false);
const actionID = ref("");
const editorOpen = ref(false);
const exportOpen = ref(false);
const confirmDelete = ref(null);
const importMode = ref("uri");
const error = ref("");
const notice = ref("");
const editorError = ref("");
const exportError = ref("");
const codes = ref({});
const now = ref(Date.now());
let clock = null;

// Draft values stay inside this component and are cleared whenever its editor
// closes. They are never copied into the shared state, localStorage, logs, or
// diagnostic exports. The operating system credential vault owns the secret.
const draft = ref(newDraft());
const exportDraft = ref({ password: "", confirmation: "" });

const statusLabel = computed(() => {
  if (loading.value) return "正在读取";
  return entries.value.length ? `${entries.value.length} 个本机验证器` : "尚未添加";
});

function newDraft() {
  return {
    uri: "",
    secret: "",
    label: "",
    issuer: "",
    account: "",
    algorithm: "SHA1",
    digits: 6,
    period: 30,
  };
}

function resetFeedback() {
  error.value = "";
  notice.value = "";
}

function clearEditorDraft() {
  draft.value = newDraft();
  editorError.value = "";
}

async function load() {
  loading.value = true;
  try {
    const result = await getTOTPEntries();
    if (!result?.ok) {
      error.value = result?.message || "无法读取本机验证器。";
      return;
    }
    entries.value = Array.isArray(result.entries) ? result.entries : [];
    const active = new Set(entries.value.map((entry) => entry.id));
    codes.value = Object.fromEntries(Object.entries(codes.value).filter(([id]) => active.has(id)));
  } catch (cause) {
    error.value = cause?.message || "无法读取本机验证器。";
  } finally {
    loading.value = false;
  }
}

function openEditor() {
  resetFeedback();
  clearEditorDraft();
  importMode.value = "uri";
  editorOpen.value = true;
}

function closeEditor() {
  if (adding.value) return;
  editorOpen.value = false;
  clearEditorDraft();
}

function inputForDraft() {
  if (importMode.value === "uri") return { uri: draft.value.uri.trim() };
  return {
    secret: draft.value.secret.trim(),
    label: draft.value.label.trim(),
    issuer: draft.value.issuer.trim(),
    account: draft.value.account.trim(),
    algorithm: draft.value.algorithm,
    digits: Number(draft.value.digits),
    period: Number(draft.value.period),
  };
}

async function saveEntry() {
  editorError.value = "";
  if (importMode.value === "uri" && !draft.value.uri.trim()) {
    editorError.value = "请粘贴验证器提供的 otpauth://totp/ 链接。";
    return;
  }
  if (importMode.value === "manual" && (!draft.value.secret.trim() || !draft.value.label.trim())) {
    editorError.value = "手动添加需要 Base32 密钥和一个识别名称。";
    return;
  }
  adding.value = true;
  try {
    const result = await addTOTPEntry(inputForDraft());
    if (!result?.ok) {
      editorError.value = result?.message || "无法保存验证器。";
      return;
    }
    entries.value = Array.isArray(result.entries) ? result.entries : [];
    notice.value = result.message || "验证器已保存到系统凭据库。";
    editorOpen.value = false;
    clearEditorDraft();
  } catch (cause) {
    editorError.value = cause?.message || "无法保存验证器。";
  } finally {
    adding.value = false;
  }
}

function currentCode(id) {
  return codes.value[id] || null;
}

function remainingSeconds(id) {
  const until = Date.parse(currentCode(id)?.validUntil || "");
  if (!Number.isFinite(until)) return 0;
  return Math.max(0, Math.ceil((until - now.value) / 1000));
}

function codeExpired(id) {
  return Boolean(currentCode(id)) && remainingSeconds(id) === 0;
}

async function showCode(entry) {
  resetFeedback();
  actionID.value = `code:${entry.id}`;
  try {
    const result = await generateTOTPCode(entry.id);
    if (!result?.ok || !result.code?.value) {
      error.value = result?.message || "无法生成动态验证码。";
      return;
    }
    codes.value = { ...codes.value, [entry.id]: result.code };
    notice.value = "动态验证码仅在此窗口短时显示。";
  } catch (cause) {
    error.value = cause?.message || "无法生成动态验证码。";
  } finally {
    actionID.value = "";
  }
}

async function copyCode(entry) {
  const value = currentCode(entry.id)?.value;
  if (!value || codeExpired(entry.id)) {
    await showCode(entry);
    return;
  }
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(value);
    } else {
      const area = document.createElement("textarea");
      area.value = value;
      area.setAttribute("readonly", "");
      area.style.position = "fixed";
      area.style.opacity = "0";
      document.body.appendChild(area);
      area.select();
      const copied = document.execCommand("copy");
      area.remove();
      if (!copied) throw new Error("浏览器未允许复制");
    }
    notice.value = "验证码已复制到剪贴板。";
  } catch (cause) {
    error.value = cause?.message || "无法复制验证码。";
  }
}

function openDelete(entry) {
  resetFeedback();
  confirmDelete.value = entry;
}

async function removeEntry() {
  const entry = confirmDelete.value;
  if (!entry) return;
  resetFeedback();
  actionID.value = `delete:${entry.id}`;
  try {
    const result = await deleteTOTPEntry(entry.id);
    if (!result?.ok) {
      error.value = result?.message || "删除验证器失败。";
      return;
    }
    entries.value = Array.isArray(result.entries) ? result.entries : [];
    const nextCodes = { ...codes.value };
    delete nextCodes[entry.id];
    codes.value = nextCodes;
    notice.value = result.message || "验证器已从系统凭据库删除。";
    confirmDelete.value = null;
  } catch (cause) {
    error.value = cause?.message || "删除验证器失败。";
  } finally {
    actionID.value = "";
  }
}

function closeExport(force = false) {
  if (!force && actionID.value === "export") return;
  exportOpen.value = false;
  exportError.value = "";
  exportDraft.value = { password: "", confirmation: "" };
}

async function exportEntries() {
  exportError.value = "";
  if (exportDraft.value.password.length < 10) {
    exportError.value = "导出密码至少需要 10 个字符。";
    return;
  }
  if (exportDraft.value.password !== exportDraft.value.confirmation) {
    exportError.value = "两次输入的导出密码不一致。";
    return;
  }
  actionID.value = "export";
  try {
    const result = await exportTOTPEncrypted(exportDraft.value.password);
    if (!result?.ok) {
      exportError.value = result?.message || "无法导出加密备份。";
      return;
    }
    notice.value = result.message || "已导出加密验证器备份。";
    closeExport(true);
  } catch (cause) {
    exportError.value = cause?.message || "无法导出加密备份。";
  } finally {
    actionID.value = "";
  }
}

function formatEntry(entry) {
  return [entry.issuer, entry.account].filter(Boolean).join(" · ") || entry.label || "未命名验证器";
}

function clearSensitiveViewState() {
  clearEditorDraft();
  exportDraft.value = { password: "", confirmation: "" };
  codes.value = {};
}

onMounted(() => {
  void load();
  clock = window.setInterval(() => { now.value = Date.now(); }, 1000);
});

onUnmounted(() => {
  if (clock) window.clearInterval(clock);
  clearSensitiveViewState();
});
</script>

<template>
  <Card title="验证器（2FA）" subtitle="密钥只保存在本机系统凭据库；不会进入 XIASS Tools 的同步、日志或诊断文件。">
    <template #action>
      <Badge :tone="entries.length ? 'ok' : 'neutral'" :label="statusLabel" />
    </template>

    <div class="totp-card">
      <div class="totp-intro">
        <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 2.8 4.7 6v5.1c0 4.5 3.1 8.6 7.3 10.1 4.2-1.5 7.3-5.6 7.3-10.1V6L12 2.8Zm0 5.1a2.25 2.25 0 1 1 0 4.5 2.25 2.25 0 0 1 0-4.5Zm3.2 9.1H8.8v-1.1c0-1.6 1.4-2.9 3.2-2.9s3.2 1.3 3.2 2.9V17Z" /></svg>
        <div>
          <strong>本地验证器</strong>
          <p>支持标准 <code>otpauth://totp/</code> 链接或手动 Base32 密钥。保存后由 Keychain / Credential Manager 保护。</p>
        </div>
      </div>

      <p v-if="error" class="feedback error" role="alert">{{ error }}</p>
      <p v-if="notice" class="feedback success" role="status">{{ notice }}</p>

      <div v-if="loading" class="empty-state">正在读取本机验证器…</div>
      <div v-else-if="!entries.length" class="empty-state">还没有验证器。添加后可在这里按需显示和复制动态验证码。</div>
      <div v-else class="entry-list">
        <article v-for="entry in entries" :key="entry.id" class="entry-row">
          <div class="entry-copy">
            <strong>{{ entry.label }}</strong>
            <span>{{ formatEntry(entry) }}</span>
            <small>{{ entry.algorithm }} · {{ entry.digits }} 位 · {{ entry.period }} 秒</small>
          </div>
          <div class="entry-code" :class="{ expired: codeExpired(entry.id) }">
            <template v-if="currentCode(entry.id) && !codeExpired(entry.id)">
              <output class="code-value" :aria-label="`${entry.label} 的动态验证码`">{{ currentCode(entry.id).value }}</output>
              <span>{{ remainingSeconds(entry.id) }} 秒后失效</span>
            </template>
            <span v-else>尚未显示</span>
          </div>
          <div class="entry-actions" aria-label="验证器操作">
            <Button variant="plain" size="sm" :loading="actionID === `code:${entry.id}`" @click="showCode(entry)">{{ currentCode(entry.id) && !codeExpired(entry.id) ? '刷新' : '显示验证码' }}</Button>
            <Button variant="tinted" size="sm" :disabled="!currentCode(entry.id) || codeExpired(entry.id)" @click="copyCode(entry)">复制</Button>
            <Button variant="danger" size="sm" :disabled="Boolean(actionID)" @click="openDelete(entry)">删除</Button>
          </div>
        </article>
      </div>

      <div class="totp-actions">
        <Button variant="plain" :disabled="loading || actionID === 'export'" @click="openEditor">添加验证器</Button>
        <Button variant="tinted" :disabled="!entries.length || Boolean(actionID)" @click="exportOpen = true">导出加密备份</Button>
      </div>
    </div>
  </Card>

  <Modal :open="editorOpen" title="添加本地验证器" wide persistent :closable="!adding" @close="closeEditor">
    <div class="editor">
      <p>选择一种导入方式。密钥仅用于本次保存，随后写入系统凭据库并立即从界面清除。</p>
      <div class="mode-switch" role="radiogroup" aria-label="验证器导入方式">
        <button type="button" :class="{ active: importMode === 'uri' }" :aria-checked="importMode === 'uri'" role="radio" @click="importMode = 'uri'">粘贴验证器链接</button>
        <button type="button" :class="{ active: importMode === 'manual' }" :aria-checked="importMode === 'manual'" role="radio" @click="importMode = 'manual'">手动输入密钥</button>
      </div>

      <label v-if="importMode === 'uri'" class="field">
        <span>验证器链接</span>
        <textarea v-model="draft.uri" rows="4" autocomplete="off" spellcheck="false" placeholder="otpauth://totp/服务名:账号?secret=…" />
        <small>从服务的“手动设置验证器”或二维码对应链接中复制。仅支持标准 TOTP 链接。</small>
      </label>

      <template v-else>
        <div class="two-fields">
          <label class="field"><span>识别名称</span><input v-model="draft.label" autocomplete="off" placeholder="例如：XIASS API" /></label>
          <label class="field"><span>Base32 密钥</span><input v-model="draft.secret" type="password" autocomplete="new-password" spellcheck="false" placeholder="例如：JBSWY3DPEHPK3PXP" /></label>
          <label class="field"><span>服务商（可选）</span><input v-model="draft.issuer" autocomplete="off" placeholder="例如：OpenAI" /></label>
          <label class="field"><span>账号（可选）</span><input v-model="draft.account" autocomplete="off" placeholder="例如：name@example.com" /></label>
          <label class="field"><span>算法</span><select v-model="draft.algorithm"><option>SHA1</option><option>SHA256</option><option>SHA512</option></select></label>
          <label class="field"><span>验证码位数</span><select v-model.number="draft.digits"><option :value="6">6 位</option><option :value="8">8 位</option></select></label>
          <label class="field"><span>更新周期</span><select v-model.number="draft.period"><option :value="30">30 秒</option><option :value="60">60 秒</option></select></label>
        </div>
      </template>

      <p v-if="editorError" class="feedback error" role="alert">{{ editorError }}</p>
    </div>
    <template #footer>
      <Button variant="plain" :disabled="adding" @click="closeEditor">取消</Button>
      <Button variant="filled" :loading="adding" :disabled="adding" @click="saveEntry">保存到系统凭据库</Button>
    </template>
  </Modal>

  <Modal :open="exportOpen" title="导出加密验证器备份" persistent :closable="actionID !== 'export'" @close="closeExport">
    <div class="editor">
      <p>导出文件会使用 AES-256-GCM 加密，并由你设置的导出密码保护。请分别妥善保存密码和导出文件。</p>
      <label class="field"><span>导出密码</span><input v-model="exportDraft.password" type="password" autocomplete="new-password" /></label>
      <label class="field"><span>再次输入导出密码</span><input v-model="exportDraft.confirmation" type="password" autocomplete="new-password" /></label>
      <p v-if="exportError" class="feedback error" role="alert">{{ exportError }}</p>
    </div>
    <template #footer>
      <Button variant="plain" :disabled="actionID === 'export'" @click="closeExport">取消</Button>
      <Button variant="filled" :loading="actionID === 'export'" :disabled="actionID === 'export'" @click="exportEntries">选择保存位置</Button>
    </template>
  </Modal>

  <Modal :open="Boolean(confirmDelete)" title="删除本地验证器" persistent :closable="!actionID" @close="confirmDelete = null">
    <div class="editor"><p>将从系统凭据库和 XIASS Tools 的本机索引中删除“{{ confirmDelete?.label }}”。此操作无法恢复。</p></div>
    <template #footer>
      <Button variant="plain" :disabled="Boolean(actionID)" @click="confirmDelete = null">取消</Button>
      <Button variant="danger" :loading="Boolean(actionID)" :disabled="Boolean(actionID)" @click="removeEntry">删除</Button>
    </template>
  </Modal>
</template>

<style scoped>
.totp-card { display:flex; flex-direction:column; gap:12px; }
.totp-intro { display:flex; gap:11px; padding:11px 12px; border:1px solid var(--accent-border); border-radius:var(--r-md); background:var(--accent-soft); }
.totp-intro svg { width:25px; height:25px; flex:none; fill:var(--accent); }
.totp-intro strong { display:block; font-size:13px; color:var(--text-primary); }
.totp-intro p, .editor p { margin:2px 0 0; color:var(--text-secondary); font-size:12px; line-height:1.55; }
.totp-intro code { font-family:var(--font-num); font-size:11px; color:var(--text-primary); }
.feedback { margin:0; padding:9px 11px; border-radius:var(--r-sm); font-size:12px; white-space:pre-wrap; }
.feedback.error { color:var(--red); background:rgba(255,69,58,.1); }
.feedback.success { color:var(--green); background:rgba(48,209,88,.1); }
.empty-state { padding:14px; border:1px dashed var(--separator-strong); border-radius:var(--r-md); color:var(--text-secondary); font-size:12px; text-align:center; }
.entry-list { overflow:hidden; border:1px solid var(--separator); border-radius:var(--r-md); }
.entry-row { display:grid; grid-template-columns:minmax(0,1fr) minmax(116px,auto) auto; gap:12px; align-items:center; padding:11px 12px; background:var(--bg-card); }
.entry-row + .entry-row { border-top:1px solid var(--separator); }
.entry-copy { min-width:0; display:flex; flex-direction:column; gap:2px; }
.entry-copy strong { overflow:hidden; color:var(--text-primary); font-size:13px; text-overflow:ellipsis; white-space:nowrap; }
.entry-copy span, .entry-copy small { overflow:hidden; color:var(--text-secondary); font-size:11px; text-overflow:ellipsis; white-space:nowrap; }
.entry-copy small { color:var(--text-tertiary); font-family:var(--font-num); }
.entry-code { min-width:116px; display:flex; flex-direction:column; align-items:flex-end; gap:2px; color:var(--text-tertiary); font-size:11px; font-variant-numeric:tabular-nums; }
.entry-code.expired { color:var(--orange); }
.code-value { color:var(--accent-strong); font-family:var(--font-num); font-size:19px; font-weight:700; letter-spacing:.1em; line-height:1.1; }
.entry-actions, .totp-actions { display:flex; align-items:center; gap:7px; flex-wrap:wrap; }
.totp-actions { justify-content:flex-end; }
.editor { display:flex; flex-direction:column; gap:12px; }
.mode-switch { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:7px; padding:4px; border-radius:var(--r-md); background:var(--bg-inset); border:1px solid var(--separator); }
.mode-switch button { min-height:34px; padding:7px 9px; border-radius:calc(var(--r-md) - 4px); color:var(--text-secondary); font:600 12px var(--font-ui); transition:background .16s var(--ease), color .16s var(--ease), box-shadow .16s var(--ease); }
.mode-switch button.active { color:var(--text-primary); background:var(--bg-elevated); box-shadow:0 2px 7px rgba(0,0,0,.11); }
.mode-switch button:focus-visible, .field input:focus, .field textarea:focus, .field select:focus { outline:none; border-color:var(--accent); box-shadow:0 0 0 3px var(--accent-soft); }
.field { display:flex; flex-direction:column; gap:6px; color:var(--text-primary); font-size:12px; font-weight:650; }
.field input, .field textarea, .field select { width:100%; border:1px solid var(--separator-strong); border-radius:var(--r-sm); background:var(--bg-inset); color:var(--text-primary); font:13px var(--font-ui); transition:border-color .16s var(--ease), box-shadow .16s var(--ease); }
.field input, .field select { min-height:36px; padding:0 10px; }
.field textarea { min-height:90px; padding:9px 10px; resize:vertical; font-family:var(--font-num); font-size:12px; }
.field small { color:var(--text-tertiary); font-size:11px; font-weight:430; line-height:1.45; }
.two-fields { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:10px; }
@media (max-width:620px) { .entry-row { grid-template-columns:1fr; gap:8px; }.entry-code { align-items:flex-start; }.entry-actions { justify-content:flex-start; }.two-fields { grid-template-columns:1fr; }.totp-actions { justify-content:stretch; }.totp-actions :deep(.btn) { flex:1; justify-content:center; } }
</style>
