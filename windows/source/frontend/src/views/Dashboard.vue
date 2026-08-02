<script setup>
import { computed, onMounted, onUnmounted, ref } from "vue";
import Badge from "@/components/ui/Badge.vue";
import Button from "@/components/ui/Button.vue";
import Card from "@/components/ui/Card.vue";
import {
  state,
  statusTone,
  statusLabel,
  cacheHitPct,
  formatK,
  loadStats,
  loadPatchStatus,
  applyPatch,
  applyIDEPatch,
  restorePatch,
  startProxy,
  stopProxy,
  loadHistorySync,
  syncHistoryNow,
  launchOrRestartAntigravity,
} from "@/state/appState";

const patchError = ref("");
let pollTimer = null;

async function handleApply() {
  patchError.value = "";
  const res = await applyPatch();
  if (!res?.ok) patchError.value = res?.message || "补丁失败";
}

async function handleApplyIDE() {
  patchError.value = "";
  const res = await applyIDEPatch();
  if (!res?.ok) patchError.value = res?.message || "IDE 补丁失败";
}

async function handleRestore() {
  patchError.value = "";
  const res = await restorePatch();
  if (!res?.ok) patchError.value = res?.message || "恢复失败";
}

async function handleToggleProxy() {
  patchError.value = "";
  let res;
  if (state.proxyRunning) {
    res = await stopProxy();
  } else {
    res = await startProxy();
  }
  if (!res?.ok) patchError.value = res?.message || "代理操作失败";
}

async function handleHistorySync() {
  await syncHistoryNow();
}

async function handleAntigravityAction(target) {
  patchError.value = "";
  const res = await launchOrRestartAntigravity(target.appPath);
  if (!res?.ok) patchError.value = res?.message || "Antigravity 操作失败";
}

const cacheRing = computed(() => {
  const rate = state.stats.cacheHitRate;
  if (!Number.isFinite(rate) || rate === 0) return "stroke-dasharray: 0 100";
  const pct = Math.min(100, Math.max(0, rate * 100));
  return `stroke-dasharray: ${pct} 100`;
});

const hasSeparateAgentAndIDE = computed(() => {
	const kinds = new Set((state.patch.targets || []).map((target) => target.kind));
	return kinds.has("agent") && kinds.has("ide");
});

const launchTargets = computed(() =>
  (state.patch.targets || []).filter((target) => target.launchable !== false)
);

const proxyDiagnostic = computed(() => {
  const configured = state.models.length;
  if (!state.patch.proxyListening) {
    return { tone: "err", text: "本地代理尚未监听，Antigravity 暂时无法读取自定义模型。" };
  }
  if (!state.patch.proxyManaged) {
    return { tone: "warn", text: "端口 50999 正由其他进程占用，请先关闭旧版助手后再试。" };
  }
  if (state.patch.lastError) {
    const details = [
      state.patch.lastModelStatusCode ? `HTTP ${state.patch.lastModelStatusCode}` : "",
      state.patch.lastModelEncoding ? `编码 ${state.patch.lastModelEncoding}` : "",
    ].filter(Boolean).join("，");
    return { tone: "err", text: `最近模型注入失败${details ? `（${details}）` : ""}：${state.patch.lastError}` };
  }
  if (state.patch.lastModelInjectionAt) {
    const injected = state.patch.lastInjectedModelCount || 0;
    return {
      tone: injected >= configured && configured > 0 ? "ok" : "warn",
      text: configured > 0
        ? `最近一次模型列表请求已注入 ${injected}/${configured} 个自定义模型。`
        : "模型列表请求已到达本地代理；当前还没有配置自定义模型。",
    };
  }
  return { tone: "neutral", text: "助手已就绪，等待 Antigravity 发起模型列表请求。" };
});

const historyTone = computed(() => {
  if (state.historySync.state === "success") return "ok";
  if (state.historySync.state === "error") return "err";
  return "warn";
});

const historyLabel = computed(() => {
  if (state.historySync.state === "success") return "已自动恢复";
  if (state.historySync.state === "error") return "恢复失败";
  if (state.historySync.state === "running") return "正在恢复";
  return "等待恢复";
});

function targetKindLabel(kind) {
	return kind === "agent" ? "Agent / 2.x" : "IDE";
}

onMounted(() => {
  pollTimer = setInterval(() => {
    loadPatchStatus().catch(() => {});
    loadStats().catch(() => {});
    loadHistorySync().catch(() => {});
  }, 15000);
});

onUnmounted(() => {
  clearInterval(pollTimer);
});
</script>

<template>
  <div class="page fade-up">
    <!-- ── 顶部状态横幅 ── -->
    <div class="banner" :class="`tone-${statusTone}`">
      <div class="row grow" style="gap: 10px">
        <span class="dot-big" :class="`dot-${statusTone}`"></span>
        <div class="col" style="gap: 1px">
          <div class="t-headline">{{ statusLabel }}</div>
          <div class="t-caption" style="color: inherit; opacity: 0.7">
            代理端口 50999 ·
            {{ state.patch.proxyManaged ? "当前 WF助手正在监听" : state.patch.proxyListening ? "被其他程序占用" : "未监听" }}
          </div>
        </div>
      </div>
      <div class="row" style="gap: 6px">
        <Button
          :variant="state.proxyRunning ? 'plain' : 'filled'"
          size="sm"
          @click="handleToggleProxy"
        >
          {{ state.proxyRunning ? "停止代理" : "启动代理" }}
        </Button>
      </div>
    </div>

    <Card v-if="launchTargets.length" title="Antigravity 快捷启动" subtitle="自动识别本机安装与运行状态">
      <template #action>
        <Badge tone="info" :label="`${launchTargets.length} 个可用安装`" />
      </template>
      <div class="launcher-list">
        <div v-for="target in launchTargets" :key="`launcher-${target.appPath}`" class="launcher-row">
          <div class="launcher-copy">
            <div class="row" style="gap:7px;min-width:0">
              <span class="t-headline truncate">{{ target.name }}</span>
              <span v-if="target.version" class="mono target-version">v{{ target.version }}</span>
              <Badge :tone="target.running ? 'ok' : 'neutral'" :label="target.running ? '运行中' : '未运行'" />
            </div>
            <div class="mono truncate launcher-path">{{ target.appPath }}</div>
          </div>
          <Button
            :variant="target.running ? 'tinted' : 'filled'"
            :loading="!!state.antigravityActionBusy[target.appPath]"
            :disabled="Object.values(state.antigravityActionBusy).some(Boolean)"
            @click="handleAntigravityAction(target)"
          >
            {{ target.running ? "重启" : "启动" }}
          </Button>
        </div>
        <div class="t-caption launcher-hint">
          重启只会请求应用正常退出，不会强制结束进程；退出完成后会再次同步聊天历史再启动。
        </div>
        <div v-if="state.antigravityActionMessage && !patchError" class="log-box">
          {{ state.antigravityActionMessage }}
        </div>
      </div>
    </Card>

    <!-- ── 统计卡片网格 ── -->
    <div class="metrics-grid">
      <!-- 缓存命中率 -->
      <div class="metric-card">
        <div class="t-footnote" style="margin-bottom: 10px">缓存命中率</div>
        <div class="ring-wrap">
          <svg viewBox="0 0 36 36" class="ring-svg">
            <circle cx="18" cy="18" r="15.9" fill="none" stroke="var(--separator-strong)" stroke-width="3.2"/>
            <circle
              cx="18" cy="18" r="15.9" fill="none"
              stroke="var(--green)" stroke-width="3.2"
              stroke-linecap="round"
              stroke-dashoffset="25"
              :style="cacheRing"
              transform="rotate(-90 18 18)"
            />
          </svg>
          <div class="ring-label">
            <span class="num" style="font-size:15px;font-weight:650;">{{ cacheHitPct }}</span>
          </div>
        </div>
      </div>

      <!-- 请求次数 -->
      <div class="metric-card">
        <div class="t-footnote" style="margin-bottom:10px">自定义请求</div>
        <div class="num" style="font-size:28px;font-weight:650;line-height:1">
          {{ formatK(state.stats.customRequests) }}
        </div>
        <div class="t-caption" style="margin-top:6px">
          共 {{ formatK(state.stats.totalRequests) }} 次请求
        </div>
      </div>

      <!-- Token 消耗 -->
      <div class="metric-card">
        <div class="t-footnote" style="margin-bottom:10px">Token 消耗</div>
        <div class="num" style="font-size:28px;font-weight:650;line-height:1">
          {{ formatK(state.stats.totalTokens) }}
        </div>
        <div class="t-caption" style="margin-top:6px">
          缓存读取 {{ formatK(state.stats.cacheReadTokens) }}
        </div>
      </div>

      <!-- 刷新 -->
      <div class="metric-card" style="justify-content:center;align-items:center">
        <button class="refresh-btn" :class="{ spinning: state.statsLoading }" @click="loadStats">
          <svg width="18" height="18" viewBox="0 0 18 18" fill="none">
            <path d="M15 9A6 6 0 1 1 9 3" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/>
            <path d="M9 1L12.5 3.5L9 6" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/>
          </svg>
        </button>
        <div class="t-caption" style="margin-top:8px">刷新统计</div>
      </div>
    </div>

    <Card title="历史会话" subtitle="启动时自动合并旧目录中的全部会话">
      <template #action>
        <Badge :tone="historyTone" :label="historyLabel" />
      </template>
      <div class="row between history-row">
        <div class="t-caption history-message">{{ state.historySync.message }}</div>
        <Button
          variant="plain"
          size="sm"
          :loading="state.historySyncBusy || state.historySync.state === 'running'"
          :disabled="state.historySyncBusy || state.historySync.state === 'running'"
          @click="handleHistorySync"
        >
          立即同步
        </Button>
      </div>
    </Card>

    <!-- ── 补丁状态 ── -->
    <Card title="补丁与安装" subtitle="按检测到的安装类型自动应用对应补丁">
      <template #action>
		<Badge
		  :tone="state.patch.targets?.length ? 'info' : 'warn'"
		  :label="state.patch.targets?.length ? `发现 ${state.patch.targets.length} 个安装` : '未找到安装'"
		/>
      </template>

      <div class="col" style="gap:10px">
        <div v-if="patchError" class="err-box">{{ patchError }}</div>
        <div v-if="state.patchLog && !patchError" class="log-box">{{ state.patchLog }}</div>

        <div class="proxy-diagnostic" :class="`diag-${proxyDiagnostic.tone}`">
          <div>{{ proxyDiagnostic.text }}</div>
          <div v-if="state.patch.lastRequestPath" class="mono diagnostic-path">
            最近请求：{{ state.patch.lastRequestPath }}
          </div>
        </div>

        <div class="row" style="gap:8px;flex-wrap:wrap">
          <Button variant="filled" :disabled="state.patchBusy" :loading="state.patchBusy" @click="handleApply">
            应用全部补丁
          </Button>
		  <Button v-if="hasSeparateAgentAndIDE" variant="tinted" :disabled="state.patchBusy" :loading="state.patchBusy" @click="handleApplyIDE">
			仅 IDE 补丁
          </Button>
          <Button variant="plain" :disabled="state.patchBusy" @click="handleRestore">
            恢复原始文件
          </Button>
        </div>

		<div v-if="state.patch.targets?.length" class="target-list">
		  <div v-for="target in state.patch.targets" :key="target.appPath" class="target-row">
			<div class="row between" style="gap:8px">
			  <div class="row" style="gap:7px;min-width:0">
				<span class="t-headline truncate">{{ target.name }}</span>
				<span v-if="target.version" class="mono target-version">v{{ target.version }}</span>
			  </div>
			  <div class="row" style="gap:6px">
					<Badge tone="neutral" :label="targetKindLabel(target.kind)" />
					<Badge :tone="target.running ? 'ok' : 'neutral'" :label="target.running ? '运行中' : '未运行'" />
					<Badge :tone="target.patched ? 'ok' : 'warn'" :label="target.patched ? '已补丁' : '待补丁'" />
			  </div>
			</div>
			<div class="mono truncate target-path">{{ target.appPath }}</div>
		  </div>
		</div>
      </div>
    </Card>
  </div>
</template>

<style scoped>
.page {
  display: flex;
  flex-direction: column;
  gap: 14px;
  padding: 18px 20px 28px;
  height: 100%;
  overflow-y: auto;
}

.page > * {
  flex-shrink: 0;
}

/* 顶部横幅 */
.banner {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 15px 17px;
  border-radius: var(--r-lg);
  border: 1px solid var(--separator-strong);
  box-shadow: var(--shadow-card);
  backdrop-filter: blur(16px);
}

.tone-ok {
  background: rgba(48, 209, 88, 0.09);
  border-color: rgba(48, 209, 88, 0.25);
  color: var(--green);
}
.tone-warn {
  background: rgba(255, 159, 10, 0.09);
  border-color: rgba(255, 159, 10, 0.25);
  color: var(--orange);
}
.tone-err {
  background: rgba(255, 69, 58, 0.09);
  border-color: rgba(255, 69, 58, 0.25);
  color: var(--red);
}

.dot-big {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  flex-shrink: 0;
}
.dot-ok { background: var(--green); box-shadow: 0 0 8px var(--green); animation: pulse-dot 2s ease infinite; }
.dot-warn { background: var(--orange); }
.dot-err { background: var(--red); }

/* 统计网格 */
.metrics-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 12px;
}

.metric-card {
  background: var(--bg-card);
  border: 1px solid var(--separator);
  border-radius: var(--r-lg);
  padding: 15px 17px;
  display: flex;
  flex-direction: column;
  min-height: 104px;
  box-shadow: var(--shadow-card);
  backdrop-filter: blur(16px);
  transition: transform 0.18s var(--ease), border-color 0.18s var(--ease);
}

.metric-card:hover {
  transform: translateY(-1px);
  border-color: var(--separator-strong);
}

.history-row {
  gap: 12px;
  min-width: 0;
}

.history-message {
  color: var(--text-secondary);
  min-width: 0;
  overflow-wrap: anywhere;
}

.launcher-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.launcher-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 11px;
  border: 1px solid var(--separator);
  border-radius: var(--r-sm);
  background: var(--bg-inset);
}

.launcher-copy {
  min-width: 0;
  flex: 1;
}

.launcher-path {
  margin-top: 5px;
  color: var(--text-tertiary);
  font-size: 10.5px;
}

.launcher-hint {
  color: var(--text-secondary);
  line-height: 1.5;
}

/* 环形图 */
.ring-wrap {
  position: relative;
  width: 64px;
  height: 64px;
}
.ring-svg {
  width: 100%;
  height: 100%;
}
.ring-label {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
}

/* 刷新按钮 */
.refresh-btn {
  width: 40px;
  height: 40px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  background: var(--bg-fill);
  color: var(--text-secondary);
  transition: all 0.18s var(--ease);
}
.refresh-btn:hover {
  background: var(--bg-fill-hover);
  color: var(--text-primary);
}
.refresh-btn.spinning svg {
  animation: spin 0.9s linear infinite;
}

/* 路径行 */
.path-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 6px 0;
  border-top: 0.5px solid var(--separator);
}

.target-list {
	display: flex;
	flex-direction: column;
	gap: 7px;
	padding-top: 2px;
}

.target-row {
	padding: 9px 10px;
	border: 1px solid var(--separator);
	border-radius: var(--r-sm);
	background: var(--bg-inset);
}

.target-version {
	color: var(--text-tertiary);
	font-size: 10.5px;
}

.target-path {
	margin-top: 5px;
	color: var(--text-tertiary);
	font-size: 10.5px;
}

.err-box {
  padding: 10px 12px;
  background: rgba(255, 69, 58, 0.1);
  border: 0.5px solid rgba(255, 69, 58, 0.25);
  border-radius: var(--r-sm);
  color: var(--red);
  font-size: 12.5px;
  white-space: pre-wrap;
}

.log-box {
  padding: 10px 12px;
  background: var(--bg-inset);
  border-radius: var(--r-sm);
  font-family: var(--font-num);
  font-size: 11.5px;
  color: var(--text-secondary);
  white-space: pre-wrap;
  max-height: 120px;
  overflow-y: auto;
}

.proxy-diagnostic {
  padding: 10px 12px;
  border: 1px solid var(--separator);
  border-radius: var(--r-sm);
  background: var(--bg-inset);
  color: var(--text-secondary);
  font-size: 12px;
}

.diag-ok { border-color: rgba(48, 209, 88, 0.28); color: var(--green); }
.diag-warn { border-color: rgba(255, 159, 10, 0.28); color: var(--orange); }
.diag-err { border-color: rgba(255, 69, 58, 0.28); color: var(--red); }

.diagnostic-path {
  margin-top: 4px;
  color: var(--text-tertiary);
}

@media (max-width: 980px) {
  .metrics-grid { grid-template-columns: repeat(2, 1fr); }
}
</style>
