<script setup lang="ts">
import { ref } from 'vue'
import { useProxyStore, type Subscription } from '../stores/proxy'
import {
  formatBytes,
  formatExpire,
  formatRelativeTime,
  hasTrafficInfo,
  trafficPercent,
} from '../lib/traffic'

const store = useProxyStore()
const editingId = ref('')
const editingUrl = ref('')
const editingRemark = ref('')
const savingId = ref('')

const isRefreshing = (id: string) => store.refreshingSubscriptionId === id

function cardSwitchBlocked(): boolean {
  return Boolean(
    editingId.value || store.switching || store.refreshingSubscriptionId,
  )
}

function refreshButtonDisabled(id: string): boolean {
  if (store.switching) {
    return true
  }
  const busyId = store.refreshingSubscriptionId
  return Boolean(busyId && busyId !== id)
}

function sideActionDisabled(): boolean {
  return store.switching || Boolean(store.refreshingSubscriptionId) || store.loading
}

function onCardClick(item: Subscription) {
  if (cardSwitchBlocked()) {
    if (store.refreshingSubscriptionId) {
      store.showToast('正在刷新订阅，请稍候或点刷新按钮取消')
    } else if (store.switching) {
      store.showToast('正在切换订阅，请稍候')
    }
    return
  }
  void store.toggleSubscription(item)
}

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

function usedText(item: { upload?: number; download?: number }): string {
  const used = (item.upload || 0) + (item.download || 0)
  return formatBytes(used)
}

function isActive(item: { enabled: boolean }): boolean {
  return Boolean(item.enabled && store.connected)
}

function hasSelectedNode(): boolean {
  const name = store.nodeName.trim()
  if (name && name !== 'DIRECT') {
    return true
  }
  const selected = store.nodes.find((node) => node.selected && node.name !== 'DIRECT')
  return Boolean(selected?.name)
}

function statusBadge(item: Subscription): string {
  if (store.switching && item.enabled) {
    return '切换中'
  }
  if (isRefreshing(item.id)) {
    return '刷新中'
  }
  if (isActive(item)) {
    return hasSelectedNode() ? '使用中' : '等待节点'
  }
  return '待用'
}

function startEdit(event: Event, id: string, url: string, remark: string) {
  event.stopPropagation()
  if (sideActionDisabled()) {
    return
  }
  editingId.value = id
  editingUrl.value = url
  editingRemark.value = remark || ''
}

function cancelEdit(event?: Event) {
  event?.stopPropagation()
  editingId.value = ''
  editingUrl.value = ''
  editingRemark.value = ''
}

async function saveEdit(event: Event, id: string) {
  event.stopPropagation()
  if (savingId.value || sideActionDisabled()) {
    return
  }
  savingId.value = id
  try {
    await store.updateSubscription(id, editingUrl.value, editingRemark.value)
    cancelEdit()
  } finally {
    if (savingId.value === id) {
      savingId.value = ''
    }
  }
}

async function refreshTraffic(event: Event, id: string) {
  event.stopPropagation()
  if (isRefreshing(id)) {
    await store.cancelRefreshSubscriptionTraffic(id)
    return
  }
  if (refreshButtonDisabled(id)) {
    if (store.switching) {
      store.showToast('正在切换订阅，请稍候')
    }
    return
  }
  await store.refreshSubscriptionTraffic(id)
}
</script>

<template>
  <section class="scroll-area flex min-h-0 flex-1 flex-col gap-2 [--wails-draggable:no-drag]">
    <template v-if="store.showAddForm">
      <div class="sp-card flex flex-col gap-2.5 rounded-xl p-3">
        <p class="text-[11px] font-medium text-[var(--text-muted)]">添加订阅</p>
        <input
          v-model="store.draftUrl"
          class="sp-input w-full rounded-xl px-3 py-2 text-xs outline-none"
          type="url"
          placeholder="粘贴订阅 URL"
          :disabled="store.loading || store.subscriptionInteractionLocked"
          autofocus
          @keydown.enter="store.addSubscription()"
        />
        <input
          v-model="store.draftRemark"
          class="sp-input w-full rounded-xl px-3 py-2 text-xs outline-none"
          type="text"
          maxlength="40"
          placeholder="备注（可选）"
          :disabled="store.loading || store.subscriptionInteractionLocked"
          @keydown.enter="store.addSubscription()"
        />
        <button
          class="sp-btn-primary w-full rounded-xl py-2 text-xs"
          type="button"
          :disabled="store.loading || store.subscriptionInteractionLocked || !store.draftUrl.trim()"
          @click="store.addSubscription()"
        >
          确认添加
        </button>
      </div>
    </template>

    <div v-if="store.subscriptions.length === 0" class="sp-empty">
      <span class="sp-empty-icon">
        <svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8">
          <path d="M12 5v14M5 12h14" />
        </svg>
      </span>
      <p>还没有订阅</p>
      <p class="text-[10px]">点右上角 + 添加订阅链接</p>
    </div>

    <article
      v-for="item in store.subscriptions"
      :key="item.id"
      class="sp-card sp-card-sub rounded-xl px-3 py-2.5 transition"
      :class="[
        isActive(item) ? 'sp-card-in-use' : '',
        cardSwitchBlocked() ? 'cursor-default' : 'cursor-pointer',
      ]"
      @click="onCardClick(item)"
    >
      <div class="flex items-start justify-between gap-2">
        <div class="min-w-0 flex-1">
          <template v-if="editingId === item.id">
            <input
              v-model="editingUrl"
              class="sp-input w-full rounded-lg px-2 py-1 text-xs outline-none"
              type="url"
              placeholder="订阅 URL"
              autofocus
              @click.stop
              @keydown.enter="saveEdit($event, item.id)"
              @keydown.esc="cancelEdit($event)"
            />
            <input
              v-model="editingRemark"
              class="sp-input mt-1.5 w-full rounded-lg px-2 py-1 text-xs outline-none"
              type="text"
              maxlength="40"
              placeholder="备注（可选）"
              @click.stop
              @keydown.enter="saveEdit($event, item.id)"
              @keydown.esc="cancelEdit($event)"
            />
          </template>
          <div v-else class="flex items-center gap-2">
            <p
              class="min-w-0 truncate text-xs font-medium"
              :style="{ color: isActive(item) ? 'var(--accent-text)' : 'var(--text)' }"
            >
              {{ titleOf(item) }}
            </p>
            <span class="sp-badge shrink-0" :class="isActive(item) ? 'sp-badge-in-use' : 'sp-badge-idle'">
              {{ statusBadge(item) }}
            </span>
          </div>
          <p class="mt-1 truncate text-[10px] text-[var(--text-faint)]">
            {{ item.remark ? hostOf(item.url) : item.id }}
          </p>
        </div>
        <div class="flex shrink-0 items-center gap-0.5" @click.stop>
          <button
            class="sp-btn-icon"
            type="button"
            :disabled="refreshButtonDisabled(item.id)"
            :aria-label="isRefreshing(item.id) ? '取消刷新' : '刷新流量与节点'"
            :title="
              store.switching
                ? '正在切换订阅，请稍候'
                : isRefreshing(item.id)
                  ? '取消刷新'
                  : '刷新流量与节点（需先开启代理）'
            "
            @click="refreshTraffic($event, item.id)"
          >
            <svg
              class="h-3.5 w-3.5"
              :class="isRefreshing(item.id) ? 'animate-spin' : ''"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              stroke-linecap="round"
              stroke-linejoin="round"
            >
              <path d="M21 12a9 9 0 1 1-2.64-6.36" />
              <path d="M21 3v6h-6" />
            </svg>
          </button>
          <button
            v-if="editingId === item.id"
            class="sp-btn-icon sp-btn-icon-active"
            type="button"
            aria-label="保存备注"
            title="保存"
            :disabled="savingId === item.id"
            @click.stop="saveEdit($event, item.id)"
          >
            <svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M5 12l5 5L20 7" />
            </svg>
          </button>
          <button
            v-else
            class="sp-btn-icon"
            type="button"
            aria-label="编辑订阅"
            title="编辑"
            :disabled="sideActionDisabled()"
            @click="startEdit($event, item.id, item.url, item.remark)"
          >
            <svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M12 20h9" />
              <path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4Z" />
            </svg>
          </button>
          <button
            class="sp-btn-icon !text-[var(--danger)]"
            type="button"
            aria-label="删除订阅"
            title="删除"
            :disabled="sideActionDisabled()"
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

      <div v-if="hasTrafficInfo(item)" class="mt-2">
        <div class="flex items-center justify-between gap-2 text-[10px] text-[var(--text-muted)]">
          <span class="tabular-nums">
            {{ usedText(item) }} / {{ formatBytes(item.total || 0) }}
          </span>
          <span v-if="item.expire" class="shrink-0 tabular-nums">{{ formatExpire(item.expire) }}</span>
        </div>
        <div class="sp-progress-track mt-1.5">
          <div
            class="sp-progress-fill"
            :style="{ width: `${trafficPercent(item.upload || 0, item.download || 0, item.total || 0)}%` }"
          />
        </div>
        <p v-if="item.updatedAt" class="mt-1 text-[10px] text-[var(--text-faint)]">
          {{ formatRelativeTime(item.updatedAt) }}更新
        </p>
      </div>

      <p class="mt-1.5 text-[10px]" :class="isActive(item) ? 'text-[var(--accent-text)]' : 'text-[var(--text-faint)]'">
        <template v-if="isRefreshing(item.id)">刷新中，再次点击刷新按钮可取消</template>
        <template v-else-if="store.switching && item.enabled">正在切换，请稍候</template>
        <template v-else-if="isActive(item) && !hasSelectedNode()">节点加载中，可点刷新</template>
        <template v-else-if="isActive(item)">再次点击可关闭</template>
        <template v-else>点击开始使用</template>
      </p>
    </article>
  </section>
</template>
