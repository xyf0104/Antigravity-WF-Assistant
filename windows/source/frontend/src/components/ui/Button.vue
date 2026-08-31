<script setup>
defineProps({
  variant: { type: String, default: "plain" }, // plain | filled | tinted | danger
  size: { type: String, default: "md" }, // sm | md
  disabled: Boolean,
  loading: Boolean,
});
</script>

<template>
  <button
    class="btn"
    :class="[`v-${variant}`, `s-${size}`, { disabled: disabled || loading }]"
    :disabled="disabled || loading"
    :aria-busy="loading || undefined"
  >
    <span v-if="loading" class="loader spin" aria-hidden="true"></span>
    <span class="btn-label"><slot /></span>
  </button>
</template>

<style scoped>
.btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  border-radius: var(--r-sm);
  font-weight: 550;
  min-width: max-content;
  max-width: 100%;
  line-height: 1.3;
  white-space: nowrap;
  overflow-wrap: normal;
  transition: transform 0.14s var(--spring), background 0.16s var(--ease),
    opacity 0.16s var(--ease);
  user-select: none;
}

.btn:active:not(.disabled) {
  transform: scale(0.96);
}

.btn-label {
  display: inline-flex;
  min-width: 0;
  align-items: center;
  justify-content: center;
}

.s-md {
  min-height: 44px;
  height: auto;
  padding: 8px 14px;
  font-size: 14px;
}

.s-sm {
  min-height: 44px;
  height: auto;
  padding: 8px 10px;
  font-size: 13px;
  border-radius: var(--r-sm);
}

.v-plain {
  background: var(--bg-fill);
  color: var(--text-primary);
}

.v-plain:hover:not(.disabled) {
  background: var(--bg-fill-hover);
}

.v-filled {
  background: var(--accent);
  color: #fff;
  box-shadow: 0 5px 14px var(--accent-soft);
}

.v-filled:hover:not(.disabled) {
  background: var(--accent-hover);
}

.v-tinted {
  background: var(--accent-soft);
  color: var(--accent-strong);
}

.v-tinted:hover:not(.disabled) {
  background: var(--accent-border);
}

.v-danger {
  background: rgba(255, 69, 58, 0.14);
  color: var(--red);
}

.v-danger:hover:not(.disabled) {
  background: rgba(255, 69, 58, 0.22);
}

.disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.loader {
  width: 13px;
  height: 13px;
  border: 1.8px solid currentColor;
  border-top-color: transparent;
  border-radius: 50%;
}
</style>
