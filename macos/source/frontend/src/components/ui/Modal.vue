<script setup>
import { nextTick, onBeforeUnmount, ref, watch } from "vue";

const props = defineProps({
  open: Boolean,
  title: String,
  wide: Boolean,
  // Persistent sheets intentionally ignore clicks on the dimmed background.
  // Model credentials can be expensive to re-enter, so the editor uses this.
  persistent: Boolean,
	inline: Boolean,
	closable: { type: Boolean, default: true },
});
const emit = defineEmits(["close"]);
const sheetRef = ref(null);
let previousFocus = null;

const focusableSelector = [
  'button:not([disabled])',
  'a[href]',
  'input:not([disabled])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',');

function focusableElements() {
  return Array.from(sheetRef.value?.querySelectorAll(focusableSelector) || [])
    .filter((element) => element.getClientRects().length > 0);
}

function handleDocumentKeydown(event) {
  if (!props.open || props.inline) return;
  if (event.key === "Escape" && props.closable) {
    event.preventDefault();
    emit("close");
    return;
  }
  if (event.key !== "Tab") return;

  const elements = focusableElements();
  if (!elements.length) {
    event.preventDefault();
    sheetRef.value?.focus();
    return;
  }

  const first = elements[0];
  const last = elements[elements.length - 1];
  if (event.shiftKey && (document.activeElement === first || document.activeElement === sheetRef.value)) {
    event.preventDefault();
    last.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first.focus();
  }
}

function handleBackdropClick() {
  if (!props.persistent && props.closable) emit("close");
}

watch(
  () => props.open,
  async (open) => {
    document.removeEventListener("keydown", handleDocumentKeydown);
    if (!open || props.inline) {
      if (!open && previousFocus?.isConnected) previousFocus.focus();
      if (!open) previousFocus = null;
      return;
    }

    previousFocus = document.activeElement;
    document.addEventListener("keydown", handleDocumentKeydown);
    await nextTick();
    const first = focusableElements()[0];
    (first || sheetRef.value)?.focus();
  },
  { immediate: true },
);

onBeforeUnmount(() => {
  document.removeEventListener("keydown", handleDocumentKeydown);
});
</script>

<template>
  <Teleport to="body">
    <Transition name="mask">
      <div
        v-if="open"
        class="mask"
        :class="{ inline }"
        role="presentation"
        @mousedown.self.prevent
        @click.self="handleBackdropClick"
      >
        <Transition name="sheet" appear>
          <div ref="sheetRef" class="sheet" :class="{ wide, inline }" role="dialog" :aria-modal="inline ? undefined : 'true'" :aria-label="title" tabindex="-1" @click.stop>
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

.mask.inline {
  position: fixed;
  inset: 0;
  display: block;
  width: 100%;
  height: 100%;
  min-height: 100vh;
  padding: 0;
  background: var(--embedded-workspace-backdrop, #03161d);
  backdrop-filter: none;
  -webkit-backdrop-filter: none;
}

.sheet.inline {
  width: 100%;
  max-width: none;
  height: 100vh;
  max-height: none;
  border: 0;
  border-radius: 0;
  background: var(--embedded-workspace-surface, #03161d);
  backdrop-filter: none;
  -webkit-backdrop-filter: none;
  box-shadow: inset 0 1px 0 rgba(236, 255, 255, 0.16);
}

.sheet.inline > .head {
  display: none;
}

.sheet.inline > .body {
  padding: 24px clamp(18px, 2.4vw, 30px) 32px;
  overflow-x: hidden;
  overscroll-behavior: contain;
  scrollbar-gutter: stable;
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
  width: 40px;
  height: 40px;
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
  overflow-x: hidden;
  overflow-y: auto;
  flex: 1;
  min-height: 0;
  scrollbar-gutter: stable;
}

.foot {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
  padding: 13px 16px;
  border-top: 0.5px solid var(--separator);
  flex-shrink: 0;
}

@media (max-width: 520px) {
  .mask:not(.inline) {
    padding: 16px;
  }

  .sheet.inline > .body {
    padding: 20px 18px 28px;
  }

  .head,
  .body,
  .foot {
    padding-inline: 14px;
  }

  .foot :deep(.btn) {
    flex: 1 1 112px;
  }
}

/* Embedded desktop canvases can become short after the Agent navigation wraps.
   Keep both the close control and footer actions reachable in that state. */
@media (max-height: 420px) {
  .mask:not(.inline) {
    align-items: stretch;
    padding: 8px;
  }

  .sheet:not(.inline) {
    height: 100%;
    max-height: none;
    border-radius: 12px;
  }

  .sheet:not(.inline) > .head {
    padding: 8px 10px;
  }

  .sheet:not(.inline) > .body {
    padding: 10px;
  }

  .sheet:not(.inline) > .foot {
    padding: 8px 10px;
  }
}
</style>
