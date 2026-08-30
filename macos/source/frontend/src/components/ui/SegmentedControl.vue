<script setup>
defineProps({
  options: { type: Array, required: true }, // [{ label, value }]
  modelValue: [String, Number],
});
defineEmits(["update:modelValue"]);
</script>

<template>
  <div class="seg">
    <button
      v-for="opt in options"
      :key="opt.value"
	  type="button"
      class="item"
      :class="{ active: modelValue === opt.value }"
      @click="$emit('update:modelValue', opt.value)"
    >
      {{ opt.label }}
    </button>
  </div>
</template>

<style scoped>
.seg {
  display: inline-flex;
  padding: 2px;
  background: var(--bg-inset);
  border-radius: 9px;
  gap: 2px;
  border: 1px solid var(--separator);
}

.item {
  padding: 5px 15px;
  border-radius: 7px;
  font-size: 12.5px;
  font-weight: 550;
  color: var(--text-secondary);
	  transition: background-color 0.2s var(--ease), color 0.2s var(--ease), box-shadow 0.2s var(--ease);
  white-space: nowrap;
}

.item:hover:not(.active) {
  color: var(--text-primary);
}

.item.active {
  background: var(--bg-card-hover);
  color: var(--text-primary);
  box-shadow: var(--shadow-card);
}

@media (max-width: 560px) {
  .seg {
    display: grid;
    width: 100%;
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .item {
    min-width: 0;
    min-height: 40px;
    padding: 7px 9px;
    white-space: normal;
    overflow-wrap: anywhere;
  }

  .item:last-child:nth-child(odd) {
    grid-column: 1 / -1;
  }
}
</style>
