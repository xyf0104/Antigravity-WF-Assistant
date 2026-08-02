<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import Dashboard from "@/views/Dashboard.vue";
import Models from "@/views/Models.vue";
import Permissions from "@/views/Permissions.vue";
import SegmentedControl from "@/components/ui/SegmentedControl.vue";
import { bootstrap } from "@/state/appState";

const tab = ref("dashboard");
const isMac = /Mac|iPhone|iPad|iPod/.test(navigator.platform);
const tabs = [
  { label: "总览", value: "dashboard", hint: "运行状态与快捷操作" },
  { label: "模型", value: "models", hint: "管理自定义上游模型" },
  { label: "权限", value: "permissions", hint: "终端命令自动批准" },
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

watch(themeMode, (value) => {
  localStorage.setItem("wf-theme", value);
  applyTheme();
}, { immediate: true });

onMounted(() => {
  systemTheme.addEventListener?.("change", handleSystemThemeChange);
  bootstrap().catch(console.error);
});

onUnmounted(() => {
  systemTheme.removeEventListener?.("change", handleSystemThemeChange);
});
</script>

<template>
  <div class="shell">
    <aside class="sidebar" :class="{ mac: isMac }" style="--wails-draggable: drag">
      <div class="brand-mark" title="Antigravity WF助手">
        <img src="/wf-logo.png" alt="Antigravity WF" />
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
          <svg v-else-if="item.value === 'models'" viewBox="0 0 24 24" aria-hidden="true">
            <path d="M12 3 3.8 7.4 12 11.8l8.2-4.4L12 3Zm-8.2 8L12 15.4l8.2-4.4M3.8 14.6 12 19l8.2-4.4" />
          </svg>
          <svg v-else viewBox="0 0 24 24" aria-hidden="true">
            <path d="M12 3 5 6v5c0 4.6 2.9 8.2 7 10 4.1-1.8 7-5.4 7-10V6l-7-3Zm-3 9 2 2 4-5" />
          </svg>
          <span>{{ item.label }}</span>
        </button>
      </nav>

      <div class="sidebar-foot">
        <span class="version-pill">v1.3</span>
      </div>
    </aside>

    <section class="workspace">
      <header class="topbar" style="--wails-draggable: drag">
        <div class="page-title">
          <div class="eyebrow">ANTIGRAVITY WF助手</div>
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
          <Models v-else-if="tab === 'models'" key="models" />
          <Permissions v-else key="permissions" />
        </Transition>
      </main>
    </section>
  </div>
</template>

<style scoped>
.shell {
  display: grid;
  grid-template-columns: 90px minmax(0, 1fr);
  height: 100vh;
  background: var(--bg-base);
}

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
