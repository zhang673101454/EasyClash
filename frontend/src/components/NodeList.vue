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
  <section class="flex h-full min-h-0 flex-col gap-2 [--wails-draggable:no-drag]">
    <SpeedTestButton />
    <div class="flex items-center justify-between px-0.5">
      <p class="text-[11px] text-[var(--text-faint)]">
        <template v-if="store.connected">{{ store.visibleNodes.length }} 个节点</template>
        <span v-if="store.hideUnavailable && store.hiddenNodeCount > 0">
          · 已隐藏 {{ store.hiddenNodeCount }} 个不可用
        </span>
      </p>
      <button
        class="text-[11px] text-[var(--text-muted)] hover:text-[var(--accent-text)]"
        type="button"
        @click="store.hideUnavailable = !store.hideUnavailable"
      >
        {{ store.hideUnavailable ? '显示全部' : '隐藏不可用' }}
      </button>
    </div>
    <p v-if="!store.connected" class="pt-10 text-center text-xs text-[var(--text-faint)]">
      请先到「连接」页点击一个订阅开始使用
    </p>
    <p v-else-if="store.loading && store.nodes.length === 0" class="pt-10 text-center text-xs text-[var(--text-faint)]">
      正在加载节点…
    </p>
    <p v-else-if="store.nodes.length === 0" class="pt-10 text-center text-xs text-[var(--text-faint)]">
      暂无可用节点，请先添加并点击订阅
    </p>
    <p v-else-if="store.visibleNodes.length === 0" class="pt-10 text-center text-xs text-[var(--text-faint)]">
      当前没有可用节点，可点「显示全部」查看
    </p>
    <div v-else class="min-h-0 flex-1 space-y-1 overflow-y-auto pr-1">
      <button
        v-for="node in store.visibleNodes"
        :key="node.name"
        class="flex w-full items-center justify-between rounded-xl border px-3 py-2 text-left transition"
        :class="node.selected
          ? 'border-[var(--accent-border)] bg-[var(--accent-soft)]'
          : 'sp-card hover:border-[var(--border-strong)]'"
        type="button"
        :disabled="store.loading"
        @click="store.selectNode(node.name)"
      >
        <span
          class="min-w-0 truncate text-xs"
          :style="{ color: node.selected ? 'var(--accent-text)' : 'var(--text)' }"
        >
          {{ node.name }}
        </span>
        <span class="ml-2 shrink-0 text-[11px] tabular-nums" :style="{ color: delayColor(node) }">
          {{ delayText(node) }}
        </span>
      </button>
    </div>
  </section>
</template>
