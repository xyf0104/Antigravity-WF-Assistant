<script setup>
defineProps({
  tone: { type: String, default: "neutral" }, // ok | warn | err | neutral | info
  pulse: Boolean,
  label: String,
});
</script>

<template>
  <span class="badge" :class="`t-${tone}`">
    <span class="dot" :class="{ pulse }"></span>
    <span>{{ label }}</span>
  </span>
</template>

<style scoped>
.badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 10px 4px 8px;
  border-radius: var(--r-full);
  font-size: 11.5px;
  font-weight: 550;
  white-space: nowrap;
}

.dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  flex-shrink: 0;
}

.t-ok {
  background: rgba(48, 209, 88, 0.14);
  color: var(--green);
}
.t-ok .dot {
  background: var(--green);
  box-shadow: 0 0 6px rgba(48, 209, 88, 0.7);
}

.t-warn {
  background: rgba(255, 159, 10, 0.14);
  color: var(--orange);
}
.t-warn .dot {
  background: var(--orange);
}

.t-err {
  background: rgba(255, 69, 58, 0.14);
  color: var(--red);
}
.t-err .dot {
  background: var(--red);
}

.t-info {
  background: var(--blue-soft);
  color: var(--blue);
}
.t-info .dot {
  background: var(--blue);
}

.t-neutral {
  background: var(--bg-fill);
  color: var(--text-secondary);
}
.t-neutral .dot {
  background: var(--text-tertiary);
}
</style>
