<script setup lang="ts">
import { WindowHide } from '../../wailsjs/runtime/runtime'
import { useProxyStore } from '../stores/proxy'

const store = useProxyStore()

function formatRate(bytes: number): string {
  if (!bytes || bytes < 0) {
    return '0'
  }
  if (bytes < 1024) {
    return `${bytes}`
  }
  if (bytes < 1024 * 1024) {
    return `${(bytes / 1024).toFixed(0)}K`
  }
  return `${(bytes / (1024 * 1024)).toFixed(1)}M`
}

function expand() {
  void store.setCompact(false)
}

function hideToTray() {
  try {
    WindowHide()
  } catch (err) {
    console.warn('隐藏窗口失败', err)
  }
}

function onPower() {
  if (!store.connected) {
    void store.requireService()
    return
  }
  void store.toggle()
}
</script>

<template>
  <div
    class="mini-dock flex h-full flex-col items-center justify-between px-0.5 py-2 text-[var(--text)] [--wails-draggable:drag]"
    title="右键隐藏到托盘"
    @dblclick="expand"
    @contextmenu.prevent="hideToTray"
  >
    <button
      class="flex h-6 w-6 shrink-0 items-center justify-center rounded-full [--wails-draggable:no-drag]"
      :style="{ background: store.connected ? 'var(--ok)' : 'var(--toggle-off)' }"
      type="button"
      :disabled="store.loading"
      :aria-label="store.connected ? '停用' : '启用'"
      @click.stop="onPower"
    >
      <svg v-if="store.connected" class="h-3.5 w-3.5 text-white" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.8">
        <path d="M5 12l5 5L20 7" />
      </svg>
      <svg v-else class="h-3.5 w-3.5" :style="{ color: 'var(--toggle-off-icon)' }" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round">
        <path d="M12 3v8" />
        <path d="M7.5 6.5a7 7 0 1 0 9 0" />
      </svg>
    </button>

    <div class="flex flex-col items-center [--wails-draggable:no-drag]" @click="expand">
      <p class="text-[12px] font-semibold leading-none tabular-nums">
        {{ store.connected && store.latencyMs > 0 ? store.latencyMs : '--' }}
      </p>
      <p class="mt-0.5 text-[8px] text-[var(--text-faint)]">ms</p>
      <p class="mt-1 text-[9px] tabular-nums text-[var(--text-muted)]">↑{{ formatRate(store.upRate) }}</p>
      <p class="text-[9px] tabular-nums text-[var(--text-muted)]">↓{{ formatRate(store.downRate) }}</p>
    </div>

    <button
      class="flex h-6 w-6 items-center justify-center rounded-full text-[var(--text-muted)] hover:bg-[var(--surface)] hover:text-[var(--text)] [--wails-draggable:no-drag]"
      type="button"
      aria-label="回到主界面"
      title="回到主界面"
      @click.stop="expand"
    >
      <svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
        <rect x="5" y="5" width="14" height="14" rx="3" />
      </svg>
    </button>
  </div>
</template>
