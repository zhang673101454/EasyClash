<script setup lang="ts">
import { useProxyStore } from '../stores/proxy'

const store = useProxyStore()
</script>

<template>
  <div class="sp-picker [--wails-draggable:no-drag]">
    <button
      class="sp-picker-btn"
      :class="!store.tun ? 'sp-picker-btn-active' : ''"
      type="button"
      :disabled="store.loading"
      @click="store.setTun(false)"
    >
      <span class="sp-picker-icon" aria-hidden="true">
        <svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M4 6h16M4 12h10M4 18h6" />
        </svg>
      </span>
      <span class="sp-picker-copy">
        <span class="sp-picker-label">规则模式</span>
        <span class="sp-picker-hint">{{ !store.tun ? '默认 · 系统代理' : '点击切换' }}</span>
      </span>
    </button>
    <button
      class="sp-picker-btn"
      :class="store.tun ? 'sp-picker-btn-active' : ''"
      type="button"
      :disabled="store.loading"
      @click="store.setTun(true)"
    >
      <span class="sp-picker-icon" aria-hidden="true">
        <svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <rect x="3" y="5" width="18" height="14" rx="2" />
          <path d="M7 9h10M7 13h6" />
        </svg>
      </span>
      <span class="sp-picker-copy">
        <span class="sp-picker-label">Tun 模式</span>
        <span class="sp-picker-hint">{{ store.tun ? '虚拟网卡' : '需管理员' }}</span>
      </span>
    </button>
  </div>
</template>
