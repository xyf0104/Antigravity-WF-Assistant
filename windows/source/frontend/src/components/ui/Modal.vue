<script setup>
defineProps({
  open: Boolean,
  title: String,
  wide: Boolean,
  // Persistent sheets intentionally ignore clicks on the dimmed background.
  // Model credentials can be expensive to re-enter, so the editor uses this.
  persistent: Boolean,
	closable: { type: Boolean, default: true },
});
defineEmits(["close"]);
</script>

<template>
  <Teleport to="body">
    <Transition name="mask">
      <div
        v-if="open"
        class="mask"
        role="presentation"
        @mousedown.self.prevent
        @click.self.prevent
      >
        <Transition name="sheet" appear>
          <div class="sheet" :class="{ wide }" role="dialog" aria-modal="true" :aria-label="title" @click.stop>
            <header class="head">
              <div class="t-headline">{{ title }}</div>
				  <button v-if="closable" type="button" class="x" aria-label="关闭" @click="$emit('close')">
                <svg width="15" height="15" viewBox="0 0 15 15" fill="none">
                  <path
                    d="M3.5 3.5L11.5 11.5M11.5 3.5L3.5 11.5"
                    stroke="currentColor"
                    stroke-width="1.6"
                    stroke-linecap="round"
                  />
                </svg>
              </button>
            </header>
            <div class="body">
              <slot />
            </div>
            <footer v-if="$slots.footer" class="foot">
              <slot name="footer" />
            </footer>
          </div>
        </Transition>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.mask {
  position: fixed;
  inset: 0;
  z-index: 2147483000;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.52);
  backdrop-filter: blur(18px) saturate(160%);
  -webkit-backdrop-filter: blur(18px) saturate(160%);
  padding: 24px;
  pointer-events: auto;
  isolation: isolate;
}

.sheet {
  width: 100%;
  max-width: 420px;
  max-height: 84vh;
  display: flex;
  flex-direction: column;
  background: var(--modal-bg);
  backdrop-filter: blur(40px) saturate(180%);
  -webkit-backdrop-filter: blur(40px) saturate(180%);
  border: 1px solid var(--separator-strong);
  border-radius: var(--r-xl);
  box-shadow: 0 24px 68px rgba(4, 10, 20, 0.34);
  overflow: hidden;
}

.sheet.wide {
  max-width: 560px;
}

.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 15px 16px;
  border-bottom: 0.5px solid var(--separator);
  flex-shrink: 0;
}

.x {
  width: 26px;
  height: 26px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  background: var(--bg-fill);
  color: var(--text-secondary);
	  transition: background-color 0.16s var(--ease), color 0.16s var(--ease);
  flex-shrink: 0;
}

.x:hover {
  background: var(--bg-fill-hover);
  color: var(--text-primary);
}

.body {
  padding: 16px;
  overflow-y: auto;
  flex: 1;
  min-height: 0;
}

.foot {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding: 13px 16px;
  border-top: 0.5px solid var(--separator);
  flex-shrink: 0;
}
</style>
