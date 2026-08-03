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
  >
    <span v-if="loading" class="loader spin"></span>
    <slot v-else />
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
  white-space: nowrap;
  transition: transform 0.14s var(--spring), background 0.16s var(--ease),
    opacity 0.16s var(--ease);
  user-select: none;
}

.btn:active:not(.disabled) {
  transform: scale(0.96);
}

.s-md {
  height: 32px;
  padding: 0 14px;
  font-size: 14px;
}

.s-sm {
  height: 26px;
  padding: 0 10px;
  font-size: 12.5px;
  border-radius: 7px;
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
