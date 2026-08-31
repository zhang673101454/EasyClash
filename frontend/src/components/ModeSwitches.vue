<script setup lang="ts">
import { useProxyStore } from '../stores/proxy'

const store = useProxyStore()
</script>

<template>
  <div class="grid grid-cols-2 gap-2 [--wails-draggable:no-drag]">
    <button
      class="rounded-xl border px-2 py-2 text-left transition"
      :class="!store.tun ? 'sp-card-active' : 'sp-card'"
      type="button"
      :disabled="store.loading || !store.tun"
      @click="store.setTun(false)"
    >
      <p class="text-[11px] font-medium" :style="{ color: !store.tun ? 'var(--accent-text)' : 'var(--text)' }">规则模式</p>
      <p class="mt-0.5 text-[10px] text-[var(--text-faint)]">{{ !store.tun ? '默认 · 系统代理' : '点击切换' }}</p>
    </button>
    <button
      class="rounded-xl border px-2 py-2 text-left transition"
      :class="store.tun ? 'sp-card-active' : 'sp-card'"
      type="button"
      :disabled="store.loading || store.tun"
      @click="store.setTun(true)"
    >
      <p class="text-[11px] font-medium" :style="{ color: store.tun ? 'var(--accent-text)' : 'var(--text)' }">Tun 模式</p>
      <p class="mt-0.5 text-[10px] text-[var(--text-faint)]">{{ store.tun ? '虚拟网卡' : '点击切换' }}</p>
    </button>
  </div>
</template>
