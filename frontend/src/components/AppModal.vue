<script setup lang="ts">
import { useProxyStore } from '../stores/proxy'

const store = useProxyStore()
</script>

<template>
  <div
    v-if="store.modalMessage"
    class="absolute inset-0 z-50 flex items-center justify-center bg-[var(--overlay)] px-4 [--wails-draggable:no-drag]"
    @click.self="store.dismissModal()"
  >
    <div
      class="animate-toast-in w-full max-w-[240px] rounded-2xl bg-[var(--bg-elevated)] px-4 py-4 shadow-xl"
    >
      <p class="text-sm font-medium text-[var(--text)]">{{ store.modalTitle }}</p>
      <p class="mt-2 text-xs leading-relaxed text-[var(--text-muted)]">{{ store.modalMessage }}</p>
      <button
        class="mt-4 w-full rounded-xl bg-[var(--accent)] px-3 py-2 text-xs font-medium text-white"
        type="button"
        @click="store.dismissModal()"
      >
        知道了
      </button>
    </div>
  </div>
</template>
