<script setup>
defineProps({
  label: String,
  hint: String,
  modelValue: [String, Number],
  placeholder: String,
  type: { type: String, default: "text" },
  mono: Boolean,
});
defineEmits(["update:modelValue"]);
</script>

<template>
  <label class="field">
    <span v-if="label" class="t-footnote">{{ label }}</span>
    <input
      :type="type"
      :value="modelValue"
      :placeholder="placeholder"
      :class="{ mono }"
      spellcheck="false"
      autocomplete="off"
      @input="$emit('update:modelValue', $event.target.value)"
    />
    <span v-if="hint" class="t-caption hint">{{ hint }}</span>
  </label>
</template>

<style scoped>
.field {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

input {
  height: 34px;
  padding: 0 12px;
  background: var(--bg-inset);
  border: 0.5px solid var(--separator-strong);
  border-radius: var(--r-sm);
  color: var(--text-primary);
  font-size: 13px;
  transition: border-color 0.16s var(--ease), box-shadow 0.16s var(--ease);
  width: 100%;
}

input:focus {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-soft);
}

input.mono {
  font-family: var(--font-num);
  font-size: 11.5px;
}

.hint {
  font-size: 10.5px;
  color: var(--text-tertiary);
}
</style>
