<script setup lang="ts">
import { useProxyStore } from '../stores/proxy'

const store = useProxyStore()

function onClick() {
  void store.speedTest()
}
</script>

<template>
  <button
    class="w-full rounded-2xl border border-[var(--border)] bg-[var(--surface)] px-4 py-2.5 text-sm text-[var(--text)] transition duration-300 hover:border-[var(--accent-border)] hover:bg-[var(--surface-hover)] disabled:cursor-not-allowed disabled:opacity-40"
    type="button"
    :disabled="store.loading || !store.connected"
    @click="onClick"
  >
    一键智能测速
  </button>
</template>
