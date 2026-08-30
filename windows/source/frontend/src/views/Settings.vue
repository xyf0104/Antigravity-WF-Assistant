<script setup>
import { computed, onMounted, ref } from "vue";
import Badge from "@/components/ui/Badge.vue";
import Button from "@/components/ui/Button.vue";
import Card from "@/components/ui/Card.vue";
import TOTPSettingsCard from "@/components/TOTPSettingsCard.vue";
import {
	state,
  cancelUpdateCheck,
  checkForUpdates,
  exportDiagnosticLogs,
  installLatestUpdate,
  loadSettings,
  saveSettings,
  skipUpdateVersion,
} from "@/state/appState";

const form = ref({
  streamRecovery: { enabled: true, maxAttempts: 2, maxDelaySeconds: 20 },
  updates: { autoCheck: true },
});
const message = ref("");
const error = ref("");
const diagnosticBusy = ref(false);
const diagnosticMessage = ref("");
const diagnosticError = ref("");

const updateInfo = computed(() => state.update.info || {});
const progressPercent = computed(() => Math.min(100, Math.max(0, Number(state.update.progress?.percent) || 0)));
const updateTone = computed(() => {
	if (state.update.checking) return "warn";
  if (state.update.installing) return "warn";
  if (updateInfo.value.available && !updateInfo.value.skipped) return "ok";
  return "neutral";
});
const updateLabel = computed(() => {
	if (state.update.checking) return "正在检查";
  if (state.update.installing) return "正在更新";
  if (updateInfo.value.available && !updateInfo.value.skipped) return `发现 v${updateInfo.value.latestVersion}`;
  if (updateInfo.value.skipped) return `已跳过 v${updateInfo.value.latestVersion}`;
  return updateInfo.value.currentVersion ? "已是最新" : "尚未检查";
});

function syncForm() {
  const settings = state.settings || {};
  form.value = {
    streamRecovery: {
      enabled: settings.streamRecovery?.enabled !== false,
      maxAttempts: Number(settings.streamRecovery?.maxAttempts) || 2,
      maxDelaySeconds: Number(settings.streamRecovery?.maxDelaySeconds) || 20,
    },
    updates: { autoCheck: settings.updates?.autoCheck !== false },
  };
}

function clamp(value, min, max, fallback) {
  const number = Math.round(Number(value));
  return Number.isFinite(number) ? Math.min(max, Math.max(min, number)) : fallback;
}

async function handleSave() {
  error.value = "";
  message.value = "";
  const next = {
    ...state.settings,
    streamRecovery: {
      enabled: Boolean(form.value.streamRecovery.enabled),
      maxAttempts: clamp(form.value.streamRecovery.maxAttempts, 1, 10, 2),
      maxDelaySeconds: clamp(form.value.streamRecovery.maxDelaySeconds, 1, 120, 20),
    },
    updates: {
      ...state.settings?.updates,
      autoCheck: Boolean(form.value.updates.autoCheck),
    },
  };
  const res = await saveSettings(next);
  if (res?.ok) {
    message.value = res.message || "设置已保存";
    syncForm();
  } else {
    error.value = res?.message || "保存失败";
  }
}

async function handleCheck() {
  error.value = "";
	message.value = "";
  const res = await checkForUpdates();
  if (!res?.ok) error.value = res?.message || "检查更新失败";
}

async function handleCancelCheck() {
	error.value = "";
	const res = await cancelUpdateCheck();
	if (res?.ok) message.value = res.message || "已取消检查更新";
	else error.value = res?.message || "无法取消检查更新";
}

async function handleSkip() {
  const version = updateInfo.value.latestVersion;
  if (!version) return;
  const res = await skipUpdateVersion(version);
  if (res?.ok) message.value = res.message || `已跳过 v${version}`;
  else error.value = res?.message || "无法跳过该版本";
}

async function handleInstall() {
  error.value = "";
  const res = await installLatestUpdate();
  if (!res?.ok) error.value = res?.message || "无法启动更新";
}

async function handleExportDiagnostics() {
  diagnosticBusy.value = true;
  diagnosticMessage.value = "";
  diagnosticError.value = "";
  try {
    const res = await exportDiagnosticLogs();
    if (res?.ok) diagnosticMessage.value = res.message || "诊断日志已导出";
    else diagnosticError.value = res?.message || "导出诊断日志失败";
  } catch (e) {
    diagnosticError.value = e?.message || "导出诊断日志失败";
  } finally {
    diagnosticBusy.value = false;
  }
}

function formatBytes(value) {
  const bytes = Number(value) || 0;
  if (!bytes) return "";
  if (bytes < 1024 * 1024) return `${Math.ceil(bytes / 1024)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

function formatCheckedAt(value) {
	const timestamp = Date.parse(value || "");
	if (!Number.isFinite(timestamp)) return "—";
	return new Intl.DateTimeFormat("zh-CN", {
		month: "numeric", day: "numeric", hour: "2-digit", minute: "2-digit",
	}).format(timestamp);
}

onMounted(async () => {
  await loadSettings();
  syncForm();
});
</script>

<template>
  <div class="page fade-up">
    <Card title="Antigravity WF · 流式稳定性与防重复">
      <template #action>
        <Badge :tone="form.streamRecovery.enabled ? 'ok' : 'neutral'" :label="form.streamRecovery.enabled ? '安全重试已开启' : '自动重试已关闭'" />
      </template>

      <div class="col" style="gap:14px">
        <label class="switch-row">
          <div class="grow">
            <div class="t-headline">首个回复前安全重试</div>
            <div class="t-caption">仅当上游尚未交付任何事件时，才会固定当前账户安全重试。已有文字、工具调用或附件后绝不重放请求。</div>
          </div>
          <input v-model="form.streamRecovery.enabled" type="checkbox" class="switch-input" />
          <span class="switch"></span>
        </label>

        <div class="recovery-grid" :class="{ disabled: !form.streamRecovery.enabled }">
          <label class="number-field">
            <span class="t-footnote">最多重试次数</span>
            <input v-model.number="form.streamRecovery.maxAttempts" :disabled="!form.streamRecovery.enabled" type="number" min="1" max="10" />
          </label>
          <label class="number-field">
            <span class="t-footnote">单次最长等待（秒）</span>
            <input v-model.number="form.streamRecovery.maxDelaySeconds" :disabled="!form.streamRecovery.enabled" type="number" min="1" max="120" />
          </label>
        </div>

        <div class="note-box">
          正常对话会由 Antigravity 提供必要的会话上下文，因为上游 API 本身是无状态的；XIASS Tools 不会再额外拼接“已输出回答”和“继续回答”指令。支持的上游会使用提示词缓存，不支持的上游只探测一次，随后自动关闭该缓存字段。
        </div>
      </div>
    </Card>

    <TOTPSettingsCard />

    <Card title="软件更新">
      <template #action>
        <Badge :tone="updateTone" :label="updateLabel" />
      </template>

      <div class="col" style="gap:14px">
        <label class="switch-row">
          <div class="grow">
            <div class="t-headline">启动时检查更新</div>
			<div class="t-caption">仅查询本工具的公开 GitHub Release；最近 10 分钟的结果会立即显示，过期后检查最多 5 秒，可随时取消，安装前会校验 SHA256 文件清单。</div>
          </div>
          <input v-model="form.updates.autoCheck" type="checkbox" class="switch-input" />
          <span class="switch"></span>
        </label>

        <div class="update-info">
          <div class="info-row"><span>当前版本</span><strong>v{{ updateInfo.currentVersion || '—' }}</strong></div>
          <div v-if="updateInfo.latestVersion" class="info-row"><span>最新版本</span><strong>v{{ updateInfo.latestVersion }}</strong></div>
          <div v-if="updateInfo.assetName" class="info-row"><span>安装包</span><span class="mono truncate">{{ updateInfo.assetName }} {{ formatBytes(updateInfo.assetSize) ? `· ${formatBytes(updateInfo.assetSize)}` : '' }}</span></div>
			<div v-if="updateInfo.checkedAt" class="info-row"><span>上次检查</span><span>{{ formatCheckedAt(updateInfo.checkedAt) }}</span></div>
        </div>
		<div v-if="updateInfo.cached" class="note-box">
			<template v-if="updateInfo.cacheReason === 'fresh'">正在显示最近缓存的更新结果；无需重复等待网络。下载并安装前仍会重新校验版本与 SHA256。</template>
			<template v-else-if="updateInfo.cacheReason === 'timeout'">本次 GitHub 检查已超时，正在显示上一次确认到的更新结果；下载并安装前仍会重新校验版本与 SHA256。</template>
			<template v-else>当前无法连接 GitHub，正在显示上一次确认到的更新结果；下载并安装前仍会重新校验版本与 SHA256。</template>
		</div>

        <div v-if="state.update.installing || state.update.progress?.phase === 'downloading'" class="progress-wrap">
          <div class="progress-meta"><span>{{ state.update.progress?.message || '正在处理更新' }}</span><strong>{{ progressPercent }}%</strong></div>
          <div class="progress-track"><div class="progress-bar" :style="{ width: `${progressPercent}%` }"></div></div>
        </div>

        <div v-if="state.update.message" class="t-caption">{{ state.update.message }}</div>
        <div v-if="error" class="result-box error">{{ error }}</div>
        <div v-if="message" class="result-box success">{{ message }}</div>

        <div class="row between" style="gap:8px; flex-wrap:wrap">
		  <Button v-if="state.update.checking" variant="plain" @click="handleCancelCheck">取消检查</Button>
		  <Button v-else variant="plain" :disabled="state.update.installing" @click="handleCheck">检查更新</Button>
          <div class="row" style="gap:8px">
            <Button v-if="updateInfo.available && !updateInfo.skipped" variant="plain" :disabled="state.update.installing" @click="handleSkip">跳过此版本</Button>
            <Button v-if="updateInfo.available" variant="filled" :loading="state.update.installing" :disabled="state.update.installing" @click="handleInstall">下载并安装</Button>
          </div>
        </div>
      </div>
    </Card>

    <Card title="诊断与日志">
      <template #action>
        <Badge tone="neutral" label="隐私脱敏" />
      </template>

      <div class="col" style="gap:14px">
        <div class="note-box diagnostic-note">
          遇到补丁、模型注入、对话或生图异常时，可导出最近一次运行的诊断包交给技术支持。导出前会自动隐藏 API Key、Token、OAuth 授权码、Cookie、用户目录和图片数据；不会主动打包账户文件、模型配置文件或聊天历史文件。
        </div>
        <div class="diagnostic-list t-caption">
          <span>包含 XIASS Tools 运行日志与代理事件</span>
          <span>包含最近一次 Antigravity 运行日志</span>
          <span>单文件自动截断，避免诊断包过大</span>
        </div>
        <div v-if="diagnosticError" class="result-box error">{{ diagnosticError }}</div>
        <div v-if="diagnosticMessage" class="result-box success">{{ diagnosticMessage }}</div>
        <div class="row end">
          <Button variant="filled" :loading="diagnosticBusy" :disabled="diagnosticBusy" @click="handleExportDiagnostics">导出诊断日志</Button>
        </div>
      </div>
    </Card>

    <div class="row end">
      <Button variant="filled" :loading="state.settingsBusy" :disabled="state.settingsBusy" @click="handleSave">保存设置</Button>
    </div>
  </div>
</template>

<style scoped>
.page { display:flex; flex-direction:column; gap:14px; padding:18px 20px 28px; height:100%; overflow-y:auto; }
.page > * { flex-shrink:0; }
.switch-row { display:flex; align-items:center; gap:12px; cursor:pointer; }
.switch-input { position:absolute; opacity:0; pointer-events:none; }
.switch { width:44px; height:26px; border-radius:999px; background:rgba(120,120,128,.32); position:relative; transition:.2s var(--ease); flex:none; }
.switch::after { content:""; position:absolute; width:22px; height:22px; border-radius:50%; background:#fff; left:2px; top:2px; box-shadow:0 2px 6px rgba(0,0,0,.35); transition:.22s var(--spring); }
.switch-input:checked + .switch { background:var(--green); }
.switch-input:checked + .switch::after { transform:translateX(18px); }
.recovery-grid { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:10px; }
.recovery-grid.disabled { opacity:.45; }
.number-field { display:flex; flex-direction:column; gap:6px; }
.number-field input { height:34px; width:100%; padding:0 10px; border:.5px solid var(--separator-strong); border-radius:var(--r-sm); background:var(--bg-inset); color:var(--text-primary); font:13px var(--font-num); }
.number-field input:focus { border-color:var(--accent); box-shadow:0 0 0 3px var(--accent-soft); }
.note-box { padding:11px 12px; border-radius:var(--r-sm); color:var(--text-secondary); background:var(--accent-soft); border:.5px solid var(--accent-border); font-size:12px; line-height:1.6; }
.diagnostic-note { background:var(--bg-inset); border-color:var(--separator); }
.diagnostic-list { display:grid; grid-template-columns:repeat(3,minmax(0,1fr)); gap:8px; }
.diagnostic-list span { padding:9px 10px; border:.5px solid var(--separator); border-radius:var(--r-sm); background:var(--bg-card); color:var(--text-secondary); line-height:1.5; }
.update-info { border:1px solid var(--separator); border-radius:var(--r-md); overflow:hidden; }
.info-row { display:flex; align-items:center; justify-content:space-between; gap:14px; min-width:0; padding:10px 12px; background:var(--bg-card); font-size:12px; }
.info-row + .info-row { border-top:.5px solid var(--separator); }
.info-row > span:first-child { color:var(--text-tertiary); flex:none; }
.info-row strong { color:var(--text-primary); font-family:var(--font-num); font-weight:600; }
.progress-wrap { display:flex; flex-direction:column; gap:7px; }
.progress-meta { display:flex; justify-content:space-between; gap:10px; color:var(--text-secondary); font-size:12px; }
.progress-track { height:7px; overflow:hidden; border-radius:999px; background:var(--bg-fill); }
.progress-bar { height:100%; border-radius:inherit; background:linear-gradient(90deg,var(--accent),var(--accent-hover)); transition:width .2s var(--ease); }
.result-box { padding:9px 11px; border-radius:var(--r-sm); white-space:pre-wrap; font-size:12px; }
.result-box.error { color:var(--red); background:rgba(255,69,58,.1); }
.result-box.success { color:var(--green); background:rgba(48,209,88,.1); }
.end { justify-content:flex-end; }
@media (max-width:620px) { .recovery-grid, .diagnostic-list { grid-template-columns:1fr; } }
</style>
