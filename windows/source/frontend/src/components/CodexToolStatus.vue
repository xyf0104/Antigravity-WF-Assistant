<script setup>
import { computed } from "vue";

const props = defineProps({
  platform: {
    type: Object,
    required: true,
  },
});

const details = computed(() => props.platform?.details || {});
const desktopState = computed(() => String(details.value.desktopState || ""));
const configPresent = computed(() => details.value.configPresent === "true");
const configValid = computed(() => details.value.configValid === "true");
const provider = computed(() => String(details.value.provider || "").trim());

const desktop = computed(() => {
  switch (desktopState.value) {
    case "running":
      return { tone: "danger", label: "运行中", detail: "已确认安装；变更后需自行退出并重启" };
    case "installed":
      return { tone: "ok", label: "已安装，未运行", detail: "已在支持的位置确认 Desktop" };
    case "not_installed":
      return { tone: "muted", label: "未发现", detail: "未在支持的位置确认 Desktop" };
    case "degraded":
      return { tone: "warn", label: "无法确认", detail: "运行状态未完整确认" };
    default:
      return { tone: "muted", label: "等待检查", detail: "尚未获得 Desktop 状态" };
  }
});

const configuration = computed(() => {
  if (!configPresent.value) {
    return { tone: "muted", label: "尚未创建", detail: "未发现 config.toml" };
  }
  if (!configValid.value) {
    return { tone: "danger", label: "需要修复", detail: "config.toml 未通过验证" };
  }
  if (provider.value === "xiass_tools") {
    return { tone: "ok", label: "已由 XIASS Tools 管理", detail: "config.toml 已验证" };
  }
  return {
    tone: "info",
    label: "已验证",
    detail: provider.value ? `当前 Provider：${provider.value}` : "当前 Provider 未标识",
  };
});

const history = computed(() => {
  if (desktopState.value === "running") {
    return { tone: "danger", label: "暂缓写入", detail: "请先自行退出 Codex" };
  }
  if (desktopState.value === "degraded") {
    return { tone: "warn", label: "暂缓写入", detail: "运行状态未确认" };
  }
  if (!configPresent.value) {
    return { tone: "muted", label: "等待配置", detail: "创建 config.toml 后再检查历史" };
  }
  if (!configValid.value) {
    return { tone: "warn", label: "等待验证", detail: "先修复 config.toml" };
  }
  return { tone: "ok", label: "可以检查", detail: "写入前仍会再次确认运行状态" };
});

const states = computed(() => [
  { key: "desktop", name: "Desktop", ...desktop.value },
  { key: "configuration", name: "config.toml", ...configuration.value },
  { key: "history", name: "历史写入", ...history.value },
]);
</script>

<template>
  <dl class="codex-tool-status" aria-label="Codex 本机状态">
    <div v-for="item in states" :key="item.key" class="status-row" :class="item.tone">
      <dt>
        <i aria-hidden="true"></i>
        <span>{{ item.name }}</span>
      </dt>
      <dd>
        <span>{{ item.detail }}</span>
      </dd>
      <em>{{ item.label }}</em>
    </div>
  </dl>
</template>

<style scoped>
.codex-tool-status {
  display: grid;
  margin: 0;
  padding: 0 18px 14px 20px;
}

.status-row {
  display: grid;
  grid-template-columns: minmax(82px, .75fr) minmax(0, 1.6fr) auto;
  align-items: center;
  gap: 10px;
  min-height: 47px;
  border-bottom: 1px solid var(--separator);
}

.status-row:last-child { border-bottom: 0; }
.status-row dt { display: inline-flex; align-items: center; gap: 7px; color: var(--text-tertiary); font-size: 11px; }
.status-row dt i { width: 7px; height: 7px; border-radius: 50%; background: var(--text-tertiary); }
.status-row dd { display: grid; min-width: 0; margin: 0; }
.status-row dd span { overflow: hidden; color: var(--text-tertiary); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.status-row em { font-family: var(--font-num); font-size: 10px; font-style: normal; font-weight: 700; white-space: nowrap; }

.status-row.ok dt i { background: var(--green); }
.status-row.ok em { color: var(--green); }
.status-row.info dt i { background: var(--blue); }
.status-row.info em { color: var(--blue); }
.status-row.warn dt i { background: var(--orange); }
.status-row.warn em { color: var(--orange); }
.status-row.danger dt i { background: var(--red); }
.status-row.danger em { color: var(--red); }
.status-row.muted em { color: var(--text-tertiary); }

@media (max-width: 560px) {
  .status-row { grid-template-columns: minmax(72px, .7fr) minmax(0, 1.55fr) auto; gap: 8px; }
  .status-row dd span { white-space: normal; }
}
</style>
