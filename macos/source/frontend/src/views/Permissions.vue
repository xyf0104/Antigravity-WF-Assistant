<script setup>
import { computed, onMounted, ref } from "vue";
import Badge from "@/components/ui/Badge.vue";
import Button from "@/components/ui/Button.vue";
import Card from "@/components/ui/Card.vue";
import { state, loadAutoApproval, saveAutoApproval } from "@/state/appState";

const enabled = ref(false);
const mode = ref("development");
const customText = ref("");
const message = ref("");
const error = ref("");

const modes = [
  { value: "development", title: "常用开发命令", detail: "自动批准 git、Go、Node、Python、Wails 等命令。" },
  { value: "custom", title: "指定命令规则", detail: "仅批准你输入的 command(...) 规则。" },
  { value: "all", title: "所有命令", detail: "写入 command(*)。风险最高，仅适用于完全受信任的工作区。" },
];

const managedSummary = computed(() => {
  const count = state.autoApproval.managedGrants?.length || 0;
  return count ? `当前由 XIASS Tools 管理 ${count} 条规则` : "当前没有 XIASS Tools 管理的规则";
});

function syncForm() {
  enabled.value = Boolean(state.autoApproval.enabled);
  mode.value = state.autoApproval.mode || "development";
  customText.value = (state.autoApproval.customRules || []).join("\n");
}

async function handleSave() {
  error.value = "";
  message.value = "";
  const customRules = customText.value.split(/\r?\n/).map((line) => line.trim()).filter(Boolean);
  const res = await saveAutoApproval({ enabled: enabled.value, mode: mode.value, customRules });
  if (res?.ok) {
    message.value = res.message || "设置已保存";
    syncForm();
  } else {
    error.value = res?.message || "保存失败";
  }
}

async function handleDisable() {
  enabled.value = false;
  await handleSave();
}

onMounted(async () => {
  await loadAutoApproval();
  syncForm();
});
</script>

<template>
  <div class="page fade-up">
    <div class="warning-banner">
      <div class="warning-icon">!</div>
      <div class="grow">
        <div class="t-headline">命令自动批准会降低执行前确认保护</div>
        <div class="t-caption">配置写入 Antigravity 官方 globalPermissionGrants。XIASS Tools 只移除自己添加的规则，不触碰已有授权。</div>
      </div>
    </div>

    <Card title="Agent Window 命令权限">
      <template #action>
        <Badge :tone="state.autoApproval.enabled ? 'ok' : 'warn'" :label="state.autoApproval.enabled ? '已启用' : '已关闭'" />
      </template>

      <div class="col" style="gap:14px">
        <label class="switch-row">
          <div class="grow">
            <div class="t-headline">启用自动批准</div>
            <div class="t-caption">保存后需要重新打开 Agent Window 才能让 Language Server 重新载入配置。</div>
          </div>
          <input v-model="enabled" type="checkbox" class="switch-input" />
          <span class="switch"></span>
        </label>

        <div class="mode-list" :class="{ disabled: !enabled }">
          <label v-for="item in modes" :key="item.value" class="mode-row" :class="{ selected: mode === item.value }">
            <input v-model="mode" type="radio" :value="item.value" :disabled="!enabled" />
            <div class="radio-dot"></div>
            <div class="grow">
              <div class="t-headline">{{ item.title }}</div>
              <div class="t-caption">{{ item.detail }}</div>
            </div>
          </label>
        </div>

        <div v-if="mode === 'custom' && enabled" class="custom-rules">
          <div class="t-footnote">每行一条规则</div>
          <textarea v-model="customText" rows="6" aria-label="自定义命令规则" spellcheck="false" placeholder="command(go test *)&#10;command(npm run build)&#10;command(python *)"></textarea>
          <div class="t-caption">规则必须是 command(...) 格式。匹配语义由 Antigravity Language Server 决定。</div>
        </div>

        <div v-if="mode === 'all' && enabled" class="danger-box">
          <strong>高风险：</strong>任何 Agent 生成的终端命令都可能无需再次确认执行。仅在工作区内容和指令来源完全可信时使用。
        </div>

        <div v-if="error" class="result-box error" role="alert">{{ error }}</div>
        <div v-if="message" class="result-box success" role="status" aria-live="polite">{{ message }}</div>

        <div class="row between" style="gap:10px;flex-wrap:wrap">
          <div class="t-caption">{{ managedSummary }}</div>
          <div class="row" style="gap:8px">
            <Button v-if="state.autoApproval.enabled" variant="plain" :disabled="state.autoApprovalBusy" @click="handleDisable">关闭并清理</Button>
            <Button variant="filled" :loading="state.autoApprovalBusy" :disabled="state.autoApprovalBusy" @click="handleSave">保存权限设置</Button>
          </div>
        </div>
      </div>
    </Card>

    <Card title="配置与恢复">
      <div class="path-row">
        <span class="t-footnote">配置</span>
        <span class="mono truncate">{{ state.autoApproval.configPath || '—' }}</span>
      </div>
      <div class="path-row">
        <span class="t-footnote">启用前备份</span>
        <span class="mono truncate">{{ state.autoApproval.backupPath || '启用后创建' }}</span>
      </div>
    </Card>
  </div>
</template>

<style scoped>
.page { display:flex; flex-direction:column; gap:14px; padding:18px 20px 28px; height:100%; overflow-y:auto; }
.page > * { flex-shrink:0; }
.warning-banner { display:flex; gap:12px; align-items:center; padding:13px 15px; border-radius:var(--r-lg); background:rgba(255,159,10,.09); border:1px solid rgba(255,159,10,.28); box-shadow:var(--shadow-card); }
.warning-icon { width:28px; height:28px; border-radius:50%; display:grid; place-items:center; flex:none; color:#111; background:var(--orange); font-weight:800; }
.switch-row { display:flex; align-items:center; gap:12px; cursor:pointer; }
.switch-input { position:absolute; opacity:0; pointer-events:none; }
.switch { width:44px; height:26px; border-radius:999px; background:rgba(120,120,128,.32); position:relative; transition:.2s var(--ease); flex:none; }
.switch::after { content:""; position:absolute; width:22px; height:22px; border-radius:50%; background:#fff; left:2px; top:2px; box-shadow:0 2px 6px rgba(0,0,0,.35); transition:.22s var(--spring); }
.switch-input:checked + .switch { background:var(--green); }
.switch-input:checked + .switch::after { transform:translateX(18px); }
.mode-list { border:1px solid var(--separator); border-radius:var(--r-md); overflow:hidden; }
.mode-list.disabled { opacity:.45; }
.mode-row { display:flex; align-items:center; gap:11px; padding:11px 13px; cursor:pointer; background:var(--bg-card); position:relative; }
.mode-row + .mode-row { border-top:.5px solid var(--separator); }
.mode-row.selected { background:var(--accent-soft); }
.mode-row input { position:absolute; opacity:0; }
.radio-dot { width:18px; height:18px; border-radius:50%; border:1.5px solid var(--text-tertiary); position:relative; flex:none; }
.mode-row.selected .radio-dot { border-color:var(--accent); }
.mode-row.selected .radio-dot::after { content:""; position:absolute; inset:3px; border-radius:50%; background:var(--accent); }
.custom-rules { display:flex; flex-direction:column; gap:7px; }
textarea { width:100%; resize:vertical; border:1px solid var(--separator-strong); border-radius:var(--r-sm); background:var(--bg-inset); padding:10px 12px; color:var(--text-primary); font-family:var(--font-num); font-size:11.5px; }
textarea:focus { border-color:var(--accent); box-shadow:0 0 0 3px var(--accent-soft); }
.danger-box { padding:10px 12px; border-radius:var(--r-sm); color:#ffb4ae; background:rgba(255,69,58,.1); border:.5px solid rgba(255,69,58,.3); font-size:12px; }
.result-box { padding:9px 11px; border-radius:var(--r-sm); white-space:pre-wrap; font-size:12px; }
.result-box.error { color:var(--red); background:rgba(255,69,58,.1); }
.result-box.success { color:var(--green); background:rgba(48,209,88,.1); }
.path-row { display:flex; align-items:center; gap:10px; min-width:0; padding:6px 0; }
.path-row + .path-row { border-top:.5px solid var(--separator); }
.path-row .t-footnote { width:64px; flex:none; }
.path-row .mono { color:var(--text-secondary); }
</style>
