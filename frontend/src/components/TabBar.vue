<script setup lang="ts">
import { useProxyStore } from '../stores/proxy'

const store = useProxyStore()

const tabs = [
  { id: 'home', label: '连接' },
  { id: 'nodes', label: '节点' },
] as const
</script>

<template>
  <nav v-if="!store.showSettings" class="px-3 pb-3 pt-1 [--wails-draggable:no-drag]">
    <div class="sp-picker">
      <button
        v-for="item in tabs"
        :key="item.id"
        class="sp-picker-btn sp-picker-btn-simple"
        :class="store.tab === item.id ? 'sp-picker-btn-active' : ''"
        type="button"
        @click="store.tab = item.id"
      >
        {{ item.label }}
      </button>
    </div>
  </nav>
</template>
