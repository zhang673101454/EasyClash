<script setup lang="ts">
import { useProxyStore } from '../stores/proxy'

const store = useProxyStore()

const intervalOptions = [5, 10, 15, 30, 60]
</script>

<template>
  <section class="scroll-area flex min-h-0 flex-1 flex-col gap-2.5 [--wails-draggable:no-drag]">
    <p class="text-xs font-semibold text-[var(--text)]">设置</p>

    <div class="sp-card rounded-xl px-3 py-3">
      <label class="flex items-center justify-between gap-3">
        <span class="text-xs text-[var(--text)]">开机自动启动</span>
        <button
          class="sp-toggle"
          :class="store.autoStart ? 'sp-toggle-on' : 'sp-toggle-off'"
          type="button"
          role="switch"
          :aria-checked="store.autoStart"
          @click="store.toggleAutoStart()"
        >
          <span
            class="sp-toggle-thumb"
            :class="store.autoStart ? 'sp-toggle-thumb-on' : 'sp-toggle-thumb-off'"
          />
        </button>
      </label>
    </div>

    <div class="sp-card rounded-xl px-3 py-3">
      <label class="flex items-center justify-between gap-3">
        <div class="min-w-0 pr-2">
          <span class="text-xs text-[var(--text)]">自动选最快节点</span>
          <p class="mt-0.5 text-[10px] leading-relaxed text-[var(--text-faint)]">定时测速，仅当新节点快 ≥80ms 才切换</p>
        </div>
        <button
          class="sp-toggle shrink-0"
          :class="store.autoSelectBest ? 'sp-toggle-on' : 'sp-toggle-off'"
          type="button"
          role="switch"
          :aria-checked="store.autoSelectBest"
          @click="store.toggleAutoSelectBest()"
        >
          <span
            class="sp-toggle-thumb"
            :class="store.autoSelectBest ? 'sp-toggle-thumb-on' : 'sp-toggle-thumb-off'"
          />
        </button>
      </label>
    </div>

    <div
      class="sp-card flex items-center justify-between rounded-xl px-3 py-3 transition"
      :class="store.autoSelectBest ? '' : 'opacity-50'"
    >
      <span class="text-xs text-[var(--text)]">测速间隔</span>
      <select
        class="sp-input rounded-lg px-2.5 py-1.5 text-xs outline-none"
        :disabled="!store.autoSelectBest"
        :value="store.autoSelectIntervalMin"
        @change="store.setAutoSelectInterval(Number(($event.target as HTMLSelectElement).value))"
      >
        <option v-for="item in intervalOptions" :key="item" :value="item">
          {{ item }} 分钟
        </option>
      </select>
    </div>

    <p class="px-1 text-[10px] leading-relaxed text-[var(--text-faint)]">
      启用订阅时会立即选一次最快节点。Tun 模式需要管理员权限。点击 − 进入悬浮窗，× 隐藏到托盘；悬浮窗右键可隐藏到托盘。
    </p>
  </section>
</template>
