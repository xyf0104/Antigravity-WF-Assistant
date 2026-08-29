<script setup>
import { computed, ref, watch } from "vue";
import Modal from "@/components/ui/Modal.vue";
import Button from "@/components/ui/Button.vue";
import {
  getClaudeCodeConfiguration,
  applyClaudeCodeConfiguration,
  restoreClaudeCodeConfiguration,
  deleteClaudeCodeConfigurationBackup,
  migrateClaudeCodeLegacyBackup,
} from "@/state/appState";

const props = defineProps({ open: Boolean });
const emit = defineEmits(["close", "changed"]);

const data = ref(null);
const draft = ref({ baseUrl: "", authToken: "", model: "" });
const loading = ref(false);
const saving = ref(false);
const actionID = ref("");
const error = ref("");
const notice = ref("");

const snapshot = computed(() => data.value?.snapshot || {});
const busy = computed(() => loading.value || saving.value || Boolean(actionID.value));
const valid = computed(() => Boolean(snapshot.value.valid));
const readyToSave = computed(() => Boolean(
  draft.value.baseUrl.trim()
  && draft.value.authToken.trim()
  && draft.value.model.trim(),
));
const backups = computed(() => data.value?.backups || []);
const legacyBackups = computed(() => data.value?.legacyBackups || []);

function resetVisibleToken() {
  draft.value.authToken = "";
}

function applyStatus(result, { updateDraft = true } = {}) {
  data.value = result || null;
  if (!updateDraft) return;
  const next = result?.snapshot || {};
  draft.value = {
    baseUrl: next.baseUrl || "",
    authToken: "",
    model: next.model || "",
  };
}

async function refresh() {
  loading.value = true;
  error.value = "";
  notice.value = "";
  try {
    const result = await getClaudeCodeConfiguration();
    applyStatus(result);
    if (!result?.ok) {
      error.value = "无法读取 Claude Code 用户设置。请确认 settings.json 是有效的普通 JSON 文件。";
      return;
    }
    notice.value = result.message || "已读取 Claude Code 用户设置。";
  } catch {
    data.value = null;
    resetVisibleToken();
    error.value = "无法读取 Claude Code 用户设置。";
  } finally {
    loading.value = false;
  }
}

async function save() {
  error.value = "";
  notice.value = "";
  if (!readyToSave.value) {
    error.value = "请填写 API 根地址、授权令牌与模型名称。保存时需要重新输入授权令牌；本工具不会读取已有令牌。";
    return;
  }

  saving.value = true;
  const request = {
    baseUrl: draft.value.baseUrl,
    authToken: draft.value.authToken,
    model: draft.value.model,
  };
  // The reactive field is cleared before any native result is rendered. The
  // request object remains local to this function and is discarded afterwards.
  resetVisibleToken();
  try {
    const result = await applyClaudeCodeConfiguration(request);
    request.authToken = "";
    applyStatus(result);
    if (!result?.ok) {
      error.value = "未保存 Claude Code 用户设置。请检查填写内容与当前 settings.json 后重试。";
      return;
    }
    notice.value = result.message || "Claude Code 用户设置已安全保存。";
    emit("changed");
  } catch {
    request.authToken = "";
    error.value = "未保存 Claude Code 用户设置。请检查填写内容后重试。";
  } finally {
    request.authToken = "";
    saving.value = false;
  }
}

async function runBackupAction(action, backupID) {
  error.value = "";
  notice.value = "";
  actionID.value = `${action}:${backupID}`;
  try {
    const result = action === "restore"
      ? await restoreClaudeCodeConfiguration(backupID)
      : await deleteClaudeCodeConfigurationBackup(backupID);
    applyStatus(result, { updateDraft: action === "restore" });
    if (!result?.ok) {
      error.value = "Claude Code 用户设置备份操作未完成。请确认备份仍可安全验证。";
      return;
    }
    notice.value = result.message || (action === "restore" ? "已恢复用户设置备份。" : "已删除用户设置备份。");
    emit("changed");
  } catch {
    error.value = "Claude Code 用户设置备份操作未完成。";
  } finally {
    actionID.value = "";
  }
}

function confirmBackupAction(action, backupID) {
  const message = action === "restore"
    ? "将恢复这份 Claude Code 用户设置备份。当前 settings.json 会先自动备份；不会影响登录、OAuth、会话、MCP 或项目设置。是否继续？"
    : "确定删除这份 Claude Code 用户设置备份吗？该操作不可恢复。";
  if (!window.confirm(message)) return;
  void runBackupAction(action, backupID);
}

async function migrateLegacyBackup(backup) {
  if (!window.confirm("将这份旧版备份复制为新的 XIASS Tools 恢复点。不会恢复、修改或删除旧备份，是否继续？")) return;
  error.value = "";
  notice.value = "";
  actionID.value = `migrate:${backup.source}:${backup.id}`;
  try {
    const result = await migrateClaudeCodeLegacyBackup(backup.source, backup.id);
    applyStatus(result, { updateDraft: false });
    if (!result?.ok) {
      error.value = "旧版 Claude Code 用户设置备份未导入。该备份可能未通过安全校验。";
      return;
    }
    notice.value = result.message || "旧版备份已复制为新的恢复点。";
    emit("changed");
  } catch {
    error.value = "旧版 Claude Code 用户设置备份未导入。";
  } finally {
    actionID.value = "";
  }
}

function close() {
  if (busy.value) return;
  resetVisibleToken();
  emit("close");
}

watch(() => props.open, (open) => {
  if (open) {
    void refresh();
    return;
  }
  resetVisibleToken();
  error.value = "";
  notice.value = "";
});
</script>

<template>
  <Modal :open="open" title="配置 Claude Code" wide persistent :closable="!busy" @close="close">
    <div class="claude-config">
      <p class="intro">
        XIASS Tools 仅管理 Claude Code 用户 <code>settings.json</code> 中的 API 根地址、授权令牌和模型。
        不读取或管理登录、OAuth、账号额度、会话、MCP、项目配置或托管设置。
      </p>

      <div v-if="loading" class="state-block">正在读取本机 Claude Code 用户设置…</div>

      <template v-else>
        <section class="status-card" :class="{ invalid: !valid }">
          <div>
            <strong>{{ valid ? (snapshot.managed ? "XIASS Tools 已配置" : "已发现用户设置") : "用户设置需要处理" }}</strong>
            <span>{{ snapshot.exists ? "settings.json 已存在" : "尚未创建 settings.json" }}</span>
          </div>
          <span class="status-pill" :class="valid ? 'ok' : 'warn'">{{ valid ? "可安全管理" : "不可写入" }}</span>
        </section>

        <div v-if="snapshot.baseUrl || snapshot.model || snapshot.authTokenConfigured" class="current-state">
          <div v-if="snapshot.baseUrl"><span>当前 API 根地址</span><code>{{ snapshot.baseUrl }}</code></div>
          <div v-if="snapshot.model"><span>当前模型</span><code>{{ snapshot.model }}</code></div>
          <div><span>授权令牌</span><strong :class="snapshot.authTokenConfigured ? 'configured' : 'missing'">{{ snapshot.authTokenConfigured ? "已配置" : "未配置" }}</strong></div>
        </div>

        <p v-if="notice" class="notice" role="status">{{ notice }}</p>
        <p v-if="error" class="error" role="alert">{{ error }}</p>

        <section class="configuration-section" :aria-disabled="!valid">
          <div class="section-heading">
            <div>
              <strong>用户设置</strong>
              <span>使用明确的 API 根地址；不自动猜测或拼接请求路径。</span>
            </div>
          </div>

          <label class="field">
            <span>API 根地址</span>
            <input v-model="draft.baseUrl" autocomplete="url" inputmode="url" spellcheck="false" placeholder="https://api.example.com/v1" :disabled="!valid || busy" />
            <small>远程地址必须使用 HTTPS；本机 localhost 或回环地址可使用 HTTP。</small>
          </label>

          <label class="field">
            <span>授权令牌</span>
            <input v-model="draft.authToken" type="password" autocomplete="new-password" spellcheck="false" placeholder="仅用于此次安全保存" :disabled="!valid || busy" />
            <small>每次保存需要重新输入。不会显示、读取或写入到应用状态中。</small>
          </label>

          <label class="field">
            <span>模型</span>
            <input v-model="draft.model" autocomplete="off" spellcheck="false" placeholder="claude-sonnet-4-5" :disabled="!valid || busy" />
            <small>这是 Claude Code 的显式用户模型设置，不会远程拉取模型目录。</small>
          </label>
        </section>

        <details class="backup-section">
          <summary>可恢复备份 <span>{{ backups.length }}</span></summary>
          <p>备份只包含这一个用户 settings.json 的校验副本，不包含账号、OAuth、会话或项目数据。</p>
          <div v-if="!backups.length" class="empty-backups">尚无可恢复备份。</div>
          <article v-for="backup in backups" :key="backup.id" class="backup-row">
            <div>
              <strong>{{ backup.reason || "用户设置备份" }}</strong>
              <span>{{ new Date(backup.createdAt).toLocaleString() }}</span>
            </div>
            <div>
              <button type="button" :disabled="busy" @click="confirmBackupAction('restore', backup.id)">恢复</button>
              <button type="button" :disabled="busy" @click="confirmBackupAction('delete', backup.id)">删除</button>
            </div>
          </article>
        </details>

        <details v-if="legacyBackups.length || data?.legacyBackupWarning" class="backup-section legacy-section">
          <summary>旧版备份迁移 <span>{{ legacyBackups.length }}</span></summary>
          <p v-if="data?.legacyBackupWarning" class="warning-copy">{{ data.legacyBackupWarning }}</p>
          <p v-else>仅可复制已校验的旧版备份为新的恢复点；旧备份保持不变。</p>
          <article v-for="backup in legacyBackups" :key="`${backup.source}:${backup.id}`" class="backup-row">
            <div>
              <strong>{{ backup.reason || "旧版用户设置备份" }}</strong>
              <span>{{ new Date(backup.createdAt).toLocaleString() }}</span>
            </div>
            <div><button type="button" :disabled="busy" @click="migrateLegacyBackup(backup)">复制为恢复点</button></div>
          </article>
        </details>
      </template>
    </div>

    <template #footer>
      <Button variant="plain" :disabled="busy" @click="close">关闭</Button>
      <Button variant="filled" :disabled="!valid || !readyToSave || busy" :loading="saving" @click="save">保存用户设置</Button>
    </template>
  </Modal>
</template>

<style scoped>
.claude-config { display: grid; gap: 13px; }
.intro { margin: 0; color: var(--text-secondary); font-size: 12px; line-height: 1.65; }
.intro code, .current-state code { color: var(--accent-strong); font-family: var(--font-num); font-size: .95em; }
.state-block { min-height: 110px; display: grid; place-items: center; border: 1px dashed var(--separator-strong); border-radius: 10px; color: var(--text-tertiary); font-size: 12px; }
.status-card { display: flex; align-items: center; justify-content: space-between; gap: 12px; border: 1px solid color-mix(in srgb, var(--green) 42%, var(--separator)); border-left: 3px solid var(--green); border-radius: 10px; background: color-mix(in srgb, var(--green) 6%, var(--bg-inset)); padding: 11px 12px; }
.status-card.invalid { border-color: color-mix(in srgb, var(--orange) 52%, var(--separator)); border-left-color: var(--orange); background: color-mix(in srgb, var(--orange) 7%, var(--bg-inset)); }
.status-card > div { display: grid; gap: 3px; min-width: 0; }
.status-card strong { color: var(--text-primary); font-size: 13px; }
.status-card span { color: var(--text-tertiary); font-size: 11px; }
.status-pill { flex: 0 0 auto; border-radius: 999px; font-size: 10px; font-weight: 720; padding: 4px 7px; }
.status-pill.ok { background: color-mix(in srgb, var(--green) 15%, transparent); color: var(--green); }
.status-pill.warn { background: color-mix(in srgb, var(--orange) 16%, transparent); color: var(--orange); }
.current-state { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px; }
.current-state > div { display: grid; min-width: 0; gap: 3px; border: 1px solid var(--separator); border-radius: 8px; background: var(--bg-inset); padding: 8px 9px; }
.current-state span { color: var(--text-tertiary); font-size: 10px; }
.current-state code { overflow: hidden; color: var(--text-secondary); text-overflow: ellipsis; white-space: nowrap; }
.current-state strong { font-size: 11px; }
.configured { color: var(--green); }
.missing { color: var(--text-tertiary); }
.notice, .error { margin: 0; border-radius: 8px; font-size: 11px; line-height: 1.5; padding: 8px 10px; }
.notice { border: 1px solid color-mix(in srgb, var(--green) 35%, var(--separator)); background: color-mix(in srgb, var(--green) 7%, transparent); color: var(--green); }
.error { border: 1px solid color-mix(in srgb, var(--red) 42%, var(--separator)); background: color-mix(in srgb, var(--red) 8%, transparent); color: var(--red); }
.configuration-section { display: grid; gap: 10px; border-top: 1px solid var(--separator); padding-top: 12px; }
.section-heading { display: flex; justify-content: space-between; gap: 12px; }
.section-heading > div { display: grid; gap: 2px; }
.section-heading strong { color: var(--text-primary); font-size: 12px; }
.section-heading span { color: var(--text-tertiary); font-size: 10px; line-height: 1.45; }
.field { display: grid; gap: 5px; }
.field > span { color: var(--text-secondary); font-size: 11px; }
.field input { width: 100%; min-width: 0; border: 1px solid var(--separator-strong); border-radius: 8px; outline: none; background: var(--bg-base); color: var(--text-primary); font: inherit; font-family: var(--font-num); font-size: 12px; padding: 9px 10px; }
.field input:focus { border-color: var(--accent-strong); box-shadow: 0 0 0 3px var(--accent-soft); }
.field input:disabled { cursor: not-allowed; opacity: .58; }
.field small { color: var(--text-tertiary); font-size: 10px; line-height: 1.45; }
.backup-section { border-top: 1px solid var(--separator); padding-top: 12px; }
.backup-section summary { cursor: pointer; color: var(--text-primary); font-size: 12px; font-weight: 700; }
.backup-section summary span { display: inline-grid; place-items: center; min-width: 17px; min-height: 17px; border-radius: 999px; background: var(--bg-fill); color: var(--text-tertiary); font-family: var(--font-num); font-size: 10px; }
.backup-section p { margin: 8px 0 7px; color: var(--text-tertiary); font-size: 10px; line-height: 1.5; }
.warning-copy { color: var(--orange) !important; }
.empty-backups { border: 1px dashed var(--separator-strong); border-radius: 8px; color: var(--text-tertiary); font-size: 11px; padding: 9px 10px; }
.backup-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; border-top: 1px solid var(--separator); padding: 9px 0; }
.backup-row > div:first-child { display: grid; min-width: 0; gap: 2px; }
.backup-row strong { overflow: hidden; color: var(--text-secondary); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.backup-row span { color: var(--text-tertiary); font-size: 10px; }
.backup-row > div:last-child { display: flex; flex: 0 0 auto; gap: 6px; }
.backup-row button { border: 1px solid var(--separator); border-radius: 6px; color: var(--text-secondary); font: inherit; font-size: 10px; padding: 4px 7px; }
.backup-row button:hover:not(:disabled) { border-color: var(--accent-border); color: var(--accent-strong); }
.backup-row button:disabled { cursor: wait; opacity: .5; }
@media (max-width: 560px) { .current-state { grid-template-columns: 1fr; } .backup-row { align-items: flex-start; flex-direction: column; } .backup-row > div:last-child { align-self: stretch; } .backup-row button { flex: 1; } }
</style>
