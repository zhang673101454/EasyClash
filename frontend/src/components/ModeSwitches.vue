<script setup lang="ts">
import { useProxyStore } from '../stores/proxy'

const store = useProxyStore()
</script>

<template>
  <div class="grid grid-cols-2 gap-2 [--wails-draggable:no-drag]">
    <button
      class="sp-card rounded-xl px-3 py-2.5 text-left transition"
      :class="!store.tun ? 'sp-card-active' : 'hover:border-[var(--border-strong)]'"
      type="button"
      :disabled="store.loading || !store.tun"
      @click="store.setTun(false)"
    >
      <div class="flex items-center gap-2">
        <span
          class="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg"
          :style="{ background: !store.tun ? 'var(--accent-soft)' : 'var(--surface)' }"
        >
          <svg class="h-3.5 w-3.5" :style="{ color: !store.tun ? 'var(--accent-text)' : 'var(--text-muted)' }" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M4 6h16M4 12h10M4 18h6" />
          </svg>
        </span>
        <div class="min-w-0">
          <p class="text-[11px] font-medium" :style="{ color: !store.tun ? 'var(--accent-text)' : 'var(--text)' }">规则模式</p>
          <p class="mt-0.5 text-[10px] text-[var(--text-faint)]">{{ !store.tun ? '默认 · 系统代理' : '点击切换' }}</p>
        </div>
      </div>
    </button>
    <button
      class="sp-card rounded-xl px-3 py-2.5 text-left transition"
      :class="store.tun ? 'sp-card-active' : 'hover:border-[var(--border-strong)]'"
      type="button"
      :disabled="store.loading || store.tun"
      @click="store.setTun(true)"
    >
      <div class="flex items-center gap-2">
        <span
          class="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg"
          :style="{ background: store.tun ? 'var(--accent-soft)' : 'var(--surface)' }"
        >
          <svg class="h-3.5 w-3.5" :style="{ color: store.tun ? 'var(--accent-text)' : 'var(--text-muted)' }" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <rect x="3" y="5" width="18" height="14" rx="2" />
            <path d="M7 9h10M7 13h6" />
          </svg>
        </span>
        <div class="min-w-0">
          <p class="text-[11px] font-medium" :style="{ color: store.tun ? 'var(--accent-text)' : 'var(--text)' }">Tun 模式</p>
          <p class="mt-0.5 text-[10px] text-[var(--text-faint)]">{{ store.tun ? '虚拟网卡' : '需管理员' }}</p>
        </div>
      </div>
    </button>
  </div>
</template>
