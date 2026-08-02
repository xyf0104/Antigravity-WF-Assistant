<script setup>
defineProps({
  title: String,
  subtitle: String,
  padded: { type: Boolean, default: true },
});
</script>

<template>
  <section class="card">
    <header v-if="title || $slots.action" class="head">
      <div class="grow col">
        <div v-if="title" class="t-headline">{{ title }}</div>
        <div v-if="subtitle" class="t-caption">{{ subtitle }}</div>
      </div>
      <slot name="action" />
    </header>
    <div :class="{ body: padded, 'body-flush': !padded }">
      <slot />
    </div>
  </section>
</template>

<style scoped>
.card {
  background: var(--bg-card);
  border: 1px solid var(--separator);
  border-radius: var(--r-lg);
  overflow: hidden;
  box-shadow: var(--shadow-card);
  backdrop-filter: blur(18px) saturate(125%);
  -webkit-backdrop-filter: blur(18px) saturate(125%);
}

.head {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 14px 16px 0;
}

.body {
  padding: 14px 16px 16px;
}

.body-flush {
  padding: 0;
}

.head + .body-flush {
  margin-top: 12px;
}
</style>
