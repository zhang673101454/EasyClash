<script setup lang="ts">
import { useProxyStore } from '../stores/proxy'

const store = useProxyStore()
</script>

<template>
  <p
    class="shrink-0 text-center text-xs leading-relaxed"
    :style="{ color: store.connected ? 'var(--accent-text)' : 'var(--text-muted)' }"
  >
    {{ store.statusLabel }}
  </p>
</template>
