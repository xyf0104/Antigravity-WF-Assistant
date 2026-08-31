<script setup>
import { computed } from "vue";
import CodexToolStatus from "@/components/CodexToolStatus.vue";
import AgentIcon from "@/components/AgentIcon.vue";

const props = defineProps({
  platforms: {
    type: Array,
    default: () => [],
  },
  selectedAgentId: {
    type: String,
    required: true,
  },
  loading: Boolean,
	preview: Boolean,
	actionBusy: {
		type: Object,
		default: () => ({}),
	},
	actionMessages: {
		type: Object,
		default: () => ({}),
	},
	manualSelections: {
		type: Object,
		default: () => ({}),
	},
	message: {
		type: String,
		default: "",
	},
});

const emit = defineEmits(["refresh", "configure", "diagnose", "choose", "launch", "open", "accounts"]);

const platformOrder = ["antigravity", "codex", "claude-code", "cursor", "windsurf"];

const fallbackPlatforms = [
  { agentId: "antigravity", displayName: "Antigravity WF", category: "desktop-ide" },
  { agentId: "codex", displayName: "Codex", category: "terminal-agent" },
  { agentId: "claude-code", displayName: "Claude Code", category: "terminal-agent" },
  { agentId: "cursor", displayName: "Cursor", category: "code-editor" },
  { agentId: "windsurf", displayName: "Windsurf", category: "code-editor" },
];

const agentActionDefinitions = {
  codex: [
    { id: "overview", label: "概览", icon: "overview" },
    { id: "provider", label: "Provider", icon: "configure", event: "configure", title: "配置 XIASS Tools Codex Provider" },
    { id: "models", label: "模型发现", icon: "models", event: "configure", title: "进入 Codex 模型发现" },
    { id: "backups", label: "备份 / 恢复", icon: "backup", event: "configure", title: "管理 Codex 配置恢复点" },
    { id: "history", label: "历史兼容", icon: "history", event: "configure", title: "检查 Codex Provider 历史兼容" },
    { id: "desktop", label: "Desktop", icon: "desktop", event: "configure", title: "管理已验证的 Codex Desktop" },
    { id: "diagnose", label: "诊断", icon: "diagnose", event: "diagnose", title: "诊断 Codex 本机状态" },
  ],
  "claude-code": [
    { id: "overview", label: "概览", icon: "overview" },
    { id: "gateway", label: "网关", icon: "gateway", event: "configure", title: "配置 Claude Code 网关" },
    { id: "model-test", label: "模型测试", icon: "test", event: "configure", title: "执行 Claude Messages 安全测试" },
    { id: "backups", label: "备份 / 恢复", icon: "backup", event: "configure", title: "管理 Claude Code 设置恢复点" },
    { id: "diagnose", label: "诊断", icon: "diagnose", event: "diagnose", title: "诊断 Claude Code 本机状态" },
  ],
  cursor: [
    { id: "overview", label: "概览", icon: "overview" },
    { id: "global-mcp", label: "全局 MCP", icon: "gateway", event: "configure", title: "配置 Cursor 全局 MCP" },
    { id: "project-mcp", label: "项目 MCP", icon: "project", event: "configure", title: "配置明确选择的 Cursor 项目 MCP" },
    { id: "backups", label: "恢复点", icon: "backup", event: "configure", title: "管理 Cursor MCP 恢复点" },
    { id: "diagnose", label: "诊断", icon: "diagnose", event: "diagnose", title: "诊断 Cursor 本机状态" },
    { id: "choose", label: "选择应用", icon: "choose", event: "choose", title: "选择并验证 Cursor 应用" },
    { id: "launch", label: "启动", icon: "launch", event: "launch", title: "启动已验证的 Cursor 应用" },
  ],
  windsurf: [
    { id: "overview", label: "概览", icon: "overview" },
    { id: "global-mcp", label: "全局 MCP", icon: "gateway", event: "configure", title: "配置 Windsurf 全局 MCP" },
    { id: "backups", label: "恢复点", icon: "backup", event: "configure", title: "管理 Windsurf MCP 恢复点" },
    { id: "diagnose", label: "诊断", icon: "diagnose", event: "diagnose", title: "诊断 Windsurf 本机状态" },
    { id: "choose", label: "选择应用", icon: "choose", event: "choose", title: "选择并验证 Windsurf 应用" },
    { id: "launch", label: "启动", icon: "launch", event: "launch", title: "启动已验证的 Windsurf 应用" },
  ],
};

const actionIconPaths = {
  overview: "M4 5.5h16v13H4zM8 9h8M8 13h5",
  configure: "M4 7h10M18 7h2M4 17h2M10 17h10M14 4v6M7 14v6",
  models: "M4 6h16v12H4zM8 10h8M8 14h5M17 14l2 2",
  backup: "M5 8h14v11H5zM8 5h8l2 3H6l2-3Zm4 6v5m-2-2 2 2 2-2",
  history: "M12 5a7 7 0 1 1-6.2 3.8M4 5v4h4M12 8v4l3 2",
  desktop: "M3 5h18v12H3zM8 21h8M12 17v4",
  gateway: "M5 7h14v10H5zM2.5 12H5m14 0h2.5M9 10l3 2 3-2M9 14l3-2 3 2",
  test: "M5 4h14v16H5zM8 9h8M8 13h4m2 3 1.5 1.5L19 14",
  project: "M3.5 7.5h6l1.7 2H20v10H3.5zM8 13h8M12 11v6",
  diagnose: "M3 12h4l2-5 4 10 2-5h6M5 5v3M19 16v3",
  choose: "M3.5 7.5h6l1.7 2H20a1.5 1.5 0 0 1 1.5 1.5v7A1.5 1.5 0 0 1 20 19.5H4A1.5 1.5 0 0 1 2.5 18V9A1.5 1.5 0 0 1 4 7.5h-.5ZM12 12v4m-2-2h4",
  launch: "m9 6 8 6-8 6V6Z",
};

const normalizedPlatforms = computed(() => {
  const incoming = new Map((props.platforms || []).map((platform) => [platform.agentId || platform.id, platform]));
  return platformOrder.map((agentId) => {
    const fallback = fallbackPlatforms.find((platform) => platform.agentId === agentId);
    const platform = incoming.get(agentId) || fallback;
    const capabilities = platform.capabilities || [];
    const available = capabilities.filter((capability) => capability.available).map((capability) => capability.capability);
    return {
      ...fallback,
      ...platform,
      agentId,
      displayName: agentId === "antigravity" ? "Antigravity WF" : (platform.displayName || fallback.displayName),
      capabilities,
      available,
      state: platform.state || "unknown",
      message: platform.message || "等待本地检查",
      installation: platform.installation || {},
    };
  });
});

const selectedPlatform = computed(() => normalizedPlatforms.value.find((platform) => platform.agentId === props.selectedAgentId) || null);
const selectedPlatforms = computed(() => selectedPlatform.value ? [selectedPlatform.value] : []);
const selectedFunctionActions = computed(() => agentActionDefinitions[selectedPlatform.value?.agentId] || []);

function stateLabel(state) {
  return {
    ready: "已就绪",
    detected: "已检测",
    degraded: "需要处理",
    "not-installed": "未安装",
    error: "检查失败",
    unbound: "待接入",
    unknown: "等待检查",
  }[state] || "等待检查";
}

function stateTone(state) {
  return {
    ready: "ok",
    detected: "info",
    degraded: "warn",
    "not-installed": "muted",
    error: "danger",
    unbound: "muted",
    unknown: "muted",
  }[state] || "muted";
}

function actionBusy(platform, action) {
  return Boolean(props.actionBusy?.[`${platform.agentId}:${action}`]);
}

function canLaunch(platform) {
  if (!platform || !["cursor", "windsurf"].includes(platform.agentId)) return false;
	if (Boolean(manualSelection(platform)?.canLaunch)) return true;
  const root = String(platform.installation?.root || "").trim().toLowerCase();
  const executable = String(platform.installation?.executablePath || "").trim().toLowerCase();
  const macTarget = root.endsWith(".app") && executable.startsWith(`${root}/contents/macos/`);
  const windowsRoot = root.replaceAll("\\", "/").replace(/\/+$/, "");
  const windowsExecutable = executable.replaceAll("\\", "/");
  const windowsTarget = Boolean(windowsRoot) && windowsExecutable.startsWith(`${windowsRoot}/`) && windowsExecutable.endsWith(".exe");
  return ["ready", "detected", "degraded"].includes(platform.state) && (macTarget || windowsTarget);
}

function manualSelection(platform) {
	return props.manualSelections?.[platform?.agentId] || null;
}

function displayToolMessage(platform) {
	if (manualSelection(platform)?.selected) return "已选择并验证手动应用；可安全打开，路径不会显示或保存。";
	return platform.message;
}

function displayToolVersion(platform) {
	return platform.installation?.version || manualSelection(platform)?.version || "";
}

function actionMessage(platform) {
  return props.actionMessages?.[platform.agentId] || null;
}

// The capability declarations come from the native adapter rather than a
// renderer-owned feature list. Rendering the distinction matters here: a
// platform may be discovered while a particular action still needs a verified
// local binding, and users should never have to infer that from a missing
// button or a generic "ready" badge.
function visibleCapabilities(platform) {
  return [...(platform?.capabilities || [])]
    .filter((item) => !["not-implemented", "not-applicable"].includes(item?.availability))
    .sort((left, right) => {
      const order = ["available", "requires-binding"];
      const leftIndex = order.indexOf(String(left?.availability || ""));
      const rightIndex = order.indexOf(String(right?.availability || ""));
      return (leftIndex === -1 ? order.length : leftIndex) - (rightIndex === -1 ? order.length : rightIndex);
    });
}

function capabilityStateLabel(item) {
  if (item?.available && item?.availability === "available") return "可用";
  return {
    "requires-binding": "待连接",
    "not-implemented": "未接入",
    "not-applicable": "不适用",
  }[item?.availability] || "待检查";
}

function capabilityStateTone(item) {
  if (item?.available && item?.availability === "available") return "ok";
  return {
    "requires-binding": "info",
    "not-implemented": "muted",
    "not-applicable": "muted",
  }[item?.availability] || "muted";
}

// The account page belongs to Antigravity WF's proxy and model-routing module.
// Codex and Claude Code keep compatible saved-credential selection inside
// their own configuration modals, so their entry points and state stay separate.
function functionActionBusy(action, platform) {
  return Boolean(action?.event && actionBusy(platform, action.event));
}

function functionActionDisabled(action, platform) {
  if (!action?.event) return false;
  if (props.preview) return true;
  if (action.event === "launch" && !canLaunch(platform)) return true;
  return functionActionBusy(action, platform);
}

function triggerFunctionAction(action, platform) {
  if (!action?.event || functionActionDisabled(action, platform)) return;
  // Several entries share one configuration dialog. Preserve the selected
  // action so the dialog can open the exact section instead of making every
  // toolbar button behave identically.
  emit(action.event, platform.agentId, action.id);
}

function categoryLabel(category) {
  return {
    "desktop-ide": "桌面开发环境",
    "terminal-agent": "终端 Agent",
    "code-editor": "代码编辑器",
  }[category] || "本地工具";
}

function capabilityLabel(name) {
	return {
		"installation-discovery": "安装检测",
		configuration: "配置",
		"local-proxy": "本地代理健康检查",
		"patch-injection": "连接补丁",
		"model-catalog": "手动模型获取",
		"session-recovery": "会话恢复",
		"image-input-output": "图片输入输出",
		diagnostics: "诊断",
		backup: "备份",
	}[name] || name;
}
</script>

<template>
  <section class="tools-view" aria-labelledby="tools-title">
    <header class="tools-header">
      <div class="tools-heading">
        <span class="tools-kicker">XIASS TOOLS</span>
        <h2 id="tools-title">{{ selectedPlatform?.displayName || "Agent" }}</h2>
        <p>{{ selectedPlatform ? `${categoryLabel(selectedPlatform.category)}的独立检测、配置、启动与诊断。` : "正在读取本机 Agent 状态。" }}</p>
      </div>
      <div class="tools-summary">
        <button class="refresh-button" type="button" :disabled="loading || preview" @click="emit('refresh')">
          <svg :class="{ spin: loading }" viewBox="0 0 24 24" aria-hidden="true">
            <path d="M20 11a8 8 0 1 0 2 5.3M20 4v7h-7" />
          </svg>
          <span>{{ loading ? "正在检查" : "检查本机" }}</span>
        </button>
      </div>
    </header>

    <div v-if="preview" class="tools-preview" role="status">{{ message }}</div>
    <p v-else-if="message" class="tools-error" role="alert">{{ message }}</p>

    <nav v-if="selectedPlatform" class="agent-function-bar" :aria-label="`${selectedPlatform.displayName} 功能`">
      <template
        v-for="action in selectedFunctionActions"
        :key="action.id"
      >
        <span
          v-if="!action.event"
          class="agent-function-action active"
          aria-current="page"
        >
          <svg viewBox="0 0 24 24" aria-hidden="true"><path :d="actionIconPaths[action.icon]" /></svg>
          <span>{{ action.label }}</span>
        </span>
        <button
          v-else
        type="button"
        class="agent-function-action"
        :title="action.title || action.label"
        :data-action="action.id"
        :disabled="functionActionDisabled(action, selectedPlatform)"
        @click="triggerFunctionAction(action, selectedPlatform)"
      >
        <svg :class="{ spin: functionActionBusy(action, selectedPlatform) }" viewBox="0 0 24 24" aria-hidden="true"><path :d="actionIconPaths[action.icon]" /></svg>
        <span>{{ action.label }}</span>
        </button>
      </template>
    </nav>

    <div v-if="selectedPlatform && actionMessage(selectedPlatform)?.message" class="tool-action-feedback" :class="actionMessage(selectedPlatform)?.tone || 'muted'" role="status" aria-live="polite">
      {{ actionMessage(selectedPlatform)?.message }}
    </div>

    <div class="agent-workbench">
      <div class="tool-rail" aria-live="polite">
      <article v-for="platform in selectedPlatforms" :key="platform.agentId" class="tool-card" :data-agent="platform.agentId">
        <div class="tool-card-head">
          <div class="tool-identity">
            <div class="tool-glyph"><AgentIcon :agent-id="platform.agentId" /></div>
            <div>
              <h3>{{ platform.displayName }}</h3>
              <span>{{ categoryLabel(platform.category) }}</span>
            </div>
          </div>
          <span class="status-chip" :class="stateTone(platform.state)">
            <i aria-hidden="true"></i>{{ stateLabel(platform.state) }}
          </span>
        </div>

        <div class="tool-state">
		  <p>{{ displayToolMessage(platform) }}</p>
		  <code v-if="displayToolVersion(platform)">v{{ displayToolVersion(platform) }}</code>
        </div>

        <CodexToolStatus v-if="platform.agentId === 'codex'" :platform="platform" />

        <dl v-else class="tool-facts">
          <div>
            <dt>安装位置</dt>
            <dd :title="platform.installation?.root || platform.installation?.executablePath || ''">
              {{ platform.installation?.root || platform.installation?.executablePath || "尚未确认" }}
            </dd>
          </div>
          <div>
            <dt>可用本机操作</dt>
				<dd>{{ platform.available.length ? platform.available.map(capabilityLabel).join(" · ") : "等待该工具完成本机检查" }}</dd>
          </div>
        </dl>

        <section class="tool-capabilities" :aria-label="`${platform.displayName} 功能状态`">
          <div class="tool-capability-head">
            <span>功能状态</span>
            <span>{{ platform.available.length }}/{{ visibleCapabilities(platform).length }} 可用</span>
          </div>
          <ul v-if="visibleCapabilities(platform).length">
            <li v-for="item in visibleCapabilities(platform)" :key="item.capability" :title="item.reason || ''">
              <span>{{ capabilityLabel(item.capability) }}</span>
              <em :class="capabilityStateTone(item)">{{ capabilityStateLabel(item) }}</em>
            </li>
          </ul>
          <p v-else class="tool-capability-empty">{{ preview ? "预览模式不会模拟本机能力；安装版完成检查后会显示可用功能。" : "本机尚未报告可展示的功能。" }}</p>
        </section>

	  </article>
      </div>
    </div>

    <footer class="tools-footer">
      <span>检测只读取本机公开安装信息；修改配置前会由对应工具创建可恢复备份。</span>
    </footer>
  </section>
</template>

<style scoped>
.tools-view {
  display: flex;
  min-height: 100%;
  flex-direction: column;
  gap: 22px;
  color: var(--text-primary);
  container-type: inline-size;
}

.tools-header {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 20px;
  padding: 2px 2px 0;
}

.tools-heading h2 {
  margin: 3px 0 4px;
  font-size: 27px;
  font-weight: 720;
  letter-spacing: 0;
}

.tools-heading p {
  margin: 0;
  color: var(--text-secondary);
  font-size: 14px;
}

.tools-kicker {
  color: var(--teal);
  font-family: var(--font-num);
  font-size: 11px;
  font-weight: 760;
  letter-spacing: .1em;
}

.tools-summary {
  display: flex;
  align-items: center;
  justify-content: flex-end;
}

.tools-error {
	margin: -4px 0 0;
	border: 1px solid color-mix(in srgb, var(--red) 35%, transparent);
	border-radius: var(--r-sm);
	background: color-mix(in srgb, var(--red) 8%, transparent);
	color: var(--red);
	padding: 9px 11px;
	font-size: 13px;
}

.tools-preview {
	display: flex;
	min-height: 108px;
	align-items: center;
	border: 1px dashed var(--separator-strong);
	border-left: 3px solid var(--blue);
	border-radius: var(--r-md);
	background: var(--bg-inset);
	color: var(--text-secondary);
	padding: 18px 20px;
	font-size: 13px;
	line-height: 1.6;
}

.refresh-button,
.agent-function-action {
  transition: background .16s var(--ease), border-color .16s var(--ease), color .16s var(--ease), transform .16s var(--ease);
}

.refresh-button {
  display: inline-flex;
  min-height: 38px;
  align-items: center;
  gap: 8px;
  border: 1px solid rgba(71, 213, 203, .3);
  border-radius: var(--r-sm);
  background: linear-gradient(135deg, rgba(37, 185, 213, .14), rgba(91, 214, 178, .12));
  color: var(--text-primary);
  padding: 0 13px;
  font-size: 13px;
  font-weight: 680;
}

.refresh-button:hover:not(:disabled) { border-color: rgba(71, 213, 203, .68); transform: translateY(-1px); }
.refresh-button:disabled { cursor: wait; opacity: .68; }
.refresh-button svg { width: 16px; fill: none; stroke: var(--teal); stroke-linecap: round; stroke-linejoin: round; stroke-width: 1.9; }

.agent-function-bar {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(126px, 1fr));
  min-height: 48px;
  align-items: center;
  gap: 7px;
  overflow: hidden;
  border: 1px solid var(--separator-strong);
  border-radius: var(--r-md);
  background: var(--bg-card);
  box-shadow: var(--shadow-card);
  padding: 7px;
}

.agent-function-action {
  display: inline-flex;
  min-width: 0;
  min-height: 40px;
  align-items: center;
  justify-content: center;
  gap: 7px;
  border: 1px solid transparent;
  border-radius: 9px;
  color: var(--text-secondary);
  padding: 0 11px;
  font-size: 13px;
  font-weight: 680;
}

.agent-function-action span {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.agent-function-action:hover:not(:disabled) {
  border-color: var(--separator-strong);
  background: var(--bg-fill-hover);
  color: var(--text-primary);
}

.agent-function-action.active {
  border-color: var(--accent-border);
  background: var(--accent-soft);
  color: var(--accent-strong);
}

.agent-function-action:disabled { cursor: wait; opacity: .68; }

.agent-function-action svg {
  width: 16px;
  height: 16px;
  fill: none;
  stroke: currentColor;
  stroke-linecap: round;
  stroke-linejoin: round;
  stroke-width: 1.8;
}

.tool-rail {
  display: grid;
  min-width: 0;
  grid-template-columns: minmax(0, 1fr);
  gap: 14px;
}

.agent-workbench { min-width: 0; }
.tool-card {
  width: 100%;
}

.tool-card {
  position: relative;
  display: flex;
  min-width: 0;
  min-height: 264px;
  flex-direction: column;
  overflow: hidden;
  border: 1px solid var(--separator-strong);
  border-radius: var(--r-md);
  background: var(--bg-card);
  box-shadow: var(--shadow-card);
}

.tool-action-feedback {
	min-height: 18px;
	margin: -10px 2px -8px;
	color: var(--text-tertiary);
	font-size: 12px;
	line-height: 1.45;
}

.tool-action-feedback.ok { color: var(--green); }
.tool-action-feedback.error { color: var(--red); }
.tool-action-feedback.progress { color: var(--blue); }

.tool-card::before {
  position: absolute;
  inset: 0 auto 0 0;
  width: 3px;
  background: var(--tool-accent, var(--teal));
  content: "";
}

.tool-card[data-agent="antigravity"] { --tool-accent: #6eb5ff; }
.tool-card[data-agent="codex"] { --tool-accent: #57d8c6; }
.tool-card[data-agent="claude-code"] { --tool-accent: #f3a76c; }
.tool-card[data-agent="cursor"] { --tool-accent: #9d93ff; }
.tool-card[data-agent="windsurf"] { --tool-accent: #42c4e9; }

.tool-card-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 18px 18px 14px 20px;
}

.tool-identity { display: flex; min-width: 0; align-items: center; gap: 11px; }

.tool-glyph {
  display: grid;
  width: 34px;
  height: 34px;
  flex: 0 0 34px;
  place-items: center;
  border: 1px solid color-mix(in srgb, var(--tool-accent) 35%, transparent);
  border-radius: 10px;
  background: color-mix(in srgb, var(--tool-accent) 14%, transparent);
  color: var(--tool-accent);
  font-family: var(--font-num);
  font-size: 13px;
  font-weight: 800;
}

.tool-glyph :deep(.agent-icon) { width: 21px; height: 21px; }

.tool-identity h3 { margin: 0; font-size: 16px; font-weight: 700; letter-spacing: 0; }
.tool-identity span { display: block; margin-top: 1px; color: var(--text-tertiary); font-size: 13px; }

.status-chip {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 6px;
  border: 1px solid var(--separator);
  border-radius: var(--r-full);
  color: var(--text-secondary);
  padding: 4px 8px;
  font-size: 12px;
  font-weight: 680;
}

.status-chip i { width: 6px; height: 6px; border-radius: 999px; background: var(--text-tertiary); }
.status-chip.ok { color: var(--green); border-color: color-mix(in srgb, var(--green) 34%, transparent); background: color-mix(in srgb, var(--green) 8%, transparent); }
.status-chip.ok i { background: var(--green); }
.status-chip.info { color: var(--blue); border-color: color-mix(in srgb, var(--blue) 34%, transparent); background: color-mix(in srgb, var(--blue) 8%, transparent); }
.status-chip.info i { background: var(--blue); }
.status-chip.warn { color: var(--orange); border-color: color-mix(in srgb, var(--orange) 34%, transparent); background: color-mix(in srgb, var(--orange) 8%, transparent); }
.status-chip.warn i { background: var(--orange); }
.status-chip.danger { color: var(--red); border-color: color-mix(in srgb, var(--red) 34%, transparent); background: color-mix(in srgb, var(--red) 8%, transparent); }
.status-chip.danger i { background: var(--red); }

.tool-state {
  display: flex;
  min-height: 42px;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  border-top: 1px solid var(--separator);
  border-bottom: 1px solid var(--separator);
  background: var(--bg-inset);
  padding: 0 18px 0 20px;
}

.tool-state p { overflow: hidden; margin: 0; color: var(--text-secondary); font-size: 13px; text-overflow: ellipsis; white-space: nowrap; }
.tool-state code { flex: 0 0 auto; color: var(--tool-accent); font-family: var(--font-num); font-size: 12px; }

.tool-facts { display: grid; gap: 13px; margin: 0; padding: 16px 18px 14px 20px; }
.tool-facts div { min-width: 0; }
.tool-facts dt { margin-bottom: 3px; color: var(--text-tertiary); font-size: 12px; }
.tool-facts dd { overflow: hidden; margin: 0; color: var(--text-secondary); font-family: var(--font-num); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }

.tool-capabilities {
  display: grid;
  gap: 8px;
  border-top: 1px solid var(--separator);
  padding: 13px 18px 14px 20px;
}

.tool-capability-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  color: var(--text-tertiary);
  font-size: 11px;
  letter-spacing: .04em;
}

.tool-capability-head span:last-child { color: var(--tool-accent); font-family: var(--font-num); letter-spacing: 0; }
.tool-capabilities ul { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 5px 8px; margin: 0; padding: 0; list-style: none; }
.tool-capabilities li { display: flex; min-width: 0; align-items: center; justify-content: space-between; gap: 6px; border: 1px solid var(--separator); border-radius: 7px; background: color-mix(in srgb, var(--bg-inset) 76%, transparent); padding: 5px 6px; }
.tool-capabilities li > span { overflow: hidden; color: var(--text-secondary); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.tool-capabilities em { flex: 0 0 auto; font-family: var(--font-num); font-size: 10px; font-style: normal; font-weight: 700; }
.tool-capabilities em.ok { color: var(--green); }
.tool-capabilities em.info { color: var(--blue); }
.tool-capabilities em.muted { color: var(--text-tertiary); }

.tool-capability-empty {
  margin: 0;
  border: 1px dashed var(--separator);
  border-radius: 7px;
  background: var(--bg-inset);
  color: var(--text-tertiary);
  padding: 9px 10px;
  font-size: 12px;
  line-height: 1.5;
}

.tools-footer {
  display: flex;
  justify-content: space-between;
  gap: 18px;
  border-top: 1px solid var(--separator);
  color: var(--text-tertiary);
  padding: 13px 2px 0;
  font-size: 13px;
}

@media (max-width: 940px) {
  .tools-header { align-items: flex-start; flex-direction: column; }
  .tools-summary { justify-content: flex-start; }
}

@media (max-width: 1200px) {
  .tool-rail { grid-template-columns: repeat(auto-fit, minmax(270px, 1fr)); }
}

@media (max-width: 540px) {
	.agent-function-bar { grid-template-columns: repeat(2, minmax(0, 1fr)); }
	.tool-rail { grid-template-columns: minmax(0, 1fr); }
	.tool-capabilities ul { grid-template-columns: 1fr; }
	.tools-footer { align-items: flex-start; flex-direction: column; }
}

@media (prefers-reduced-motion: reduce) {
  .refresh-button, .agent-function-action { transition: none; }
}
</style>
