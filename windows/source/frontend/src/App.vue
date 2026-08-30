<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import Dashboard from "@/views/Dashboard.vue";
import Tools from "@/views/Tools.vue";
import Models from "@/views/Models.vue";
import Accounts from "@/views/Accounts.vue";
import Permissions from "@/views/Permissions.vue";
import Settings from "@/views/Settings.vue";
import CodexConfigurationModal from "@/components/CodexConfigurationModal.vue";
import ClaudeCodeConfigurationModal from "@/components/ClaudeCodeConfigurationModal.vue";
import MCPConfigurationModal from "@/components/MCPConfigurationModal.vue";
import Button from "@/components/ui/Button.vue";
import Modal from "@/components/ui/Modal.vue";
import SegmentedControl from "@/components/ui/SegmentedControl.vue";
import {
	state,
	bootstrap,
	requestQuit,
	checkForUpdates,
	installLatestUpdate,
	applyPatch,
	loadPatchStatus,
	refreshAgentStatuses,
	diagnoseAgent,
} from "@/state/appState";

const tab = ref("dashboard");
const exitDialogOpen = ref(false);
const updateDialogOpen = ref(false);
const updateDialogError = ref("");
const dismissedUpdateVersion = ref("");
const repatchDialogOpen = ref(false);
const repatchError = ref("");
const agentDetailOpen = ref(false);
const codexConfigurationOpen = ref(false);
const claudeCodeConfigurationOpen = ref(false);
const mcpConfigurationOpen = ref(false);
const mcpConfigurationTarget = ref("");
const selectedAgentID = ref("");
const selectedAgentDiagnostics = ref([]);
let removeMainWindowShownListener = null;
const isMac = /Mac|iPhone|iPad|iPod/.test(navigator.platform);
const tabs = [
  { label: "总览", value: "dashboard", hint: "运行状态与快捷操作" },
  { label: "工具", value: "tools", hint: "本机 Agent 检测与配置入口" },
  { label: "模型", value: "models", hint: "管理自定义上游模型" },
  { label: "账户池", value: "accounts", hint: "凭据、健康状态与自动调度" },
  { label: "权限", value: "permissions", hint: "终端命令自动批准" },
  { label: "设置", value: "settings", hint: "稳定性、缓存与软件更新" },
];
const themeOptions = [
  { label: "浅色", value: "light" },
  { label: "深色", value: "dark" },
  { label: "跟随系统", value: "system" },
];
const storedTheme = localStorage.getItem("wf-theme");
const themeMode = ref(["light", "dark", "system"].includes(storedTheme) ? storedTheme : "system");
const systemTheme = window.matchMedia("(prefers-color-scheme: dark)");

const currentTab = computed(() => tabs.find((item) => item.value === tab.value) || tabs[0]);
const updateProgressPercent = computed(() => Math.min(100, Math.max(0, Number(state.update.progress?.percent) || 0)));
const selectedAgent = computed(() => state.agents.platforms.find((platform) => (platform.agentId || platform.id) === selectedAgentID.value) || null);

function applyTheme() {
  const resolved = themeMode.value === "system"
    ? (systemTheme.matches ? "dark" : "light")
    : themeMode.value;
  document.documentElement.dataset.theme = resolved;
  document.documentElement.dataset.themeMode = themeMode.value;
}

function handleSystemThemeChange() {
  if (themeMode.value === "system") applyTheme();
}

function quitAssistant() {
  requestQuit().catch(console.error);
}

function dismissUpdateDialog() {
	dismissedUpdateVersion.value = state.update.info?.latestVersion || "";
	updateDialogOpen.value = false;
}

async function handleInstallUpdate() {
	updateDialogError.value = "";
	const res = await installLatestUpdate();
	if (!res?.ok) updateDialogError.value = res?.message || "下载更新失败";
}

async function handleRequiredReconnect() {
	repatchError.value = "";
	const res = await applyPatch();
	if (!res?.ok) {
		repatchError.value = res?.message || "重新连接失败";
		return;
	}
	repatchDialogOpen.value = false;
}

function dismissRepatchDialog() {
	if (state.patchBusy) return;
	repatchError.value = "";
	repatchDialogOpen.value = false;
}

function handleMainWindowShown() {
	tab.value = "dashboard";
	void loadPatchStatus();
	if (state.settings?.updates?.autoCheck !== false) void checkForUpdates();
}

async function handleAgentRefresh() {
	await refreshAgentStatuses();
}

async function handleAgentDiagnose(agentID) {
	selectedAgentID.value = agentID;
	const result = await diagnoseAgent(agentID);
	selectedAgentDiagnostics.value = result?.diagnostics || [];
	agentDetailOpen.value = true;
}

function handleAgentConfigure(agentID) {
	if (agentID === "antigravity") {
		tab.value = "models";
		return;
	}
	if (agentID === "codex") {
		codexConfigurationOpen.value = true;
		return;
	}
	if (agentID === "claude-code") {
		claudeCodeConfigurationOpen.value = true;
		return;
	}
	if (agentID === "cursor" || agentID === "windsurf") {
		mcpConfigurationTarget.value = agentID;
		mcpConfigurationOpen.value = true;
		return;
	}
	selectedAgentID.value = agentID;
	selectedAgentDiagnostics.value = state.agents.diagnostics.filter((item) => item.agentId === agentID);
	agentDetailOpen.value = true;
}

function handleAgentOpen(agentID) {
	selectedAgentID.value = agentID;
	selectedAgentDiagnostics.value = state.agents.diagnostics.filter((item) => item.agentId === agentID);
	agentDetailOpen.value = true;
}

function handleCodexConfigurationChanged() {
	void refreshAgentStatuses();
}

function handleClaudeCodeConfigurationChanged() {
	void refreshAgentStatuses();
}

function handleMCPConfigurationChanged() {
	void refreshAgentStatuses();
}

watch(themeMode, (value) => {
  localStorage.setItem("wf-theme", value);
  applyTheme();
}, { immediate: true });

watch(
	() => [state.update.info?.available, state.update.info?.skipped, state.update.info?.latestVersion],
	([available, skipped, version]) => {
		if (available && !skipped && version && version !== dismissedUpdateVersion.value && !repatchDialogOpen.value) {
			updateDialogOpen.value = true;
		}
	},
);

watch(
	() => state.patch.productRepatchRequired,
	(required) => {
		if (!required) {
			repatchDialogOpen.value = false;
			return;
		}
		tab.value = "dashboard";
		updateDialogOpen.value = false;
		repatchError.value = "";
		repatchDialogOpen.value = true;
	},
);

onMounted(() => {
  systemTheme.addEventListener?.("change", handleSystemThemeChange);
  bootstrap().catch(console.error);
	const runtime = window.runtime;
	if (typeof runtime?.EventsOn === "function") {
		const unsubscribe = runtime.EventsOn("wf:main-window-shown", handleMainWindowShown);
		if (typeof unsubscribe === "function") removeMainWindowShownListener = unsubscribe;
		else if (typeof runtime.EventsOff === "function") {
			removeMainWindowShownListener = () => runtime.EventsOff("wf:main-window-shown");
		}
	}
});

onUnmounted(() => {
  systemTheme.removeEventListener?.("change", handleSystemThemeChange);
	removeMainWindowShownListener?.();
});
</script>

<template>
  <div class="shell">
    <aside class="sidebar" :class="{ mac: isMac }" style="--wails-draggable: drag">
      <div class="brand-mark" title="XIASS Tools">
        <img src="/xiass-tools-logo.png" alt="XIASS Tools" />
      </div>

      <nav class="nav-stack" aria-label="主导航" style="--wails-draggable: no-drag">
        <button
          v-for="item in tabs"
          :key="item.value"
          class="nav-item"
          :class="{ active: tab === item.value }"
          :aria-current="tab === item.value ? 'page' : undefined"
          @click="tab = item.value"
        >
          <svg v-if="item.value === 'dashboard'" viewBox="0 0 24 24" aria-hidden="true">
            <path d="M4 13h6V4H4v9Zm0 7h6v-4H4v4Zm10 0h6v-9h-6v9Zm0-16v4h6V4h-6Z" />
          </svg>
          <svg v-else-if="item.value === 'tools'" viewBox="0 0 24 24" aria-hidden="true">
            <path d="M13.4 6.6a4.1 4.1 0 0 0-5.8 5.8l-4.1 4.1a2 2 0 0 0 2.8 2.8l4.1-4.1a4.1 4.1 0 0 0 5.8-5.8l-2.5 2.5-2.3-.6-.6-2.3 2.5-2.4Z" />
          </svg>
          <svg v-else-if="item.value === 'models'" viewBox="0 0 24 24" aria-hidden="true">
            <path d="M12 3 3.8 7.4 12 11.8l8.2-4.4L12 3Zm-8.2 8L12 15.4l8.2-4.4M3.8 14.6 12 19l8.2-4.4" />
          </svg>
          <svg v-else-if="item.value === 'accounts'" viewBox="0 0 24 24" aria-hidden="true">
            <path d="M5 7.5A2.5 2.5 0 0 1 7.5 5h9A2.5 2.5 0 0 1 19 7.5v9a2.5 2.5 0 0 1-2.5 2.5h-9A2.5 2.5 0 0 1 5 16.5v-9ZM5 10h14M8 15h3" />
          </svg>
          <svg v-else-if="item.value === 'permissions'" viewBox="0 0 24 24" aria-hidden="true">
            <path d="M12 3 5 6v5c0 4.6 2.9 8.2 7 10 4.1-1.8 7-5.4 7-10V6l-7-3Zm-3 9 2 2 4-5" />
          </svg>
          <svg v-else viewBox="0 0 24 24" aria-hidden="true">
            <path d="M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8Zm8.6 4a6.7 6.7 0 0 0-.08-1l2-1.56-2-3.46-2.4.97a7.3 7.3 0 0 0-1.72-1L6.04 3H2.05L1.68 5.95a7.3 7.3 0 0 0-1.72 1l-2.4-.97-2 3.46 2 1.56a6.7 6.7 0 0 0 0 2l-2 1.56 2 3.46 2.4-.97a7.3 7.3 0 0 0 1.72 1L2.05 21h3.99l.36-2.95a7.3 7.3 0 0 0 1.72-1l2.4.97 2-3.46-2-1.56c.05-.33.08-.66.08-1Z" transform="translate(5.7 0) scale(.63)" />
          </svg>
          <span>{{ item.label }}</span>
        </button>
      </nav>

      <div class="sidebar-foot">
        <button
          class="exit-trigger"
          type="button"
          title="退出助手并停止本地代理"
          aria-label="退出助手并停止本地代理"
          style="--wails-draggable: no-drag"
          @click="exitDialogOpen = true"
        >
          <svg viewBox="0 0 24 24" aria-hidden="true">
            <path d="M12 3v9m5.66-5.66A8 8 0 1 1 6.34 6.34" />
          </svg>
        </button>
        <span class="version-pill">v1.6.6</span>
      </div>
    </aside>

    <section class="workspace">
      <header class="topbar" style="--wails-draggable: drag">
        <div class="page-title">
          <div class="eyebrow">XIASS TOOLS</div>
          <div class="title-row">
            <h1>{{ currentTab.label }}</h1>
            <span>{{ currentTab.hint }}</span>
          </div>
        </div>
        <SegmentedControl
          v-model="themeMode"
          :options="themeOptions"
          class="theme-control"
          style="--wails-draggable: no-drag"
        />
      </header>

      <main class="main">
        <Transition name="page" mode="out-in">
          <Dashboard v-if="tab === 'dashboard'" key="dashboard" />
          <Tools
            v-else-if="tab === 'tools'"
            key="tools"
            :platforms="state.agents.platforms"
            :loading="state.agents.loading"
            :action-busy="state.agents.actionBusy"
			:preview="state.agents.preview"
			:message="state.agents.message"
            @refresh="handleAgentRefresh"
            @configure="handleAgentConfigure"
            @diagnose="handleAgentDiagnose"
            @open="handleAgentOpen"
          />
          <Models v-else-if="tab === 'models'" key="models" />
          <Accounts v-else-if="tab === 'accounts'" key="accounts" />
          <Permissions v-else-if="tab === 'permissions'" key="permissions" />
          <Settings v-else key="settings" />
        </Transition>
      </main>
    </section>

    <Modal :open="exitDialogOpen" title="退出助手？" @close="exitDialogOpen = false">
      <p class="exit-copy">退出后会停止本地代理。点击窗口叉号只会隐藏主窗口；也可从系统菜单栏或托盘图标退出。</p>
      <template #footer>
        <Button variant="plain" @click="exitDialogOpen = false">取消</Button>
        <Button variant="danger" @click="quitAssistant">退出助手</Button>
      </template>
    </Modal>

	<Modal :open="agentDetailOpen" :title="selectedAgent ? `${selectedAgent.displayName} 本机状态` : '工具状态'" @close="agentDetailOpen = false">
	  <div class="agent-detail-copy">
		<span v-if="selectedAgent?.message">{{ selectedAgent.message }}</span>
		<span v-if="(selectedAgent?.agentId || selectedAgent?.id) !== 'codex' && selectedAgent?.installation?.root" class="agent-detail-path">{{ selectedAgent.installation.root }}</span>
		<div v-if="selectedAgentDiagnostics.length" class="agent-diagnostic-list">
		  <article v-for="(diagnostic, index) in selectedAgentDiagnostics" :key="`${diagnostic.code}-${index}`" :class="['agent-diagnostic', diagnostic.severity || 'info']">
			<strong>{{ diagnostic.summary }}</strong>
			<span v-if="diagnostic.detail">{{ diagnostic.detail }}</span>
			<span v-if="diagnostic.remediation" class="agent-remediation">{{ diagnostic.remediation }}</span>
		  </article>
		</div>
		<span v-else class="agent-detail-empty">尚无额外诊断信息。</span>
	  </div>
	  <template #footer>
		<Button variant="plain" @click="agentDetailOpen = false">关闭</Button>
	  </template>
	</Modal>

	<CodexConfigurationModal
		:open="codexConfigurationOpen"
		@close="codexConfigurationOpen = false"
		@changed="handleCodexConfigurationChanged"
	/>

	<ClaudeCodeConfigurationModal
		:open="claudeCodeConfigurationOpen"
		@close="claudeCodeConfigurationOpen = false"
		@changed="handleClaudeCodeConfigurationChanged"
	/>

	<MCPConfigurationModal
		:open="mcpConfigurationOpen"
		:target="mcpConfigurationTarget"
		@close="mcpConfigurationOpen = false"
		@changed="handleMCPConfigurationChanged"
	/>

	<Modal :open="updateDialogOpen" title="发现新版本" @close="dismissUpdateDialog">
	  <div class="global-dialog-copy">
		<strong>XIASS Tools v{{ state.update.info.latestVersion }}</strong>
		<span>当前为 v{{ state.update.info.currentVersion || '—' }}，可以直接下载、校验并启动新版安装程序。</span>
		<span v-if="state.update.info.notes" class="update-notes">{{ state.update.info.notes }}</span>
	  </div>
	  <div v-if="state.update.installing" class="global-progress">
		<div><span>{{ state.update.progress?.message || '正在下载更新' }}</span><strong>{{ updateProgressPercent }}%</strong></div>
		<div class="global-progress-track"><span :style="{ width: `${updateProgressPercent}%` }"></span></div>
	  </div>
	  <div v-if="updateDialogError" class="global-error">{{ updateDialogError }}</div>
	  <template #footer>
		<Button variant="plain" :disabled="state.update.installing" @click="dismissUpdateDialog">稍后</Button>
		<Button variant="filled" :loading="state.update.installing" :disabled="state.update.installing" @click="handleInstallUpdate">一键下载安装</Button>
	  </template>
	</Modal>

	<Modal :open="repatchDialogOpen" title="Antigravity 需要重新连接" :closable="!state.patchBusy" persistent @close="dismissRepatchDialog">
	  <div class="global-dialog-copy">
		<strong>检测到安装版本或连接规则已更新</strong>
		<span>{{ state.patch.productRepatchMessage || '检测到 Antigravity 安装发生变化，请重新连接后继续使用。' }}</span>
		<span>请先完全退出正在运行的 Antigravity。助手会按当前安装结构自动选择兼容注入方式。</span>
		<span>也可以暂时跳过，之后随时在首页手动连接并升级到最新补丁规则。</span>
	  </div>
	  <div v-if="repatchError" class="global-error">{{ repatchError }}</div>
	  <template #footer>
		<Button variant="plain" :disabled="state.patchBusy" @click="dismissRepatchDialog">稍后再说</Button>
		<Button variant="filled" :loading="state.patchBusy" :disabled="state.patchBusy" @click="handleRequiredReconnect">立即重新连接</Button>
	  </template>
	</Modal>
  </div>
</template>

<style scoped>
.shell {
  display: grid;
  grid-template-columns: 90px minmax(0, 1fr);
  height: 100vh;
  background: var(--bg-base);
}

.agent-detail-copy {
	display: grid;
	gap: 12px;
	color: var(--text-secondary);
	font-size: 13px;
	line-height: 1.6;
}

.agent-detail-path {
	overflow: hidden;
	border: 1px solid var(--separator);
	border-radius: 7px;
	background: var(--bg-inset);
	color: var(--text-tertiary);
	font-family: var(--font-num);
	font-size: 11px;
	padding: 8px 10px;
	text-overflow: ellipsis;
	white-space: nowrap;
}

.agent-diagnostic-list { display: grid; gap: 8px; }

.agent-diagnostic {
	display: grid;
	gap: 3px;
	border: 1px solid var(--separator);
	border-left: 3px solid var(--blue);
	border-radius: 7px;
	background: var(--bg-inset);
	padding: 10px 11px;
}

.agent-diagnostic.warning { border-left-color: var(--orange); }
.agent-diagnostic.error { border-left-color: var(--red); }
.agent-diagnostic strong { color: var(--text-primary); font-size: 12px; }
.agent-diagnostic span { color: var(--text-secondary); font-size: 12px; }
.agent-diagnostic .agent-remediation { color: var(--text-tertiary); }
.agent-detail-empty { color: var(--text-tertiary); }

.sidebar {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 20px;
  padding: 18px 10px 14px;
  border-right: 1px solid var(--separator);
  background: var(--bg-sidebar);
  position: relative;
  z-index: 20;
}

.sidebar.mac {
  padding-top: 42px;
}

.brand-mark {
  width: 58px;
  height: 58px;
  display: grid;
  place-items: center;
  border-radius: 18px;
}

.brand-mark img {
  width: 48px;
  height: 48px;
  object-fit: contain;
}

.nav-stack {
  display: flex;
  flex-direction: column;
  gap: 9px;
  width: 100%;
}

.nav-item {
  width: 100%;
  min-height: 62px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 5px;
  border-radius: 16px;
  color: var(--text-tertiary);
  transition: color 0.18s var(--ease), background 0.18s var(--ease), transform 0.18s var(--spring);
}

.nav-item:hover {
  color: var(--text-primary);
  background: var(--bg-fill);
}

.nav-item:active {
  transform: scale(0.96);
}

.nav-item.active {
  color: var(--accent-strong);
  background: var(--accent-soft);
  box-shadow: inset 0 0 0 1px var(--accent-border);
}

.nav-item svg {
  width: 22px;
  height: 22px;
  fill: none;
  stroke: currentColor;
  stroke-width: 1.8;
  stroke-linecap: round;
  stroke-linejoin: round;
}

.nav-item:first-child svg {
  fill: currentColor;
  stroke: none;
}

.nav-item span {
  font-size: 11px;
  font-weight: 650;
}

.sidebar-foot {
  margin-top: auto;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
}

.exit-trigger {
  width: 30px;
  height: 30px;
  display: grid;
  place-items: center;
  border-radius: 10px;
  color: var(--text-tertiary);
  transition: color 0.16s var(--ease), background 0.16s var(--ease);
}

.exit-trigger:hover {
  color: var(--red);
  background: rgba(255, 69, 58, 0.12);
}

.exit-trigger svg {
  width: 17px;
  height: 17px;
  fill: none;
  stroke: currentColor;
  stroke-width: 1.9;
  stroke-linecap: round;
}

.exit-copy {
  margin: 0;
  color: var(--text-secondary);
  font-size: 13px;
  line-height: 1.65;
}

.global-dialog-copy {
  display: flex;
  flex-direction: column;
  gap: 8px;
  color: var(--text-secondary);
  font-size: 13px;
  line-height: 1.6;
}

.global-dialog-copy strong {
  color: var(--text-primary);
  font-size: 15px;
}

.update-notes {
  max-height: 96px;
  overflow: auto;
  padding: 9px 10px;
  border-radius: var(--r-md);
  background: var(--bg-fill);
  white-space: pre-wrap;
}

.global-progress {
  margin-top: 14px;
}

.global-progress > div:first-child {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 7px;
  color: var(--text-secondary);
  font-size: 12px;
}

.global-progress-track {
  height: 5px;
  overflow: hidden;
  border-radius: 999px;
  background: var(--bg-fill);
}

.global-progress-track span {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: var(--accent-strong);
  transition: width 0.18s var(--ease);
}

.global-error {
  margin-top: 12px;
  padding: 9px 10px;
  border-radius: var(--r-md);
  color: var(--red);
  background: rgba(255, 69, 58, 0.1);
  font-size: 12px;
  line-height: 1.5;
}

.version-pill {
  display: inline-flex;
  padding: 4px 8px;
  border-radius: 999px;
  background: var(--bg-fill);
  color: var(--text-tertiary);
  font: 600 10px/1 var(--font-num);
}

.workspace {
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
  background:
    radial-gradient(circle at 92% 0%, var(--ambient-one), transparent 34%),
    radial-gradient(circle at 20% 10%, var(--ambient-two), transparent 38%),
    var(--bg-base);
}

.topbar {
  min-height: 74px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  padding: 14px 24px;
  border-bottom: 1px solid var(--separator);
  background: var(--topbar-bg);
  backdrop-filter: blur(24px) saturate(140%);
  -webkit-backdrop-filter: blur(24px) saturate(140%);
  flex-shrink: 0;
  position: relative;
  z-index: 10;
}

.page-title {
  min-width: 0;
}

.eyebrow {
  color: var(--accent-strong);
  font-size: 9.5px;
  font-weight: 750;
  letter-spacing: 0.13em;
}

.title-row {
  display: flex;
  align-items: baseline;
  gap: 11px;
}

.title-row h1 {
  margin: 0;
  font-size: 24px;
  line-height: 1.25;
  letter-spacing: -0.035em;
}

.title-row span {
  color: var(--text-secondary);
  font-size: 12px;
}

.theme-control {
  flex-shrink: 0;
}

.main {
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

.page-enter-active,
.page-leave-active {
  transition: opacity 0.18s var(--ease), transform 0.18s var(--ease);
}

.page-enter-from {
  opacity: 0;
  transform: translateY(5px);
}

.page-leave-to {
  opacity: 0;
  transform: translateY(-3px);
}

@media (max-width: 940px) {
  .shell {
    grid-template-columns: 78px minmax(0, 1fr);
  }

  .sidebar {
    padding-inline: 8px;
  }

  .brand-mark {
    width: 52px;
    height: 52px;
  }

  .brand-mark img {
    width: 43px;
    height: 43px;
  }

  .title-row span {
    display: none;
  }
}
</style>
