<script setup lang="ts">
import { onMounted, onUnmounted, watch } from 'vue'
import AppModal from './components/AppModal.vue'
import AppToast from './components/AppToast.vue'
import AppVersion from './components/AppVersion.vue'
import MiniDock from './components/MiniDock.vue'
import ModeSwitches from './components/ModeSwitches.vue'
import NodeList from './components/NodeList.vue'
import SettingsPanel from './components/SettingsPanel.vue'
import SubscribePanel from './components/SubscribePanel.vue'
import TabBar from './components/TabBar.vue'
import TitleBar from './components/TitleBar.vue'
import { useProxyStore } from './stores/proxy'
import { isServiceNotReady } from './lib/errors'
import { EventsOff, EventsOn } from '../wailsjs/runtime/runtime'
import type { ProxyStatus } from './stores/proxy'

const store = useProxyStore()

onMounted(() => {
  void store.refresh()
  store.startTrafficLoop()
  void store.applyStartLayout()
  try {
    EventsOn('proxy:status', (status: ProxyStatus) => {
      store.applyStatus(status)
      void store.refreshNodes(true)
    })
    EventsOn('proxy:error', (message: string) => {
      if (isServiceNotReady(message)) {
        void store.requireService(String(message))
        return
      }
      store.showToast(String(message))
    })
    EventsOn('subscription:refreshed', (payload: {
      id: string
      ok: boolean
      error?: string
      upload?: number
      download?: number
      total?: number
      expire?: number
      updatedAt?: number
    }) => {
      store.handleSubscriptionRefreshed(payload)
    })
    EventsOn('window:compact', (value: boolean) => {
      store.compact = Boolean(value)
      document.documentElement.dataset.compact = value ? '1' : '0'
    })
  } catch (err) {
    console.warn('Wails 事件绑定不可用', err)
  }
})

onUnmounted(() => {
  store.stopTrafficLoop()
  try {
    EventsOff('proxy:status')
    EventsOff('proxy:error')
    EventsOff('subscription:refreshed')
    EventsOff('window:compact')
  } catch (err) {
    console.warn('移除 Wails 事件失败', err)
  }
})

watch(
  () => store.tab,
  (tab) => {
    if (tab === 'nodes') {
      void store.refreshNodes()
    }
  },
)
</script>

<template>
  <div
    v-if="store.compact"
    class="flex h-full w-full justify-end bg-transparent"
  >
    <div
      class="mini-shell app-frame flex h-full flex-col"
      :class="store.connected ? 'app-frame-on' : 'app-frame-off'"
    >
      <MiniDock />
    </div>
  </div>
  <div
    v-else
    class="app-shell relative flex h-full w-full flex-col app-frame text-[var(--text)]"
    :class="store.connected ? 'app-frame-on' : 'app-frame-off'"
  >
    <TitleBar />
    <TabBar />
    <main class="flex min-h-0 flex-1 flex-col px-4 pb-4 pt-1">
      <SettingsPanel v-if="store.showSettings" />
      <section v-else-if="store.tab === 'home'" class="flex min-h-0 flex-1 flex-col gap-3">
        <ModeSwitches />
        <SubscribePanel />
      </section>
      <NodeList v-else />
    </main>
    <AppVersion v-if="!store.showSettings" />
    <AppToast />
    <AppModal />
  </div>
</template>
