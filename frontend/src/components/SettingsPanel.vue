<script setup lang="ts">
import { useProxyStore } from '../stores/proxy'

const store = useProxyStore()
</script>

<template>
  <section class="flex min-h-0 flex-1 flex-col gap-3 [--wails-draggable:no-drag]">
    <p class="text-xs font-medium text-[var(--text-muted)]">设置</p>
    <label class="sp-card flex items-center justify-between rounded-xl px-3 py-3">
      <span class="text-xs text-[var(--text)]">开机自动启动</span>
      <button
        class="relative h-5 w-9 rounded-full transition"
        :class="store.autoStart ? 'bg-[var(--accent)]' : 'bg-[var(--border-strong)]'"
        type="button"
        role="switch"
        :aria-checked="store.autoStart"
        @click="store.toggleAutoStart()"
      >
        <span
          class="absolute top-0.5 h-4 w-4 rounded-full bg-white transition"
          :class="store.autoStart ? 'left-4' : 'left-0.5'"
        />
      </button>
    </label>
    <p class="px-1 text-[10px] leading-relaxed text-[var(--text-faint)]">
      登录 Windows 后自动打开 EasyClash。Tun 模式需要管理员权限。点击 × 或 − 会最小化到系统托盘，托盘图标右键可退出。
    </p>
  </section>
</template>
