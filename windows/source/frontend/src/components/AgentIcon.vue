<script setup>
import { siClaude, siCursor, siWindsurf } from "simple-icons";

const brandPaths = {
  "claude-code": siClaude.path,
  cursor: siCursor.path,
  windsurf: siWindsurf.path,
};

defineProps({
  agentId: {
    type: String,
    required: true,
  },
});
</script>

<template>
  <svg class="agent-icon" viewBox="0 0 24 24" aria-hidden="true">
    <g v-if="agentId === 'antigravity'" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round">
      <path d="M4.5 17.5c0-7.3 3.1-12 7.5-12s7.5 4.7 7.5 12" stroke-width="2.4" />
      <path d="M8 17.5c0-4.9 1.6-8 4-8s4 3.1 4 8" stroke-width="2.4" />
    </g>
    <g v-else-if="agentId === 'codex'" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="1.75">
      <path d="M12 3.3 16.1 5.7 16 10.4 11.9 12.7 7.9 10.3 8 5.6 12 3.3Z" />
      <path d="m16.1 5.7 4 2.3v4.7l-4.1 2.4-4.1-2.4.1-4.7 4.1-2.3Z" />
      <path d="m20.1 12.7-.1 4.7-4.1 2.3-4-2.4.1-4.6 4-2.3 4.1 2.3Z" />
      <path d="m15.9 19.7-4 2.3-4.1-2.4.1-4.7 4-2.2 4.1 2.4-.1 4.6Z" />
      <path d="m7.8 19.6-4-2.3v-4.7l4.1-2.3 4 2.4-.1 4.6-4 2.3Z" />
      <path d="m3.8 12.6.1-4.7L8 5.6l4 2.4-.1 4.7-4 2.2-4.1-2.3Z" />
    </g>
    <path v-else-if="brandPaths[agentId]" :d="brandPaths[agentId]" fill="currentColor" />
    <circle v-else cx="12" cy="12" r="7" fill="none" stroke="currentColor" stroke-width="2" />
  </svg>
</template>

<style scoped>
.agent-icon {
  display: block;
  width: 100%;
  height: 100%;
}
</style>
