<script setup lang="ts">
import { useProxyStore } from '../stores/proxy'

const store = useProxyStore()

function onClick() {
  void store.speedTest()
}
</script>

<template>
  <button
    class="w-full rounded-xl px-4 py-2.5 text-sm transition duration-200 disabled:cursor-not-allowed disabled:opacity-40"
    :class="store.connected ? 'sp-btn-primary' : 'sp-card text-[var(--text-muted)]'"
    type="button"
    :disabled="store.loading || !store.connected"
    @click="onClick"
  >
    一键智能测速
  </button>
</template>
