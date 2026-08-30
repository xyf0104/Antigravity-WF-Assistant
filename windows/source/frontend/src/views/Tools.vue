<script setup>
import { computed } from "vue";
import CodexToolStatus from "@/components/CodexToolStatus.vue";

const props = defineProps({
  platforms: {
    type: Array,
    default: () => [],
  },
  loading: Boolean,
	preview: Boolean,
  actionBusy: {
    type: Object,
    default: () => ({}),
  },
	message: {
		type: String,
		default: "",
	},
});

const emit = defineEmits(["refresh", "configure", "diagnose", "open"]);

const platformOrder = ["antigravity", "codex", "claude-code", "cursor", "windsurf"];

const fallbackPlatforms = [
  { agentId: "antigravity", displayName: "Antigravity", category: "desktop-ide" },
  { agentId: "codex", displayName: "Codex", category: "terminal-agent" },
  { agentId: "claude-code", displayName: "Claude Code", category: "terminal-agent" },
  { agentId: "cursor", displayName: "Cursor", category: "code-editor" },
  { agentId: "windsurf", displayName: "Windsurf", category: "code-editor" },
];

const normalizedPlatforms = computed(() => {
  if (props.preview) return [];
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
      capabilities,
      available,
      state: platform.state || "unknown",
      message: platform.message || "等待本地检查",
      installation: platform.installation || {},
    };
  });
});

const summary = computed(() => {
  const platforms = normalizedPlatforms.value;
  const detected = platforms.filter((platform) => ["ready", "detected", "degraded"].includes(platform.state));
  const ready = platforms.filter((platform) => platform.state === "ready");
  return { total: platforms.length, detected: detected.length, ready: ready.length };
});

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

function capability(platform, name) {
  return platform.capabilities.find((item) => item.capability === name);
}

function isAvailable(platform, name) {
  const item = capability(platform, name);
  return Boolean(item?.available && item?.availability === "available");
}

function configurationActionLabel(platform) {
  const item = capability(platform, "configuration");
  if (item?.availability === "available") {
    return item.available ? "配置" : "暂不可配置";
  }
  return "等待接入";
}

function glyph(platform) {
  return {
    antigravity: "A",
    codex: "C",
    "claude-code": "Cl",
    cursor: "Cu",
    windsurf: "W",
  }[platform.agentId] || "·";
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
		oauth: "OAuth",
		usage: "用量",
		"two-factor-authentication": "双重验证",
	}[name] || name;
}
</script>

<template>
  <section class="tools-view" aria-labelledby="tools-title">
    <header class="tools-header">
      <div class="tools-heading">
        <span class="tools-kicker">XIASS TOOLS</span>
        <h2 id="tools-title">工具中心</h2>
        <p>管理本机 Agent 的检测、配置、启动与诊断。</p>
      </div>
      <div v-if="!preview" class="tools-summary" aria-label="本机集成状态">
        <div>
          <strong>{{ summary.detected }}</strong>
          <span>已检测</span>
        </div>
        <div>
          <strong>{{ summary.ready }}</strong>
          <span>已就绪</span>
        </div>
        <button class="refresh-button" type="button" :disabled="loading" @click="emit('refresh')">
          <svg :class="{ spin: loading }" viewBox="0 0 24 24" aria-hidden="true">
            <path d="M20 11a8 8 0 1 0 2 5.3M20 4v7h-7" />
          </svg>
          <span>{{ loading ? "正在检查" : "检查本机" }}</span>
        </button>
      </div>
    </header>

	<div v-if="preview" class="tools-preview" role="status">{{ message }}</div>
	<p v-else-if="message" class="tools-error" role="alert">{{ message }}</p>

    <div v-if="!preview" class="tool-rail" aria-label="Agent 工具列表">
      <article v-for="platform in normalizedPlatforms" :key="platform.agentId" class="tool-card" :data-agent="platform.agentId">
        <div class="tool-card-head">
          <div class="tool-identity">
            <div class="tool-glyph" aria-hidden="true">{{ glyph(platform) }}</div>
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
          <p>{{ platform.message }}</p>
          <code v-if="platform.installation?.version">v{{ platform.installation.version }}</code>
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

        <div class="tool-actions">
          <button class="icon-action" type="button" :title="`诊断 ${platform.displayName}`" :aria-label="`诊断 ${platform.displayName}`" :disabled="actionBusy(platform, 'diagnose')" @click="emit('diagnose', platform.agentId)">
            <svg :class="{ spin: actionBusy(platform, 'diagnose') }" viewBox="0 0 24 24" aria-hidden="true">
              <path d="M12 3a9 9 0 1 0 9 9M12 7v5l3 2M12 3v2M3 12h2M19 12h2M5.6 5.6 7 7M17 17l1.4 1.4" />
            </svg>
          </button>
          <button class="text-action" type="button" :disabled="!isAvailable(platform, 'configuration') || actionBusy(platform, 'configure')" @click="emit('configure', platform.agentId)">
            {{ configurationActionLabel(platform) }}
          </button>
          <button class="icon-action" type="button" :title="`查看 ${platform.displayName} 状态`" :aria-label="`查看 ${platform.displayName} 状态`" @click="emit('open', platform.agentId)">
            <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 12a7 7 0 0 1 14 0 7 7 0 0 1-14 0Zm7-3v3l2 1.5M12 5V3M19 12h2M5 12H3M18.4 5.6 20 4M5.6 5.6 4 4" /></svg>
          </button>
        </div>
      </article>
    </div>

    <footer v-if="!preview" class="tools-footer">
      <span>检测只读取本机公开安装信息；修改配置前会由对应工具创建可恢复备份。</span>
      <span>{{ summary.total }} 个集成</span>
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
  min-width: 320px;
  align-items: center;
  justify-content: flex-end;
  gap: 14px;
}

.tools-summary > div {
  display: grid;
  min-width: 52px;
  gap: 1px;
  text-align: right;
}

.tools-summary strong {
  color: var(--text-primary);
  font-family: var(--font-num);
  font-size: 20px;
  line-height: 1;
}

.tools-summary span {
  color: var(--text-tertiary);
  font-size: 11px;
}

.tools-error {
	margin: -4px 0 0;
	border: 1px solid color-mix(in srgb, var(--red) 35%, transparent);
	border-radius: var(--r-sm);
	background: color-mix(in srgb, var(--red) 8%, transparent);
	color: var(--red);
	padding: 9px 11px;
	font-size: 12px;
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
.text-action,
.icon-action {
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

.tool-rail {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(270px, 1fr));
  gap: 14px;
}

@container (min-width: 1406px) {
  .tool-rail { grid-template-columns: repeat(5, minmax(270px, 1fr)); }
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

.tool-identity h3 { margin: 0; font-size: 16px; font-weight: 700; letter-spacing: 0; }
.tool-identity span { display: block; margin-top: 1px; color: var(--text-tertiary); font-size: 12px; }

.status-chip {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 6px;
  border: 1px solid var(--separator);
  border-radius: var(--r-full);
  color: var(--text-secondary);
  padding: 4px 8px;
  font-size: 11px;
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

.tool-state p { overflow: hidden; margin: 0; color: var(--text-secondary); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.tool-state code { flex: 0 0 auto; color: var(--tool-accent); font-family: var(--font-num); font-size: 11px; }

.tool-facts { display: grid; gap: 13px; margin: 0; padding: 16px 18px 14px 20px; }
.tool-facts div { min-width: 0; }
.tool-facts dt { margin-bottom: 3px; color: var(--text-tertiary); font-size: 11px; }
.tool-facts dd { overflow: hidden; margin: 0; color: var(--text-secondary); font-family: var(--font-num); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }

.tool-actions {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 7px;
  margin-top: auto;
  border-top: 1px solid var(--separator);
  padding: 12px 14px 12px 20px;
}

.icon-action {
  display: grid;
  width: 32px;
  height: 32px;
  flex: 0 0 32px;
  place-items: center;
  border: 1px solid var(--separator);
  border-radius: 9px;
  color: var(--text-secondary);
}

.icon-action:hover:not(:disabled) { border-color: color-mix(in srgb, var(--tool-accent) 50%, transparent); background: color-mix(in srgb, var(--tool-accent) 10%, transparent); color: var(--tool-accent); }
.icon-action:disabled { cursor: not-allowed; opacity: .36; }
.icon-action svg { width: 15px; fill: none; stroke: currentColor; stroke-linecap: round; stroke-linejoin: round; stroke-width: 1.9; }
.icon-action svg.play-icon { fill: currentColor; stroke: none; }

.text-action {
  min-width: 0;
  min-height: 32px;
  margin-left: auto;
  border: 1px solid color-mix(in srgb, var(--tool-accent) 42%, transparent);
  border-radius: 9px;
  background: color-mix(in srgb, var(--tool-accent) 10%, transparent);
  color: var(--tool-accent);
  padding: 0 10px;
  font-size: 12px;
  font-weight: 700;
	white-space: nowrap;
}

.text-action:hover:not(:disabled) { background: color-mix(in srgb, var(--tool-accent) 18%, transparent); }
.text-action:disabled { border-color: var(--separator); background: transparent; color: var(--text-tertiary); cursor: not-allowed; }

.tools-footer {
  display: flex;
  justify-content: space-between;
  gap: 18px;
  border-top: 1px solid var(--separator);
  color: var(--text-tertiary);
  padding: 13px 2px 0;
  font-size: 12px;
}

@media (max-width: 940px) {
  .tools-header { align-items: flex-start; flex-direction: column; }
  .tools-summary { justify-content: flex-start; }
}

@media (max-width: 1200px) {
  .tool-rail { grid-template-columns: repeat(auto-fit, minmax(270px, 1fr)); }
}

@media (max-width: 540px) {
	.tool-rail { grid-template-columns: minmax(0, 1fr); }
	.tool-actions { padding-right: 16px; }
	.tools-footer { align-items: flex-start; flex-direction: column; }
}

@media (prefers-reduced-motion: reduce) {
  .refresh-button, .text-action, .icon-action { transition: none; }
}
</style>
