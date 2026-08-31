<script setup lang="ts">
import { ref } from 'vue'
import { useProxyStore } from '../stores/proxy'

const store = useProxyStore()
const editingId = ref('')
const editingRemark = ref('')

function hostOf(raw: string): string {
  try {
    return new URL(raw).host
  } catch {
    return raw
  }
}

function titleOf(item: { remark?: string; url: string }): string {
  return item.remark?.trim() || hostOf(item.url)
}

function startEdit(event: Event, id: string, remark: string) {
  event.stopPropagation()
  editingId.value = id
  editingRemark.value = remark || ''
}

function cancelEdit(event?: Event) {
  event?.stopPropagation()
  editingId.value = ''
  editingRemark.value = ''
}

async function saveEdit(event: Event, id: string) {
  event.stopPropagation()
  await store.updateRemark(id, editingRemark.value)
  cancelEdit()
}
</script>

<template>
  <section class="flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto pr-1 [--wails-draggable:no-drag]">
    <template v-if="store.showAddForm">
      <div class="flex flex-col gap-2 rounded-xl border border-[var(--border)] bg-[var(--bg-elevated)] p-2.5">
        <input
          v-model="store.draftUrl"
          class="sp-input w-full rounded-xl px-3 py-2 text-xs outline-none"
          type="url"
          placeholder="粘贴订阅 URL"
          :disabled="store.loading"
          autofocus
          @keydown.enter="store.addSubscription()"
        />
        <input
          v-model="store.draftRemark"
          class="sp-input w-full rounded-xl px-3 py-2 text-xs outline-none"
          type="text"
          maxlength="40"
          placeholder="备注（可选）"
          :disabled="store.loading"
          @keydown.enter="store.addSubscription()"
        />
        <button
          class="w-full rounded-xl bg-[var(--accent)] py-2 text-xs font-medium text-white transition hover:opacity-90 disabled:opacity-40"
          type="button"
          :disabled="store.loading || !store.draftUrl.trim()"
          @click="store.addSubscription()"
        >
          确认添加
        </button>
      </div>
    </template>

    <p v-if="store.subscriptions.length === 0" class="pt-6 text-center text-xs text-[var(--text-faint)]">
      点右上角 + 添加订阅
    </p>
    <article
      v-for="item in store.subscriptions"
      :key="item.id"
      class="sp-card cursor-pointer rounded-xl px-3 py-2.5 transition"
      :class="item.enabled && store.connected ? 'sp-card-active' : 'hover:bg-[var(--surface-hover)]'"
      @click="store.toggleSubscription(item)"
    >
        <div class="flex items-start justify-between gap-2">
          <div class="min-w-0 flex-1">
            <template v-if="editingId === item.id">
              <input
                v-model="editingRemark"
                class="sp-input w-full rounded-lg px-2 py-1 text-xs outline-none"
                type="text"
                maxlength="40"
                placeholder="输入备注"
                autofocus
                @click.stop
                @keydown.enter="saveEdit($event, item.id)"
                @keydown.esc="cancelEdit($event)"
              />
            </template>
            <p v-else class="truncate text-xs" :style="{ color: item.enabled && store.connected ? 'var(--accent-text)' : 'var(--text)' }">
              {{ titleOf(item) }}
            </p>
            <p class="mt-0.5 truncate text-[10px] text-[var(--text-faint)]">
              {{ item.remark ? hostOf(item.url) : item.id }}
            </p>
          </div>
          <div class="flex shrink-0 items-center gap-0.5" @click.stop>
            <button
              v-if="editingId === item.id"
              class="flex h-6 w-6 items-center justify-center rounded-full text-[var(--accent-text)] transition hover:bg-[var(--surface)]"
              type="button"
              aria-label="保存备注"
              title="保存"
              @click="saveEdit($event, item.id)"
            >
              <svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M5 12l5 5L20 7" />
              </svg>
            </button>
            <button
              v-else
              class="flex h-6 w-6 items-center justify-center rounded-full text-[var(--text-muted)] transition hover:bg-[var(--surface)] hover:text-[var(--text)]"
              type="button"
              aria-label="编辑备注"
              title="编辑备注"
              @click="startEdit($event, item.id, item.remark)"
            >
              <svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M12 20h9" />
                <path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4Z" />
              </svg>
            </button>
            <button
              class="flex h-6 w-6 items-center justify-center rounded-full text-[var(--danger)] transition hover:bg-[var(--surface)] hover:opacity-80"
              type="button"
              aria-label="删除订阅"
              title="删除"
              :disabled="store.loading"
              @click="store.removeSubscription(item.id)"
            >
              <svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M4 7h16" />
                <path d="M10 11v6M14 11v6" />
                <path d="M6 7l1 12a2 2 0 0 0 2 2h6a2 2 0 0 0 2-2l1-12" />
                <path d="M9 7V5a2 2 0 0 1 2-2h2a2 2 0 0 1 2 2v2" />
              </svg>
            </button>
          </div>
        </div>
        <p class="mt-1 text-[10px]" :style="{ color: item.enabled && store.connected ? 'var(--accent-text)' : 'var(--text-faint)' }">
          {{ item.enabled && store.connected ? '使用中 · 再次点击关闭' : '点击开始使用' }}
        </p>
    </article>
  </section>
</template>
