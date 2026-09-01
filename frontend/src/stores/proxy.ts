import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import {
  AddSubscription,
  AutoSelectBestNode,
  GetAutoStart,
  GetNodes,
  GetSettings,
  GetStatus,
  GetSubscriptions,
  GetTraffic,
  RefreshSubscriptionTraffic,
  RemoveSubscription,
  SelectNode,
  SetAutoSelectSettings,
  SetAutoStart,
  SetCompactMode,
  ShouldStartCompact,
  SetSubscriptionRemark,
  SetTunMode,
  ToggleProxy,
  UseSubscription,
} from '../../wailsjs/go/main/App'
import type { backend, main } from '../../wailsjs/go/models'
import { WindowShow, WindowUnminimise } from '../../wailsjs/runtime/runtime'
import { errorMessage, isServiceNotReady, isTransientLoadError } from '../lib/errors'
import { asSubscriptionList, goBindingsReady, sleep, waitForGoBindings } from '../lib/subscriptions'

export type ProxyStatus = main.ProxyStatus
export type Subscription = main.SubscriptionItem
export type ProxyNode = backend.ProxyNode
export type TrafficInfo = main.TrafficInfo

const MAX_USABLE_DELAY = 3000

function delayRank(node: ProxyNode): number {
  if (!node.tested) {
    return 1_000_000
  }
  if (node.delay <= 0 || node.delay > MAX_USABLE_DELAY) {
    return 2_000_000
  }
  return node.delay
}

function sortNodesByDelay(list: ProxyNode[]): ProxyNode[] {
  return [...list].sort((a, b) => {
    const left = delayRank(a)
    const right = delayRank(b)
    if (left !== right) {
      return left - right
    }
    return a.name.localeCompare(b.name, 'zh')
  })
}

export const useProxyStore = defineStore('proxy', () => {
  const connected = ref(false)
  const nodeName = ref('')
  const latencyMs = ref(0)
  const message = ref('未连接')
  const loading = ref(false)
  const switching = ref(false)
  const toast = ref('')
  const toastKind = ref<'ok' | 'err'>('err')
  const modalTitle = ref('后端服务未启动')
  const modalMessage = ref('')
  const tab = ref<'home' | 'nodes'>('home')
  const draftUrl = ref('')
  const draftRemark = ref('')
  const subscriptions = ref<Subscription[]>([])
  const nodes = ref<ProxyNode[]>([])
  const hideUnavailable = ref(true)
  const compact = ref(false)
  const showSettings = ref(false)
  const showAddForm = ref(false)
  const tun = ref(false)
  const ruleMode = ref(true)
  const autoStart = ref(false)
  const autoSelectBest = ref(true)
  const autoSelectIntervalMin = ref(15)
  const upRate = ref(0)
  const downRate = ref(0)
  let toastTimer = 0
  let trafficTimer = 0

  const statusLabel = computed(() => {
    if (!connected.value) {
      return '未连接'
    }
    if (nodeName.value && latencyMs.value > 0) {
      return `已连接 · ${nodeName.value} (${latencyMs.value}ms)`
    }
    return message.value || '已连接'
  })

  const visibleNodes = computed(() => {
    const list = hideUnavailable.value
      ? nodes.value.filter((node) => {
          if (node.selected) {
            return true
          }
          if (node.name === 'DIRECT' || node.name === 'REJECT') {
            return true
          }
          if (!node.tested) {
            return true
          }
          if (node.delay <= 0 || node.delay > MAX_USABLE_DELAY) {
            return false
          }
          return true
        })
      : nodes.value
    return sortNodesByDelay(list)
  })

  const hiddenNodeCount = computed(() => nodes.value.length - visibleNodes.value.length)

  function applyStatus(status: ProxyStatus) {
    connected.value = Boolean(status.connected)
    nodeName.value = status.nodeName || ''
    latencyMs.value = status.latencyMs || 0
    message.value = status.message || (connected.value ? '已连接' : '未连接')
    tun.value = Boolean(status.tun)
    ruleMode.value = !Boolean(status.tun)
  }

  function showToast(text: string, kind: 'ok' | 'err' = 'err') {
    toast.value = text
    toastKind.value = kind
    window.clearTimeout(toastTimer)
    toastTimer = window.setTimeout(() => {
      toast.value = ''
    }, 3200)
  }

  function dismissModal() {
    modalMessage.value = ''
  }

  function goHome() {
    showSettings.value = false
    showAddForm.value = false
    tab.value = 'home'
  }

  async function requireService(detail?: string) {
    if (compact.value) {
      await setCompact(false)
    }
    try {
      WindowShow()
      WindowUnminimise()
    } catch {
      /* ignore */
    }
    goHome()
    modalTitle.value = '后端服务未启动'
    modalMessage.value = detail?.trim() || '请先到「连接」页点击一个订阅开始使用。'
  }

  function notifyError(err: unknown) {
    if (isServiceNotReady(err)) {
      void requireService(errorMessage(err))
      return
    }
    showToast(errorMessage(err))
  }

  async function refresh() {
    try {
      applyStatus(await GetStatus())
    } catch {
      applyStatus({
        connected: false,
        nodeName: '',
        latencyMs: 0,
        message: '未连接',
        mode: 'rule',
        tun: false,
      } as ProxyStatus)
    }
    try {
      const settings = await GetSettings()
      tun.value = Boolean(settings.tun)
      ruleMode.value = !Boolean(settings.tun)
      autoSelectBest.value = settings.autoSelectBest !== false
      autoSelectIntervalMin.value = settings.autoSelectIntervalMin || 15
    } catch {
      /* ignore */
    }
    try {
      autoStart.value = Boolean(await GetAutoStart())
    } catch {
      autoStart.value = false
    }
    await refreshSubscriptionsRetrying()
    void refreshAllSubscriptionTraffic()
    await refreshNodes()
  }

  async function applySubscriptionList(raw: unknown) {
    subscriptions.value = asSubscriptionList(raw) as Subscription[]
  }

  async function refreshSubscriptions() {
    try {
      await applySubscriptionList(await GetSubscriptions())
    } catch (err) {
      showToast(errorMessage(err))
    }
  }

  async function refreshSubscriptionTraffic(id: string, silent = false) {
    try {
      const updated = await RefreshSubscriptionTraffic(id)
      subscriptions.value = subscriptions.value.map((sub) =>
        sub.id === id
          ? {
              ...sub,
              upload: updated.upload || 0,
              download: updated.download || 0,
              total: updated.total || 0,
              expire: updated.expire || 0,
              updatedAt: updated.updatedAt || 0,
            }
          : sub,
      )
    } catch (err) {
      if (!silent) {
        showToast(errorMessage(err))
      }
    }
  }

  async function refreshAllSubscriptionTraffic() {
    const ids = subscriptions.value.map((sub) => sub.id)
    await Promise.all(ids.map((id) => refreshSubscriptionTraffic(id, true)))
  }

  async function refreshSubscriptionsRetrying() {
    await waitForGoBindings()
    let lastErr: unknown
    for (let attempt = 0; attempt < 15; attempt += 1) {
      try {
        await applySubscriptionList(await GetSubscriptions())
        return
      } catch (err) {
        lastErr = err
        if (!isTransientLoadError(err) && goBindingsReady()) {
          break
        }
        await sleep(120 * (attempt + 1))
      }
    }
    if (lastErr) {
      showToast(errorMessage(lastErr))
    }
  }

  async function refreshNodes() {
    if (!connected.value) {
      nodes.value = []
      return
    }
    try {
      nodes.value = (await GetNodes()) || []
    } catch (err) {
      nodes.value = []
      showToast(errorMessage(err))
    }
  }

  async function onAddClick() {
    showSettings.value = false
    tab.value = 'home'
    showAddForm.value = !showAddForm.value
  }

  async function addSubscription() {
    const url = draftUrl.value.trim()
    const remark = draftRemark.value.trim()
    if (loading.value || !url) {
      return
    }
    loading.value = true
    const before = subscriptions.value.length
    try {
      subscriptions.value = asSubscriptionList(await AddSubscription(url, remark)) as Subscription[]
      draftUrl.value = ''
      draftRemark.value = ''
      showAddForm.value = false
      if (before > 0 && subscriptions.value.length <= before) {
        showToast('该订阅已在列表中', 'ok')
      } else {
        showToast('订阅已添加，点击即可使用', 'ok')
      }
      await refresh()
    } catch (err) {
      draftUrl.value = url
      draftRemark.value = remark
      showAddForm.value = true
      showToast(errorMessage(err))
      await refreshSubscriptions()
    } finally {
      loading.value = false
    }
  }

  async function removeSubscription(id: string) {
    if (loading.value) {
      return
    }
    loading.value = true
    try {
      subscriptions.value = asSubscriptionList(await RemoveSubscription(id)) as Subscription[]
      showToast('订阅已删除')
      await refresh()
    } catch (err) {
      showToast(errorMessage(err))
    } finally {
      loading.value = false
    }
  }

  async function toggleSubscription(item: Subscription) {
    if (switching.value) {
      return
    }
    switching.value = true
    const turningOff = Boolean(item.enabled && connected.value)
    subscriptions.value = subscriptions.value.map((sub) => ({
      ...sub,
      enabled: turningOff ? false : sub.id === item.id,
    }))
    connected.value = !turningOff
    nodes.value = []
    showToast(turningOff ? '已停用' : '已开始使用该订阅', turningOff ? 'err' : 'ok')
    try {
      const status = await UseSubscription(item.id)
      applyStatus(status)
      await refreshSubscriptions()
      void refreshNodes()
      if (!turningOff) {
        void refreshSubscriptionTraffic(item.id, true)
      }
    } catch (err) {
      showToast(errorMessage(err))
      await refresh()
    } finally {
      switching.value = false
    }
  }

  async function updateRemark(id: string, remark: string) {
    try {
      subscriptions.value = asSubscriptionList(await SetSubscriptionRemark(id, remark)) as Subscription[]
    } catch (err) {
      showToast(errorMessage(err))
    }
  }

  async function selectNode(name: string) {
    if (loading.value) {
      return
    }
    loading.value = true
    try {
      applyStatus(await SelectNode(name))
      await refreshNodes()
    } catch (err) {
      showToast(errorMessage(err))
    } finally {
      loading.value = false
    }
  }

  async function speedTest() {
    if (loading.value) {
      return
    }
    if (!connected.value) {
      await requireService()
      return
    }
    loading.value = true
    try {
      await refreshNodes()
      applyStatus(await AutoSelectBestNode())
      await refreshNodes()
    } catch (err) {
      notifyError(err)
    } finally {
      loading.value = false
    }
  }

  async function toggle() {
    if (loading.value) {
      return
    }
    if (!connected.value) {
      await requireService()
      return
    }
    loading.value = true
    try {
      applyStatus(await ToggleProxy())
      await refreshSubscriptions()
      await refreshNodes()
    } catch (err) {
      notifyError(err)
      await refresh()
    } finally {
      loading.value = false
    }
  }

  async function applyStartLayout() {
    try {
      if (await ShouldStartCompact()) {
        await setCompact(true)
      }
    } catch {
      /* ignore */
    }
  }

  async function setCompact(next: boolean) {
    try {
      await SetCompactMode(next)
      compact.value = next
      document.documentElement.dataset.compact = next ? '1' : '0'
      showSettings.value = false
      if (next) {
        startTrafficLoop()
      }
    } catch (err) {
      showToast(errorMessage(err))
    }
  }

  async function setTun(enabled: boolean) {
    if (loading.value || tun.value === enabled) {
      return
    }
    loading.value = true
    try {
      applyStatus(await SetTunMode(enabled))
      await refreshNodes()
    } catch (err) {
      showToast(errorMessage(err))
    } finally {
      loading.value = false
    }
  }

  async function toggleAutoSelectBest() {
    try {
      const next = !autoSelectBest.value
      const settings = await SetAutoSelectSettings(next, autoSelectIntervalMin.value)
      autoSelectBest.value = Boolean(settings.autoSelectBest)
      autoSelectIntervalMin.value = settings.autoSelectIntervalMin || 15
    } catch (err) {
      showToast(errorMessage(err))
    }
  }

  async function setAutoSelectInterval(minutes: number) {
    if (autoSelectIntervalMin.value === minutes) {
      return
    }
    try {
      const settings = await SetAutoSelectSettings(autoSelectBest.value, minutes)
      autoSelectBest.value = Boolean(settings.autoSelectBest)
      autoSelectIntervalMin.value = settings.autoSelectIntervalMin || minutes
    } catch (err) {
      showToast(errorMessage(err))
    }
  }

  async function toggleAutoStart() {
    try {
      const next = !autoStart.value
      await SetAutoStart(next)
      autoStart.value = next
    } catch (err) {
      showToast(errorMessage(err))
    }
  }

  async function pollTraffic() {
    try {
      const info = await GetTraffic()
      upRate.value = info.upRate || 0
      downRate.value = info.downRate || 0
      if (info.connected) {
        connected.value = true
        if (info.nodeName) {
          nodeName.value = info.nodeName
        }
        latencyMs.value = info.latencyMs || 0
      } else {
        connected.value = false
        upRate.value = 0
        downRate.value = 0
      }
    } catch {
      /* ignore */
    }
  }

  function startTrafficLoop() {
    if (trafficTimer) {
      return
    }
    void pollTraffic()
    trafficTimer = window.setInterval(() => {
      void pollTraffic()
    }, 1000)
  }

  function stopTrafficLoop() {
    window.clearInterval(trafficTimer)
    trafficTimer = 0
  }

  return {
    connected,
    nodeName,
    latencyMs,
    message,
    loading,
    toast,
    toastKind,
    modalTitle,
    modalMessage,
    tab,
    draftUrl,
    draftRemark,
    subscriptions,
    nodes,
    hideUnavailable,
    compact,
    showSettings,
    showAddForm,
    tun,
    ruleMode,
    autoStart,
    autoSelectBest,
    autoSelectIntervalMin,
    upRate,
    downRate,
    visibleNodes,
    hiddenNodeCount,
    statusLabel,
    applyStatus,
    showToast,
    dismissModal,
    requireService,
    goHome,
    refresh,
    refreshNodes,
    refreshSubscriptionTraffic,
    refreshAllSubscriptionTraffic,
    addSubscription,
    onAddClick,
    removeSubscription,
    toggleSubscription,
    updateRemark,
    selectNode,
    speedTest,
    toggle,
    setCompact,
    applyStartLayout,
    setTun,
    toggleAutoStart,
    toggleAutoSelectBest,
    setAutoSelectInterval,
    startTrafficLoop,
    stopTrafficLoop,
  }
})
