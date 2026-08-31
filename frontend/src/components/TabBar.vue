<script setup lang="ts">
import { useProxyStore } from '../stores/proxy'

const store = useProxyStore()

const tabs = [
  { id: 'home', label: '连接' },
  { id: 'nodes', label: '节点' },
] as const
</script>

<template>
  <nav v-if="!store.showSettings" class="grid grid-cols-2 gap-1 px-3 pb-2 [--wails-draggable:no-drag]">
    <button
      v-for="item in tabs"
      :key="item.id"
      class="rounded-lg py-1.5 text-xs transition"
      :class="store.tab === item.id
        ? 'bg-[var(--accent-soft)] text-[var(--accent-text)]'
        : 'text-[var(--text-muted)] hover:bg-[var(--surface)] hover:text-[var(--text)]'"
      type="button"
      @click="store.tab = item.id"
    >
      {{ item.label }}
    </button>
  </nav>
</template>
