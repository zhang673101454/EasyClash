<script setup lang="ts">
import { useProxyStore } from '../stores/proxy'

const store = useProxyStore()
</script>

<template>
  <div
    v-if="store.toast"
    class="animate-toast-in pointer-events-none absolute inset-x-4 bottom-4 rounded-xl px-3 py-2 text-center text-xs text-white shadow-lg"
    :class="store.toastKind === 'ok' ? 'toast-ok' : 'toast-err'"
  >
    {{ store.toast }}
  </div>
</template>
