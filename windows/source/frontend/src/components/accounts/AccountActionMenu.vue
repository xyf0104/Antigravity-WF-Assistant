<script setup>
import { computed, onUnmounted, watch } from "vue";

const props = defineProps({
  show: Boolean,
  account: { type: Object, default: null },
  position: { type: Object, default: null },
  // These are tri-state overrides. Do not declare them as Boolean: Vue would
  // coerce an omitted prop to false and suppress the XIASS-compatible inferred
  // account actions.
  canDuplicate: { default: null },
  canCreateSparkShadow: { default: null },
  canSetPrivacy: { default: null },
  hasRecoverableState: { default: null },
  hasQuotaLimit: { default: null },
});

const emit = defineEmits([
  "close",
  "test",
  "stats",
  "schedule",
  "duplicate",
  "reauth",
  "refresh-token",
  "create-spark-shadow",
  "set-privacy",
  "recover-state",
  "reset-quota",
]);

const type = computed(() => String(props.account?.type || "").toLowerCase());
const provider = computed(() => String(props.account?.provider ?? props.account?.platform ?? "").toLowerCase());
const isOAuth = computed(() => type.value === "oauth" || type.value === "setup-token" || type.value === "setup_token");
const isShadow = computed(() => Boolean(props.account?.parentAccountId ?? props.account?.parent_account_id));
const isOpenAIOAuth = computed(() => isOAuth.value && /openai|codex/.test(provider.value));
const inferredCanDuplicate = computed(() => !isShadow.value && ["api_key", "apikey", "upstream", "bedrock", "service_account"].includes(type.value));
const inferredRecoverable = computed(() => {
  const cooldown = Date.parse(props.account?.cooldownUntil ?? props.account?.cooldown_until ?? "");
  return props.account?.status === "error" || Boolean(props.account?.lastError ?? props.account?.last_error)
    || (Number.isFinite(cooldown) && cooldown > Date.now());
});
const inferredQuotaLimit = computed(() => Boolean(props.account?.quota?.available ?? props.account?.quotaUrl ?? props.account?.quota_url));
const actionPosition = computed(() => ({
  top: Math.max(8, Number(props.position?.top) || 0),
  left: Math.max(8, Number(props.position?.left) || 0),
}));

function dispatch(name) {
  emit(name, props.account);
  emit("close");
}

function handleKeydown(event) {
  if (event.key === "Escape") emit("close");
}

watch(() => props.show, (visible) => {
  if (visible) window.addEventListener("keydown", handleKeydown);
  else window.removeEventListener("keydown", handleKeydown);
}, { immediate: true });

onUnmounted(() => window.removeEventListener("keydown", handleKeydown));
</script>

<template>
  <Teleport to="body">
    <div v-if="show && position" class="action-layer" @mousedown.self="emit('close')">
      <section class="action-menu" :style="{ top: `${actionPosition.top}px`, left: `${actionPosition.left}px` }" role="menu" aria-label="账户操作" @mousedown.stop>
        <button type="button" role="menuitem" class="action green" @click="dispatch('test')"><span class="action-icon">▷</span>测试连接</button>
        <button type="button" role="menuitem" class="action indigo" @click="dispatch('stats')"><span class="action-icon">▥</span>查看统计</button>
        <button type="button" role="menuitem" class="action orange" @click="dispatch('schedule')"><span class="action-icon">◷</span>定时测试</button>
        <button v-if="canDuplicate ?? inferredCanDuplicate" type="button" role="menuitem" class="action blue" @click="dispatch('duplicate')"><span class="action-icon">⧉</span>复制账号</button>
        <template v-if="isOAuth && !isShadow">
          <button type="button" role="menuitem" class="action blue" @click="dispatch('reauth')"><span class="action-icon">⛓</span>重新授权</button>
          <button type="button" role="menuitem" class="action purple" @click="dispatch('refresh-token')"><span class="action-icon">⟳</span>刷新令牌</button>
        </template>
        <button v-if="canCreateSparkShadow ?? (isOpenAIOAuth && !isShadow)" type="button" role="menuitem" class="action amber" @click="dispatch('create-spark-shadow')"><span class="action-icon">✦</span>创建 Spark 影子账号</button>
        <button v-if="canSetPrivacy ?? (isOAuth && !isShadow)" type="button" role="menuitem" class="action emerald" @click="dispatch('set-privacy')"><span class="action-icon">♢</span>设置隐私</button>
        <div v-if="(hasRecoverableState ?? inferredRecoverable) || (hasQuotaLimit ?? inferredQuotaLimit)" class="divider" />
        <button v-if="hasRecoverableState ?? inferredRecoverable" type="button" role="menuitem" class="action emerald" @click="dispatch('recover-state')"><span class="action-icon">⟳</span>恢复可调度状态</button>
        <button v-if="hasQuotaLimit ?? inferredQuotaLimit" type="button" role="menuitem" class="action teal" @click="dispatch('reset-quota')"><span class="action-icon">↻</span>重置额度状态</button>
      </section>
    </div>
  </Teleport>
</template>

<style scoped>
.action-layer { position: fixed; inset: 0; z-index: 2147483100; }
.action-menu { position: fixed; z-index: 1; width: 260px; padding: 10px 0; overflow: hidden; border: 1px solid rgba(112,138,178,.22); border-radius: 19px; background: rgba(7, 20, 39, .98); box-shadow: 0 18px 48px rgba(0, 0, 0, .36), inset 0 1px rgba(255,255,255,.04); backdrop-filter: blur(28px) saturate(155%); -webkit-backdrop-filter: blur(28px) saturate(155%); }
.action { display: flex; align-items: center; width: 100%; min-height: 42px; gap: 14px; padding: 7px 20px; border: 0; color: #e8eef8; background: transparent; font: 17px/1.2 var(--font-ui); text-align: left; transition: background .14s var(--ease), transform .14s var(--ease); }
.action:hover, .action:focus-visible { outline: none; background: rgba(141, 167, 208, .11); }.action:active { transform: scale(.985); }
.action-icon { width: 25px; text-align: center; font-size: 26px; font-family: system-ui, sans-serif; font-weight: 400; line-height: 1; }
.green .action-icon { color: #2ee281; }.indigo .action-icon { color: #7185ff; }.orange .action-icon { color: #ff8a19; }.blue .action-icon { color: #428bff; }.purple .action-icon { color: #a74aff; }.amber .action-icon { color: #f2a016; }.emerald .action-icon { color: #20bf87; }.teal .action-icon { color: #25d1c6; }
.divider { height: 1px; margin: 7px 14px; background: rgba(125, 150, 186, .2); }
@media (max-width: 420px) { .action-menu { width: min(260px, calc(100vw - 16px)); }.action { font-size: 16px; } }
</style>
