<script setup>
import { computed, nextTick, ref, watch } from "vue";
import Modal from "@/components/ui/Modal.vue";
import Button from "@/components/ui/Button.vue";

const DEFAULT_IMAGE_PROMPT = "Generate a cute orange cat astronaut sticker on a clean pastel background.";

const props = defineProps({
  open: Boolean,
  account: { type: Object, default: null },
  // A model can be a string or an object with id/name/displayName/display_name.
  models: { type: Array, default: () => [] },
  loadingModels: Boolean,
  status: { type: String, default: "idle" }, // idle | connecting | success | error
  outputLines: { type: Array, default: () => [] },
  streamingContent: { type: String, default: "" },
  errorMessage: { type: String, default: "" },
  generatedImages: { type: Array, default: () => [] },
  defaultModelId: { type: String, default: "" },
  defaultPrompt: { type: String, default: "hi" },
  defaultTestMode: { type: String, default: "default" },
  testModes: {
    type: Array,
    default: () => [
      { value: "default", label: "常规请求" },
      { value: "compact", label: "Compact 请求" },
    ],
  },
  // Supplying false deliberately hides the prompt editor for integrations
  // that only support a fixed provider-side probe.
  showPrompt: { type: Boolean, default: true },
  // No Boolean type here: an absent Boolean prop would otherwise be coerced
  // to false and accidentally suppress OpenAI's normal/compact selector.
  showTestMode: { default: null },
  allowCopy: { type: Boolean, default: true },
});

const emit = defineEmits([
  "close",
  "cancel",
  "test",
  "retry",
  "copy-output",
  "preview-image",
  "update:model-id",
  "update:prompt",
  "update:test-mode",
]);

const terminalRef = ref(null);
const selectedModelId = ref("");
const testPrompt = ref("hi");
const testMode = ref("default");
const copied = ref(false);

function modelID(model) {
  if (typeof model === "string") return model;
  return String(model?.id ?? model?.model ?? model?.value ?? "");
}

function modelLabel(model) {
  if (typeof model === "string") return model;
  return String(model?.displayName ?? model?.display_name ?? model?.name ?? modelID(model));
}

const normalizedModels = computed(() => props.models
  .map((model) => ({ id: modelID(model), label: modelLabel(model) }))
  .filter((model) => model.id));

const selectedModel = computed(() => normalizedModels.value.find((model) => model.id === selectedModelId.value));
const selectedModelLooksLikeImage = computed(() => /(?:^|[-_/])(?:gpt-)?image(?:[-_/]|$)|image[-_]?gen/i.test(selectedModelId.value));
const isOpenAI = computed(() => /openai|codex/i.test(String(props.account?.provider ?? props.account?.platform ?? "")));
const shouldShowTestMode = computed(() => props.showTestMode === null ? isOpenAI.value : Boolean(props.showTestMode));
const promptLabel = computed(() => selectedModelLooksLikeImage.value ? "生图提示词" : "测试提示词");
const promptHint = computed(() => selectedModelLooksLikeImage.value
  ? "选择图片模型后会发送真实的生图请求，并在下方显示返回图片。"
  : "默认发送 “hi”；可修改为用于验证上游链路的短提示词。"
);
const busy = computed(() => props.status === "connecting");
const terminalLines = computed(() => props.outputLines.map((line) => {
  if (typeof line === "string") return { text: line, tone: "muted" };
  return { text: String(line?.text ?? ""), tone: String(line?.tone ?? line?.class ?? "muted") };
}));

function normalizePrompt(modelIDValue, currentPrompt) {
  const imageModel = /(?:^|[-_/])(?:gpt-)?image(?:[-_/]|$)|image[-_]?gen/i.test(modelIDValue);
  if (imageModel && (!currentPrompt || currentPrompt === "hi" || currentPrompt === props.defaultPrompt)) return DEFAULT_IMAGE_PROMPT;
  if (!imageModel && currentPrompt === DEFAULT_IMAGE_PROMPT) return props.defaultPrompt || "hi";
  return currentPrompt;
}

function initialize() {
  const validDefault = normalizedModels.value.some((model) => model.id === props.defaultModelId);
  selectedModelId.value = validDefault ? props.defaultModelId : (normalizedModels.value[0]?.id || "");
  testPrompt.value = normalizePrompt(selectedModelId.value, props.defaultPrompt || "hi");
  testMode.value = props.testModes.some((mode) => mode?.value === props.defaultTestMode)
    ? props.defaultTestMode
    : (props.testModes[0]?.value || "default");
  copied.value = false;
}

async function scrollTerminalToBottom() {
  await nextTick();
  if (terminalRef.value) terminalRef.value.scrollTop = terminalRef.value.scrollHeight;
}

watch(() => props.open, (open) => {
  if (open) {
    initialize();
    void scrollTerminalToBottom();
  }
});

watch(() => props.models, () => {
  if (!props.open) return;
  if (!normalizedModels.value.some((model) => model.id === selectedModelId.value)) initialize();
}, { deep: true });

watch(() => [props.outputLines, props.streamingContent, props.status], () => {
  if (props.open) void scrollTerminalToBottom();
}, { deep: true });

function setModel(value) {
  selectedModelId.value = value;
  testPrompt.value = normalizePrompt(value, testPrompt.value);
  emit("update:model-id", value);
  emit("update:prompt", testPrompt.value);
}

function setPrompt(value) {
  testPrompt.value = value;
  emit("update:prompt", value);
}

function setMode(value) {
  testMode.value = value;
  emit("update:test-mode", value);
}

function testPayload() {
  return {
    accountId: props.account?.id ?? "",
    account: props.account,
    modelId: selectedModelId.value,
    model: selectedModel.value ?? null,
    prompt: testPrompt.value.trim() || "hi",
    mode: testMode.value,
    image: selectedModelLooksLikeImage.value,
  };
}

function startTest() {
  if (busy.value || !selectedModelId.value) return;
  const payload = testPayload();
  // A retry is the same native operation with the same complete payload.
  // Keeping one canonical event means a host only needs `@test` and cannot
  // accidentally launch the upstream probe twice by binding both handlers.
  emit("test", payload);
}

function close() {
  if (busy.value) emit("cancel", testPayload());
  emit("close");
}

function toneClass(tone) {
  const value = String(tone || "").toLowerCase();
  if (value.includes("red") || value.includes("error") || value.includes("fail")) return "is-error";
  if (value.includes("green") || value.includes("success") || value.includes("ok")) return "is-success";
  if (value.includes("yellow") || value.includes("orange") || value.includes("warn")) return "is-warning";
  if (value.includes("blue") || value.includes("cyan") || value.includes("info")) return "is-info";
  if (value.includes("purple")) return "is-purple";
  return "is-muted";
}

function imageURL(image) {
  return typeof image === "string" ? image : String(image?.url ?? image?.image_url ?? "");
}

async function copyOutput() {
  const output = [
    ...terminalLines.value.map((line) => line.text),
    props.streamingContent,
  ].filter(Boolean).join("\n");
  if (!output) return;
  try {
    if (navigator?.clipboard?.writeText) await navigator.clipboard.writeText(output);
    copied.value = true;
    window.setTimeout(() => { copied.value = false; }, 1600);
  } catch {
    // The host can still copy through its native bridge by handling this event.
  }
  emit("copy-output", output);
}
</script>

<template>
  <Modal :open="open" title="测试账号连接" wide persistent @close="close">
    <section class="account-test" aria-live="polite">
      <div v-if="account" class="account-card">
        <div class="play-icon" aria-hidden="true">
          <svg viewBox="0 0 24 24"><path d="M8 5.4v13.2L18.4 12 8 5.4Z" /></svg>
        </div>
        <div class="account-copy">
          <div class="account-name truncate">{{ account.name || account.identity?.email || "未命名账户" }}</div>
          <div class="account-meta">
            <span class="type-chip">{{ account.type || "account" }}</span>
            <span>账号</span>
          </div>
        </div>
        <span class="state-chip" :class="{ inactive: account.enabled === false || account.status === 'inactive' }">
          {{ account.enabled === false || account.status === "inactive" ? "inactive" : "active" }}
        </span>
      </div>

      <label class="form-label" for="account-test-model">选择测试模型</label>
      <div class="select-shell" :class="{ disabled: busy || loadingModels }">
        <select
          id="account-test-model"
          :value="selectedModelId"
          :disabled="busy || loadingModels || !normalizedModels.length"
          @change="setModel($event.target.value)"
        >
          <option value="" disabled>{{ loadingModels ? "正在读取可用模型…" : "请选择测试模型" }}</option>
          <option v-for="model in normalizedModels" :key="model.id" :value="model.id">{{ model.label }}</option>
        </select>
        <svg class="chevron" viewBox="0 0 20 20" aria-hidden="true"><path d="m4 7 6 6 6-6" /></svg>
      </div>

      <template v-if="shouldShowTestMode">
        <label class="form-label" for="account-test-mode">测试模式</label>
        <div class="select-shell" :class="{ disabled: busy }">
          <select id="account-test-mode" :value="testMode" :disabled="busy" @change="setMode($event.target.value)">
            <option v-for="mode in testModes" :key="mode.value" :value="mode.value">{{ mode.label }}</option>
          </select>
          <svg class="chevron" viewBox="0 0 20 20" aria-hidden="true"><path d="m4 7 6 6 6-6" /></svg>
        </div>
      </template>

      <template v-if="showPrompt">
        <label class="form-label" for="account-test-prompt">{{ promptLabel }}</label>
        <textarea
          id="account-test-prompt"
          :value="testPrompt"
          :disabled="busy"
          rows="2"
          :placeholder="selectedModelLooksLikeImage ? '例如：生成一只戴宇航员头盔的橘猫，像素插画风格，纯色背景。' : 'hi'"
          @input="setPrompt($event.target.value)"
        />
        <p class="input-hint">{{ promptHint }}</p>
      </template>

      <div class="terminal-wrap">
        <button v-if="allowCopy && (terminalLines.length || streamingContent)" class="copy-output" type="button" @click="copyOutput">
          {{ copied ? "已复制" : "复制输出" }}
        </button>
        <div ref="terminalRef" class="terminal" role="log" aria-label="账户测试输出">
          <div v-if="status === 'idle' && !terminalLines.length" class="terminal-status muted-line">
            <span class="terminal-mark">▶</span> 准备测试。点击“开始测试”按钮开始…
          </div>
          <div v-else-if="status === 'connecting' && !terminalLines.length" class="terminal-status warning-line">
            <span class="spinner" /> 连接 API 中…
          </div>
          <div v-for="(line, index) in terminalLines" :key="`${index}-${line.text}`" class="terminal-line" :class="toneClass(line.tone)">
            {{ line.text }}
          </div>
          <div v-if="streamingContent" class="terminal-line is-success">{{ streamingContent }}<span class="cursor">_</span></div>
          <div v-if="status === 'success'" class="terminal-result result-success">✓ 测试完成！</div>
          <div v-else-if="status === 'error'" class="terminal-result result-error">× {{ errorMessage || "测试失败" }}</div>
        </div>
      </div>

      <div v-if="generatedImages.length" class="images-area">
        <div class="images-title">生成结果：</div>
        <div class="images-grid">
          <button
            v-for="(image, index) in generatedImages"
            :key="`${imageURL(image)}-${index}`"
            class="image-card"
            type="button"
            @click="emit('preview-image', image)"
          >
            <img :src="imageURL(image)" :alt="`测试图片 ${index + 1}`" />
            <span>{{ image?.mimeType || image?.mime_type || "image/*" }}</span>
          </button>
        </div>
      </div>

      <div class="test-footnote">
        <span>▦ 测试模型</span>
        <span>{{ selectedModelLooksLikeImage ? "◉ 模式：生图测试" : `◌ 提示词：“${(testPrompt || "hi").slice(0, 38)}${(testPrompt || "hi").length > 38 ? "…" : ""}”` }}</span>
      </div>
    </section>

    <template #footer>
      <Button variant="plain" @click="close">{{ busy ? "取消" : "关闭" }}</Button>
      <Button :variant="status === 'success' ? 'filled' : 'filled'" :disabled="busy || !selectedModelId" :loading="busy" @click="startTest">
        <span v-if="!busy" aria-hidden="true">{{ status === "idle" ? "▶" : "↻" }}</span>
        {{ busy ? "测试中…" : status === "idle" ? "开始测试" : "重试" }}
      </Button>
    </template>
  </Modal>
</template>

<style scoped>
.account-test { display: grid; gap: 14px; }
.account-card { display: flex; align-items: center; gap: 12px; min-height: 76px; padding: 12px; border: 1px solid rgba(142, 166, 203, .52); border-radius: 18px; background: linear-gradient(115deg, rgba(70, 91, 121, .92), rgba(75, 91, 116, .78)); box-shadow: inset 0 1px rgba(255,255,255,.1); }
.play-icon { width: 48px; height: 48px; display: grid; place-items: center; flex: 0 0 auto; border-radius: 13px; color: #fff; background: #1d9cdf; }
.play-icon svg { width: 27px; height: 27px; fill: none; stroke: currentColor; stroke-width: 1.6; }
.play-icon path { fill: none; }
.account-copy { min-width: 0; flex: 1; }
.account-name { color: #f8fafc; font-size: 17px; font-weight: 700; letter-spacing: -.015em; }
.account-meta { display: flex; align-items: center; gap: 8px; margin-top: 4px; color: rgba(226, 232, 240, .66); font-size: 12px; }
.type-chip { padding: 2px 7px; border-radius: 5px; background: rgba(165, 181, 208, .55); color: #e8eff9; font-size: 11px; font-weight: 680; letter-spacing: .04em; text-transform: uppercase; }
.state-chip { padding: 7px 13px; border-radius: 999px; background: rgba(33, 183, 106, .2); color: #54e492; font-size: 13px; font-weight: 760; }
.state-chip.inactive { background: var(--bg-fill); color: var(--text-secondary); }
.form-label { color: var(--text-primary); font-size: 14px; font-weight: 660; margin-bottom: -7px; }
.select-shell { position: relative; }
.select-shell select, textarea { width: 100%; border: 1px solid rgba(130, 152, 184, .36); border-radius: 16px; color: var(--text-primary); background: rgba(4, 15, 33, .44); font: inherit; transition: border-color .16s var(--ease), box-shadow .16s var(--ease); }
.select-shell select { height: 52px; appearance: none; padding: 0 45px 0 16px; font-size: 15px; font-weight: 590; }
textarea { resize: vertical; min-height: 58px; padding: 11px 13px; line-height: 1.48; font-size: 13px; }
.select-shell select:focus, textarea:focus { outline: none; border-color: var(--blue); box-shadow: 0 0 0 3px rgba(93,165,255,.13); }
.select-shell.disabled { opacity: .58; }
.chevron { position: absolute; right: 16px; top: 50%; width: 21px; height: 21px; pointer-events: none; fill: none; stroke: var(--text-secondary); stroke-width: 1.8; transform: translateY(-50%); }
.input-hint { margin: -8px 3px 0; color: var(--text-tertiary); font-size: 11.5px; line-height: 1.45; }
.terminal-wrap { position: relative; }
.terminal { max-height: 246px; min-height: 137px; overflow-y: auto; padding: 15px 16px; border: 1px solid rgba(80, 105, 137, .72); border-radius: 18px; color: #cbd5e1; background: #030608; box-shadow: inset 0 1px 0 rgba(255,255,255,.04); font-family: var(--font-num); font-size: 13px; line-height: 1.55; }
.terminal-line { white-space: pre-wrap; overflow-wrap: anywhere; }
.terminal-status { display: flex; align-items: center; gap: 8px; }
.terminal-mark { color: var(--blue); }.spinner { display: inline-block; width: 12px; height: 12px; border: 2px solid #f6c452; border-top-color: transparent; border-radius: 50%; animation: terminal-spin .75s linear infinite; }
.is-muted, .muted-line { color: #a7b1bf; }.is-success, .result-success { color: #54e492; }.is-error, .result-error { color: #ff7970; }.is-warning, .warning-line { color: #f6cb4d; }.is-info { color: #5cb5ff; }.is-purple { color: #cf8fff; }
.cursor { display: inline-block; margin-left: 1px; animation: cursor-blink .85s step-end infinite; }
.terminal-result { display: flex; gap: 7px; margin-top: 13px; padding-top: 12px; border-top: 1px solid rgba(125, 151, 183, .42); font-size: 14px; font-weight: 600; }
.copy-output { position: absolute; z-index: 1; right: 9px; top: 8px; padding: 5px 8px; border: 1px solid rgba(135,155,185,.18); border-radius: 7px; color: #b3c1d4; background: rgba(23, 36, 56, .88); font-size: 11px; opacity: 0; transition: opacity .16s var(--ease), color .16s var(--ease); }
.terminal-wrap:hover .copy-output, .copy-output:focus-visible { opacity: 1; }.copy-output:hover { color: #fff; }
.images-area { display: grid; gap: 8px; }.images-title { color: var(--text-secondary); font-size: 12px; font-weight: 650; }.images-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(135px, 1fr)); gap: 10px; }
.image-card { min-width: 0; overflow: hidden; border: 1px solid var(--separator-strong); border-radius: 12px; color: var(--text-secondary); background: var(--bg-inset); text-align: left; }.image-card img { display: block; width: 100%; max-height: 180px; object-fit: cover; }.image-card span { display: block; padding: 6px 8px; font: 11px var(--font-num); }
.test-footnote { display: flex; justify-content: space-between; gap: 12px; color: var(--text-tertiary); font-size: 11.5px; }.test-footnote span:last-child { text-align: right; }
@keyframes terminal-spin { to { transform: rotate(360deg); } } @keyframes cursor-blink { 50% { opacity: 0; } }
@media (max-width: 480px) { .account-card { align-items: flex-start; }.state-chip { padding: 5px 8px; font-size: 11px; }.test-footnote { flex-direction: column; gap: 3px; }.test-footnote span:last-child { text-align: left; } }
</style>
