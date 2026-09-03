<script setup lang="ts">
import { useProxyStore } from '../stores/proxy'

const store = useProxyStore()
</script>

<template>
  <div class="sp-mode [--wails-draggable:no-drag]">
    <p class="sp-mode-label">连接方式</p>
    <div class="sp-mode-grid" role="radiogroup" aria-label="连接方式">
      <button
        type="button"
        role="radio"
        class="sp-mode-card rounded-lg"
        :class="{ 'sp-mode-card-active': !store.tun }"
        :aria-checked="!store.tun"
        :disabled="store.loading"
        @click="store.setTun(false)"
      >
        <span class="sp-mode-card-icon" aria-hidden="true">
          <svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M4 6h16M4 12h10M4 18h6" />
          </svg>
        </span>
        <span class="sp-mode-card-body">
          <span class="sp-mode-card-title">智能模式</span>
          <span class="sp-mode-card-desc">系统代理</span>
        </span>
      </button>
      <button
        type="button"
        role="radio"
        class="sp-mode-card rounded-lg"
        :class="{ 'sp-mode-card-active': store.tun }"
        :aria-checked="store.tun"
        :disabled="store.loading"
        @click="store.setTun(true)"
      >
        <span class="sp-mode-card-icon" aria-hidden="true">
          <svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <rect x="3" y="5" width="18" height="14" rx="2" />
            <path d="M7 9h10M7 13h6" />
          </svg>
        </span>
        <span class="sp-mode-card-body">
          <span class="sp-mode-card-title">Tun 模式</span>
          <span class="sp-mode-card-desc">虚拟网卡</span>
        </span>
      </button>
    </div>
  </div>
</template>
