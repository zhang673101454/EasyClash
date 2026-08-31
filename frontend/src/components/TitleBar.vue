<script setup lang="ts">
import { ref } from 'vue'
import { WindowHide } from '../../wailsjs/runtime/runtime'
import { getTheme, toggleTheme, type Theme } from '../lib/theme'
import { useProxyStore } from '../stores/proxy'

const store = useProxyStore()
const theme = ref<Theme>(getTheme())

function hideWindow() {
  try {
    WindowHide()
  } catch (err) {
    console.warn('隐藏窗口失败', err)
  }
}

function onToggleTheme() {
  theme.value = toggleTheme()
}
</script>

<template>
  <header
    class="flex h-11 shrink-0 items-center justify-between px-3 [--wails-draggable:drag]"
  >
    <span class="text-xs font-medium tracking-wide text-[var(--text-muted)]">EasyClash</span>
    <div class="flex items-center gap-1 [--wails-draggable:no-drag]">
      <button
        class="flex h-6 w-6 items-center justify-center rounded-full transition hover:bg-[var(--surface)]"
        :class="store.showAddForm ? 'text-[var(--accent-text)]' : 'text-[var(--text-muted)] hover:text-[var(--text)]'"
        type="button"
        aria-label="添加订阅"
        title="添加订阅"
        :disabled="store.loading"
        @click="store.onAddClick()"
      >
        <svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2">
          <path d="M12 5v14M5 12h14" />
        </svg>
      </button>
      <button
        class="flex h-6 w-6 items-center justify-center rounded-full text-[var(--text-muted)] transition hover:bg-[var(--surface)] hover:text-[var(--text)]"
        type="button"
        :aria-label="theme === 'dark' ? '切换为浅色' : '切换为深色'"
        :title="theme === 'dark' ? '浅色' : '深色'"
        @click="onToggleTheme"
      >
        <svg v-if="theme === 'dark'" class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="12" cy="12" r="4" />
          <path d="M12 3v2M12 19v2M5.6 5.6l1.4 1.4M17 17l1.4 1.4M3 12h2M19 12h2M5.6 18.4 7 17M17 7l1.4-1.4" />
        </svg>
        <svg v-else class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="currentColor">
          <path d="M21 14.3A8.5 8.5 0 0 1 9.7 3 8.6 8.6 0 1 0 21 14.3Z" />
        </svg>
      </button>
      <button
        class="flex h-6 w-6 items-center justify-center rounded-full transition hover:bg-[var(--surface)]"
        :class="store.showSettings ? 'text-[var(--accent-text)]' : 'text-[var(--text-muted)] hover:text-[var(--text)]'"
        type="button"
        aria-label="设置"
        title="设置"
        @click="store.showSettings = !store.showSettings"
      >
        <svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="12" cy="12" r="3" />
          <path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1Z" />
        </svg>
      </button>
      <button
        class="flex h-6 w-6 items-center justify-center rounded-full text-[var(--text-muted)] transition hover:bg-[var(--surface)] hover:text-[var(--text)]"
        type="button"
        aria-label="隐藏到侧边栏"
        title="侧边栏"
        @click="store.setCompact(true)"
      >
        <svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <rect x="3" y="4" width="18" height="16" rx="2" />
          <path d="M15 4v16" />
        </svg>
      </button>
      <button
        class="flex h-6 w-6 items-center justify-center rounded-full text-[var(--text-muted)] transition hover:bg-[var(--surface)] hover:text-[var(--text)]"
        type="button"
        aria-label="最小化到托盘"
        title="最小化到托盘"
        @click="hideWindow"
      >
        <svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2">
          <path d="M5 12h14" />
        </svg>
      </button>
      <button
        class="flex h-6 w-6 items-center justify-center rounded-full text-[var(--text-muted)] transition hover:bg-[var(--surface)] hover:text-[var(--text)]"
        type="button"
        aria-label="最小化到托盘"
        title="最小化到托盘"
        @click="hideWindow"
      >
        <span class="mb-1 text-lg leading-none">×</span>
      </button>
    </div>
  </header>
</template>
