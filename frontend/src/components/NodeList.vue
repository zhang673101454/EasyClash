<script setup lang="ts">
import { useProxyStore } from '../stores/proxy'
import type { ProxyNode } from '../stores/proxy'
import SpeedTestButton from './SpeedTestButton.vue'

const store = useProxyStore()

function delayText(node: ProxyNode): string {
  if (!node.tested) {
    return '--'
  }
  if (node.delay <= 0 || node.delay > 3000) {
    return '不可用'
  }
  return `${node.delay}ms`
}

function delayColor(node: ProxyNode): string {
  if (!node.tested) {
    return 'var(--text-faint)'
  }
  if (node.delay <= 0 || node.delay > 3000) {
    return 'var(--timeout)'
  }
  if (node.delay < 150) {
    return 'var(--ok)'
  }
  if (node.delay < 400) {
    return 'var(--warn)'
  }
  return 'var(--timeout)'
}
</script>

<template>
  <section class="flex h-full min-h-0 flex-col gap-2.5 [--wails-draggable:no-drag]">
    <SpeedTestButton />
    <div class="flex items-center justify-between px-0.5">
      <p class="text-[11px] text-[var(--text-faint)]">
        <template v-if="store.connected">{{ store.visibleNodes.length }} 个节点</template>
        <span v-if="store.hideUnavailable && store.hiddenNodeCount > 0">
          · 已隐藏 {{ store.hiddenNodeCount }} 个不可用
        </span>
      </p>
      <button
        class="rounded-md px-1.5 py-0.5 text-[11px] text-[var(--text-muted)] transition hover:bg-[var(--surface)] hover:text-[var(--accent-text)]"
        type="button"
        @click="store.hideUnavailable = !store.hideUnavailable"
      >
        {{ store.hideUnavailable ? '显示全部' : '隐藏不可用' }}
      </button>
    </div>

    <div v-if="!store.connected" class="sp-empty">
      <span class="sp-empty-icon">
        <svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8">
          <path d="M12 3v8M7.5 6.5a7 7 0 1 0 9 0" />
        </svg>
      </span>
      <p>尚未连接</p>
      <p class="text-[10px]">请先到「连接」页启用订阅</p>
    </div>
    <p v-else-if="store.loading && store.nodes.length === 0" class="sp-empty">
      正在加载节点…
    </p>
    <p v-else-if="store.nodes.length === 0" class="sp-empty">
      暂无可用节点
    </p>
    <p v-else-if="store.visibleNodes.length === 0" class="sp-empty">
      当前没有可用节点，可点「显示全部」查看
    </p>

    <div v-else class="scroll-area min-h-0 flex-1 space-y-1.5">
      <button
        v-for="node in store.visibleNodes"
        :key="node.name"
        class="sp-card flex w-full items-center justify-between rounded-xl px-3 py-2.5 text-left transition"
        :class="node.selected ? 'sp-card-active' : ''"
        type="button"
        :disabled="store.loading"
        @click="store.selectNode(node.name)"
      >
        <span
          class="min-w-0 truncate text-xs"
          :class="node.selected ? 'font-medium text-[var(--accent-text)]' : 'text-[var(--text)]'"
        >
          {{ node.name }}
        </span>
        <span class="sp-delay-pill ml-2 shrink-0" :style="{ color: delayColor(node) }">
          {{ delayText(node) }}
        </span>
      </button>
    </div>
  </section>
</template>
