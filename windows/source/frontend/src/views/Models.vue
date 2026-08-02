<script setup>
import { computed, ref } from "vue";
import Badge from "@/components/ui/Badge.vue";
import Button from "@/components/ui/Button.vue";
import Field from "@/components/ui/Field.vue";
import Modal from "@/components/ui/Modal.vue";
import SegmentedControl from "@/components/ui/SegmentedControl.vue";
import { state, saveModel, deleteModel } from "@/state/appState";

const PRESET_URL = {
  openai: "https://api.openai.com/v1/chat/completions",
  anthropic: "https://api.anthropic.com/v1/messages",
  custom: "",
};

const editorOpen = ref(false);
const editorError = ref("");
const saving = ref(false);
const isNew = ref(false);
const confirmDelete = ref(null);

const form = ref(emptyForm());

function emptyForm() {
  return {
    name: "",
    displayName: "",
    description: "",
    provider: "openai",
    apiKey: "",
    apiUrl: PRESET_URL.openai,
    externalModelName: "",
    reasoningEffort: "auto",
  };
}

const providerOptions = [
  { label: "OpenAI", value: "openai" },
  { label: "Anthropic", value: "anthropic" },
  { label: "自定义", value: "custom" },
];

const openAIReasoningOptions = [
  { label: "自动", value: "auto" },
  { label: "无", value: "none" },
  { label: "最小", value: "minimal" },
  { label: "低", value: "low" },
  { label: "中", value: "medium" },
  { label: "高", value: "high" },
  { label: "超高", value: "xhigh" },
  { label: "最大", value: "max" },
];

const anthropicReasoningOptions = openAIReasoningOptions.filter((option) =>
  ["auto", "low", "medium", "high"].includes(option.value)
);

const reasoningOptions = computed(() =>
  form.value.provider === "anthropic" ? anthropicReasoningOptions : openAIReasoningOptions
);

const displayNamePlaceholder = computed(() =>
  form.value.externalModelName?.trim() || "自动使用上游模型名"
);

function reasoningLabel(value) {
  return openAIReasoningOptions.find((option) => option.value === value)?.label || "自动";
}

function providerTone(p) {
  return p === "anthropic" ? "warn" : p === "openai" ? "info" : "neutral";
}

function providerLabel(p) {
  return p === "anthropic" ? "Anthropic" : p === "openai" ? "OpenAI" : "自定义";
}

function maskKey(k) {
  const t = String(k || "").trim();
  if (!t) return "—";
  if (t.length <= 10) return "•".repeat(t.length);
  return t.slice(0, 5) + "••••" + t.slice(-4);
}

function hostOf(url) {
  try {
    return new URL(url).host;
  } catch {
    return String(url || "").replace(/^https?:\/\//, "").split("/")[0] || "—";
  }
}

function openNew() {
  form.value = emptyForm();
  isNew.value = true;
  editorError.value = "";
  editorOpen.value = true;
}

function openEdit(m) {
  form.value = { ...m, reasoningEffort: m.reasoningEffort || "auto" };
  isNew.value = false;
  editorError.value = "";
  editorOpen.value = true;
}

function onProviderChange(v) {
  form.value.provider = v;
	if (v === "anthropic" && !anthropicReasoningOptions.some((option) => option.value === form.value.reasoningEffort)) {
		form.value.reasoningEffort = "auto";
	}
  if (!form.value.apiUrl || Object.values(PRESET_URL).includes(form.value.apiUrl)) {
    form.value.apiUrl = PRESET_URL[v] || "";
  }
}

async function handleSave() {
  editorError.value = "";
  const f = form.value;

  if (!f.externalModelName?.trim()) {
    editorError.value = "请填写上游模型名（externalModelName）";
    return;
  }
  if (!f.apiUrl?.trim()) {
    editorError.value = "请填写 API 地址";
    return;
  }
  if (!f.apiKey?.trim()) {
    editorError.value = "请填写 API Key";
    return;
  }

  // 自动补全字段
  if (!f.name?.trim()) {
    f.name = "models/" + f.externalModelName.trim().replace(/[^a-zA-Z0-9.-]+/g, "-");
  }
  if (!f.displayName?.trim()) {
    f.displayName = f.externalModelName.trim();
  }

  saving.value = true;
  try {
    const res = await saveModel({ ...f });
    if (res?.ok) {
      editorOpen.value = false;
    } else {
      editorError.value = res?.message || "保存失败";
    }
  } catch (e) {
    editorError.value = String(e?.message || e);
  } finally {
    saving.value = false;
  }
}

async function handleDelete() {
  const target = confirmDelete.value;
  if (!target) return;
  await deleteModel(target.name);
  confirmDelete.value = null;
}
</script>

<template>
  <div class="page fade-up">
    <!-- 顶部操作栏 -->
    <div class="row between" style="gap: 12px">
      <div class="col" style="gap: 2px">
        <div class="t-title">自定义模型</div>
        <div class="t-caption">
          共 {{ state.models.length }} 个模型 · 修改后需重启 Antigravity 生效
        </div>
      </div>
      <Button variant="filled" @click="openNew">
        <svg width="13" height="13" viewBox="0 0 13 13" fill="none">
          <path d="M6.5 2.5v8M2.5 6.5h8" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"/>
        </svg>
        新增模型
      </Button>
    </div>

    <!-- 空态 -->
    <div v-if="state.models.length === 0" class="empty">
      <div class="empty-icon">
        <svg width="26" height="26" viewBox="0 0 24 24" fill="none">
          <rect x="3" y="4" width="18" height="16" rx="3" stroke="currentColor" stroke-width="1.5"/>
          <path d="M8 10h8M8 14h5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>
        </svg>
      </div>
      <div class="t-headline">还没有配置模型</div>
      <div class="t-caption" style="margin-top: 4px">
        添加 OpenAI 兼容或 Anthropic 模型，即可在 Antigravity 中使用
      </div>
      <Button variant="tinted" style="margin-top: 14px" @click="openNew">添加第一个模型</Button>
    </div>

    <!-- 模型卡片网格 -->
    <div v-else class="grid">
      <div v-for="m in state.models" :key="m.name" class="model-card">
        <div class="row between" style="gap: 10px; align-items: flex-start">
          <div class="grow col" style="gap: 2px; min-width: 0">
            <div class="t-headline truncate">{{ m.displayName || m.name }}</div>
            <div class="mono truncate" style="color: var(--text-tertiary)">
              {{ m.externalModelName }}
            </div>
          </div>
          <Badge :tone="providerTone(m.provider)" :label="providerLabel(m.provider)" />
        </div>

        <div class="inset-group" style="margin-top: 12px">
          <div class="inset-row" style="min-height: 38px; padding: 8px 12px">
            <span class="t-footnote" style="width: 62px; flex-shrink: 0">HOST</span>
            <span class="mono truncate grow" style="color: var(--text-secondary)">
              {{ hostOf(m.apiUrl) }}
            </span>
          </div>
          <div class="inset-row" style="min-height: 38px; padding: 8px 12px">
            <span class="t-footnote" style="width: 62px; flex-shrink: 0">思考</span>
            <span class="mono grow" style="color: var(--text-secondary)">
              {{ reasoningLabel(m.reasoningEffort) }}
            </span>
          </div>
          <div class="inset-row" style="min-height: 38px; padding: 8px 12px">
            <span class="t-footnote" style="width: 62px; flex-shrink: 0">KEY</span>
            <span class="mono truncate grow" style="color: var(--text-secondary)">
              {{ maskKey(m.apiKey) }}
            </span>
          </div>
        </div>

        <div class="row" style="gap: 6px; margin-top: 12px; justify-content: flex-end">
          <Button variant="plain" size="sm" @click="openEdit(m)">编辑</Button>
          <Button variant="danger" size="sm" @click="confirmDelete = m">删除</Button>
        </div>
      </div>
    </div>

    <!-- 编辑器 -->
    <Modal
      :open="editorOpen"
      :title="isNew ? '新增模型' : '编辑模型'"
      wide
      @close="editorOpen = false"
    >
      <div class="col" style="gap: 14px">
        <div class="col" style="gap: 6px">
          <span class="t-footnote">供应商类型</span>
          <SegmentedControl
            :options="providerOptions"
            :model-value="form.provider"
            @update:model-value="onProviderChange"
          />
        </div>

        <Field
          label="上游模型名"
          hint="第三方 API 实际接受的模型 ID，例如 claude-fable-5 / gpt-5.6-sol"
          v-model="form.externalModelName"
          placeholder="claude-fable-5"
          mono
        />

        <div class="col" style="gap: 6px">
          <span class="t-footnote">思考强度</span>
		  <div class="reasoning-grid">
			<button
			  v-for="option in reasoningOptions"
			  :key="option.value"
			  type="button"
			  class="reasoning-option"
			  :class="{ active: form.reasoningEffort === option.value }"
			  @click="form.reasoningEffort = option.value"
			>
			  {{ option.label }}
			</button>
		  </div>
		  <span class="t-caption">
			自动不覆盖上游默认值；OpenAI 支持无、最小、低、中、高、超高、最大，具体可用等级由模型决定
		  </span>
        </div>

        <Field
          label="显示名称"
          hint="留空则使用上游模型名"
          v-model="form.displayName"
          :placeholder="displayNamePlaceholder"
        />

        <Field
          label="API 地址"
          v-model="form.apiUrl"
          placeholder="https://api.example.com/v1/chat/completions"
          mono
        />

        <Field
          label="API Key"
          type="password"
          v-model="form.apiKey"
          placeholder="sk-..."
          mono
        />

        <Field
          label="描述"
          hint="可选"
          v-model="form.description"
          placeholder=""
        />

        <div v-if="editorError" class="err-box">{{ editorError }}</div>
      </div>

      <template #footer>
        <Button variant="plain" @click="editorOpen = false">取消</Button>
        <Button variant="filled" :loading="saving" @click="handleSave">
          {{ isNew ? "添加" : "保存" }}
        </Button>
      </template>
    </Modal>

    <!-- 删除确认 -->
    <Modal
      :open="!!confirmDelete"
      title="确认删除"
      @close="confirmDelete = null"
    >
      <div class="t-body">
        确定删除模型
        <strong>{{ confirmDelete?.displayName || confirmDelete?.name }}</strong>
        吗？此操作不可撤销。
      </div>
      <template #footer>
        <Button variant="plain" @click="confirmDelete = null">取消</Button>
        <Button variant="danger" @click="handleDelete">删除</Button>
      </template>
    </Modal>
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

.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(272px, 1fr));
  gap: 12px;
}

.model-card {
  background: var(--bg-card);
  border: 1px solid var(--separator);
  border-radius: var(--r-lg);
  padding: 14px;
  transition: border-color 0.18s var(--ease), transform 0.18s var(--spring);
  box-shadow: var(--shadow-card);
  backdrop-filter: blur(16px);
}

.model-card:hover {
  border-color: var(--separator-strong);
  transform: translateY(-1px);
}

.empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  text-align: center;
  padding: 40px 20px;
  border: 1px dashed var(--separator-strong);
  border-radius: var(--r-lg);
  min-height: 240px;
}

.empty-icon {
  width: 52px;
  height: 52px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: var(--r-md);
  background: var(--bg-fill);
  color: var(--text-tertiary);
  margin-bottom: 14px;
}

.err-box {
  padding: 10px 12px;
  background: rgba(255, 69, 58, 0.1);
  border: 0.5px solid rgba(255, 69, 58, 0.25);
  border-radius: var(--r-sm);
  color: var(--red);
  font-size: 12.5px;
}

.reasoning-grid {
	display: grid;
	grid-template-columns: repeat(4, minmax(0, 1fr));
	gap: 5px;
}

.reasoning-option {
	height: 32px;
	border: 1px solid var(--separator);
	border-radius: var(--r-sm);
	background: var(--bg-inset);
	color: var(--text-secondary);
	font-size: 12px;
	transition: all 0.16s var(--ease);
}

.reasoning-option:hover,
.reasoning-option.active {
	border-color: var(--accent);
	color: var(--text-primary);
}

.reasoning-option.active {
	background: var(--accent-soft);
	box-shadow: 0 0 0 2px var(--accent-border);
}
</style>
