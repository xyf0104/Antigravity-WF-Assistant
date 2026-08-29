<script setup>
import { computed, ref, watch } from "vue";
import Modal from "@/components/ui/Modal.vue";
import Button from "@/components/ui/Button.vue";
import { applyMCPConfiguration, getMCPConfiguration } from "@/state/appState";

const props = defineProps({
  open: Boolean,
  target: { type: String, default: "" },
});
const emit = defineEmits(["close", "changed"]);

const data = ref(null);
const remoteURL = ref("");
const loading = ref(false);
const saving = ref(false);
const error = ref("");
const notice = ref("");

const isCursor = computed(() => props.target === "cursor");
const displayName = computed(() => isCursor.value ? "Cursor" : "Windsurf");
const snapshot = computed(() => data.value?.snapshot || {});
const busy = computed(() => loading.value || saving.value);
const canApply = computed(() => Boolean(data.value?.canApply && remoteURL.value.trim()));

const clientState = computed(() => data.value?.clientDetected ? "已确认" : "未确认");
const configState = computed(() => {
  if (!snapshot.value.valid) return "需要处理";
  if (snapshot.value.hasSensitiveConfiguration) return "只读保护";
  return snapshot.value.exists ? "已验证" : "准备创建";
});
const managedState = computed(() => snapshot.value.managedServerConfigured ? "已配置" : "未配置");

function clearEndpoint() {
  remoteURL.value = "";
}

function applyStatus(result) {
  data.value = result || null;
}

async function refresh() {
  if (!props.target) return;
  loading.value = true;
  error.value = "";
  notice.value = "";
  try {
    const result = await getMCPConfiguration(props.target);
    applyStatus(result);
    if (!result?.ok) {
      error.value = result?.message || "无法读取本机 MCP 设置状态。";
      return;
    }
    notice.value = result.message || "已完成本机 MCP 设置检查。";
  } catch {
    data.value = null;
    clearEndpoint();
    error.value = "无法读取本机 MCP 设置状态。";
  } finally {
    loading.value = false;
  }
}

async function save() {
  error.value = "";
  notice.value = "";
  if (!canApply.value) {
    error.value = "请先确认客户端已安装、现有 MCP 设置可安全管理，并填写远程地址。";
    return;
  }
  saving.value = true;
  const request = { target: props.target, remoteUrl: remoteURL.value };
  // The endpoint is never retained in the shared app state. Clear the visible
  // draft before rendering any native result, then release the local request.
  clearEndpoint();
  try {
    const result = await applyMCPConfiguration(request);
    request.remoteUrl = "";
    applyStatus(result);
    if (!result?.ok) {
      error.value = result?.message || "未能安全保存 MCP 远程连接。";
      return;
    }
    notice.value = result.message || "MCP 远程连接已安全保存。";
    emit("changed");
  } catch {
    request.remoteUrl = "";
    error.value = "未能安全保存 MCP 远程连接。";
  } finally {
    request.remoteUrl = "";
    saving.value = false;
  }
}

function close() {
  if (busy.value) return;
  clearEndpoint();
  emit("close");
}

watch(() => [props.open, props.target], ([open, target]) => {
  clearEndpoint();
  error.value = "";
  notice.value = "";
  if (open && target) {
    void refresh();
    return;
  }
  if (!open) data.value = null;
});
</script>

<template>
  <Modal :open="open" :title="`配置 ${displayName} MCP`" wide persistent :closable="!busy" @close="close">
    <div class="mcp-config">
      <p class="intro">
        XIASS Tools 只管理一个保留的远程 MCP 条目，不读取或改写账号、Cookie、令牌、聊天记录、数据库或其他 MCP 条目。
      </p>

      <div v-if="loading" class="state-block">正在检查本机 {{ displayName }} 与全局 MCP 设置…</div>

      <template v-else>
        <section class="status-card" :class="{ guarded: snapshot.hasSensitiveConfiguration || !snapshot.valid }">
          <div>
            <strong>{{ data?.canApply ? "可以安全配置" : "暂不可写入" }}</strong>
            <span>{{ data?.message || "等待本机检查" }}</span>
          </div>
          <span class="status-pill" :class="data?.canApply ? 'ok' : 'warn'">{{ data?.canApply ? "已验证" : "受保护" }}</span>
        </section>

        <dl class="facts" aria-label="MCP 本机状态">
          <div><dt>客户端</dt><dd :class="data?.clientDetected ? 'ok' : 'muted'">{{ clientState }}</dd></div>
          <div><dt>全局 MCP 设置</dt><dd :class="snapshot.valid && !snapshot.hasSensitiveConfiguration ? 'ok' : 'warn'">{{ configState }}</dd></div>
          <div><dt>XIASS 条目</dt><dd :class="snapshot.managedServerConfigured ? 'ok' : 'muted'">{{ managedState }}</dd></div>
        </dl>

        <p v-if="notice" class="notice" role="status">{{ notice }}</p>
        <p v-if="error" class="error" role="alert">{{ error }}</p>

        <section class="configuration-section" :aria-disabled="!data?.canApply">
          <div class="section-heading">
            <div>
              <strong>远程 MCP 地址</strong>
              <span>只支持 HTTPS，或无凭据的本机 localhost/回环 HTTP 地址。</span>
            </div>
          </div>
          <label class="field">
            <span>远程地址</span>
            <input
              v-model="remoteURL"
              autocomplete="url"
              inputmode="url"
              spellcheck="false"
              placeholder="https://mcp.example.com/endpoint"
              :disabled="!data?.canApply || busy"
            />
            <small>地址只用于这一次保存，不会在工具中心、日志或诊断包中回显。</small>
          </label>
        </section>

        <details class="safety-note">
          <summary>安全边界</summary>
          <p>如果现有全局 MCP 设置含有环境变量、请求头、认证信息或其他敏感字段，XIASS Tools 会保持它不变。请在客户端内自行管理该配置。</p>
        </details>
      </template>
    </div>

    <template #footer>
      <Button variant="plain" :disabled="busy" @click="close">关闭</Button>
      <Button variant="filled" :disabled="!canApply || busy" :loading="saving" @click="save">保存 MCP 连接</Button>
    </template>
  </Modal>
</template>

<style scoped>
.mcp-config { display: grid; gap: 13px; }
.intro { margin: 0; color: var(--text-secondary); font-size: 12px; line-height: 1.65; }
.state-block { min-height: 112px; display: grid; place-items: center; border: 1px dashed var(--separator-strong); border-radius: 10px; color: var(--text-tertiary); font-size: 12px; }
.status-card { display: flex; align-items: center; justify-content: space-between; gap: 12px; border: 1px solid color-mix(in srgb, var(--green) 42%, var(--separator)); border-left: 3px solid var(--green); border-radius: 10px; background: color-mix(in srgb, var(--green) 6%, var(--bg-inset)); padding: 11px 12px; }
.status-card.guarded { border-color: color-mix(in srgb, var(--orange) 52%, var(--separator)); border-left-color: var(--orange); background: color-mix(in srgb, var(--orange) 7%, var(--bg-inset)); }
.status-card > div { display: grid; min-width: 0; gap: 3px; }
.status-card strong { color: var(--text-primary); font-size: 13px; }
.status-card span { color: var(--text-tertiary); font-size: 11px; line-height: 1.45; }
.status-pill { flex: 0 0 auto; border-radius: 999px; font-size: 10px; font-weight: 720; padding: 4px 7px; }
.status-pill.ok { background: color-mix(in srgb, var(--green) 15%, transparent); color: var(--green); }
.status-pill.warn { background: color-mix(in srgb, var(--orange) 16%, transparent); color: var(--orange); }
.facts { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px; margin: 0; }
.facts > div { display: grid; min-width: 0; gap: 3px; border: 1px solid var(--separator); border-radius: 8px; background: var(--bg-inset); padding: 8px 9px; }
.facts dt { color: var(--text-tertiary); font-size: 10px; }
.facts dd { margin: 0; font-family: var(--font-num); font-size: 11px; font-weight: 700; }
.facts dd.ok { color: var(--green); }.facts dd.warn { color: var(--orange); }.facts dd.muted { color: var(--text-tertiary); }
.notice, .error { margin: 0; border-radius: 8px; font-size: 11px; line-height: 1.5; padding: 8px 10px; }
.notice { border: 1px solid color-mix(in srgb, var(--green) 35%, var(--separator)); background: color-mix(in srgb, var(--green) 7%, transparent); color: var(--green); }
.error { border: 1px solid color-mix(in srgb, var(--red) 42%, var(--separator)); background: color-mix(in srgb, var(--red) 8%, transparent); color: var(--red); }
.configuration-section { display: grid; gap: 10px; border-top: 1px solid var(--separator); padding-top: 12px; }
.section-heading { display: flex; justify-content: space-between; gap: 12px; }.section-heading > div { display: grid; gap: 2px; }
.section-heading strong { color: var(--text-primary); font-size: 12px; }.section-heading span { color: var(--text-tertiary); font-size: 10px; line-height: 1.45; }
.field { display: grid; gap: 5px; }.field > span { color: var(--text-secondary); font-size: 11px; }
.field input { width: 100%; min-width: 0; border: 1px solid var(--separator-strong); border-radius: 8px; outline: none; background: var(--bg-base); color: var(--text-primary); font: inherit; font-family: var(--font-num); font-size: 12px; padding: 9px 10px; }
.field input:focus { border-color: var(--accent-strong); box-shadow: 0 0 0 3px var(--accent-soft); }.field input:disabled { cursor: not-allowed; opacity: .58; }
.field small { color: var(--text-tertiary); font-size: 10px; line-height: 1.45; }
.safety-note { border-top: 1px solid var(--separator); padding-top: 12px; }.safety-note summary { cursor: pointer; color: var(--text-primary); font-size: 12px; font-weight: 700; }.safety-note p { margin: 8px 0 0; color: var(--text-tertiary); font-size: 10px; line-height: 1.5; }
@media (max-width: 560px) { .facts { grid-template-columns: 1fr; } }
</style>
