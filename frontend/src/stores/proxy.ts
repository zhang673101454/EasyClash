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
  CancelAllSubscriptionRefresh,
  CancelRefreshSubscriptionTraffic,
  RefreshSubscriptionTraffic,
  RemoveSubscription,
  SelectNode,
  SetAutoSelectSettings,
  SetAutoStart,
  SetCompactMode,
  ShouldStartCompact,
  UpdateSubscription,
  SetTunMode,
  ToggleProxy,
  UseSubscription,
} from '../../wailsjs/go/main/App'
import type { backend, main } from '../../wailsjs/go/models'
import { WindowShow, WindowUnminimise } from '../../wailsjs/runtime/runtime'
import { errorMessage, isRefreshCancelled, isServiceNotReady, isTransientLoadError } from '../lib/errors'
import { asSubscriptionList, goBindingsReady, sleep, waitForGoBindings } from '../lib/subscriptions'

async function withTimeout<T>(promise: Promise<T>, ms: number, message: string): Promise<T> {
  let timer = 0
  try {
    return await Promise.race([
      promise,
      new Promise<T>((_, reject) => {
        timer = window.setTimeout(() => reject(new Error(message)), ms)
      }),
    ])
  } finally {
    window.clearTimeout(timer)
  }
}

/** 各 Go 接口前端超时（毫秒），略长于后端上限，超时后 UI 必须释放。 */
const GO_MS = {
  quick: 12_000,
  status: 8_000,
  nodes: 22_000,
  switchSub: 75_000,
  refresh: 50_000,
  speedTest: 50_000,
  toggle: 80_000,
  tun: 95_000,
  selectNode: 15_000,
  subscription: 30_000,
} as const

/** 忙碌态兜底：即使 Wails 永不 resolve，也强制清掉 loading / 切换中 等标志。 */
const BUSY_SAFETY_MS = {
  loading: 70_000,
  switching: 80_000,
  speedTesting: 55_000,
  refreshing: 50_000,
} as const

function createBusySafety(flag: { value: boolean }, ms: number) {
  let timer = 0
  return {
    arm() {
      window.clearTimeout(timer)
      timer = window.setTimeout(() => {
        if (flag.value) {
          flag.value = false
        }
      }, ms)
    },
    disarm() {
      window.clearTimeout(timer)
      timer = 0
    },
  }
}

export type ProxyStatus = main.ProxyStatus
export type Subscription = main.SubscriptionItem
export type ProxyNode = backend.ProxyNode
export type TrafficInfo = main.TrafficInfo

type SubscriptionRefreshPayload = {
  id: string
  ok: boolean
  error?: string
  upload?: number
  download?: number
  total?: number
  expire?: number
  updatedAt?: number
}

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

function nodeFromMessage(msg: string): string {
  const match = msg.match(/已连接\s*-\s*(.+?)(?:\s*\(\d+ms\))?$/)
  if (!match?.[1]) {
    return ''
  }
  const name = match[1].trim()
  if (name === 'DIRECT' || name === '智能模式') {
    return ''
  }
  return name
}

function resolveNodeName(status: ProxyStatus, list: ProxyNode[]): string {
  const raw = status as unknown as Record<string, unknown>
  const direct = String(status.nodeName ?? raw.NodeName ?? '').trim()
  if (direct && direct !== 'DIRECT') {
    return direct
  }
  const fromMessage = nodeFromMessage(status.message || '')
  if (fromMessage) {
    return fromMessage
  }
  const selected = list.find((node) => node.selected)
  if (selected?.name && selected.name !== 'DIRECT') {
    return selected.name
  }
  return ''
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
  const switchingSafety = createBusySafety(switching, BUSY_SAFETY_MS.switching)
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
    if (nodeName.value && nodeName.value !== 'DIRECT' && latencyMs.value > 0) {
      return `已连接 · ${nodeName.value} (${latencyMs.value}ms)`
    }
    return message.value || '已连接'
  })

  const headerSubtitle = computed(() => {
    if (!connected.value) {
      return '未连接'
    }
    const node =
      nodeName.value.trim() ||
      nodes.value.find((item) => item.selected && item.name !== 'DIRECT')?.name ||
      nodeFromMessage(message.value)
    if (!node || node === 'DIRECT') {
      return '等待节点'
    }
    if (latencyMs.value > 0) {
      return `${node} (${latencyMs.value}ms)`
    }
    return node
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
    nodeName.value = resolveNodeName(status, nodes.value)
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

  const refreshTasks = new Map<string, Promise<void>>()
  const refreshingSubscriptionId = ref('')
  let refreshingSafetyTimer = 0
  const speedTesting = ref(false)
  const speedTestingSafety = createBusySafety(speedTesting, BUSY_SAFETY_MS.speedTesting)
  const loadingSafety = createBusySafety(loading, BUSY_SAFETY_MS.loading)

  function clearRefreshingSubscription(id?: string) {
    if (id && refreshingSubscriptionId.value !== id) {
      return
    }
    refreshingSubscriptionId.value = ''
    window.clearTimeout(refreshingSafetyTimer)
    refreshingSafetyTimer = 0
  }

  function armRefreshingSafetyClear(id: string) {
    window.clearTimeout(refreshingSafetyTimer)
    refreshingSafetyTimer = window.setTimeout(() => {
      if (refreshingSubscriptionId.value === id) {
        refreshingSubscriptionId.value = ''
        showToast('刷新超时，已恢复操作')
      }
    }, BUSY_SAFETY_MS.refreshing)
  }

  const subscriptionInteractionLocked = computed(
    () => switching.value || Boolean(refreshingSubscriptionId.value),
  )

  function patchSubscriptionFromRefresh(payload: SubscriptionRefreshPayload) {
    subscriptions.value = subscriptions.value.map((sub) =>
      sub.id === payload.id
        ? {
            ...sub,
            upload: payload.upload ?? sub.upload ?? 0,
            download: payload.download ?? sub.download ?? 0,
            total: payload.total ?? sub.total ?? 0,
            expire: payload.expire ?? sub.expire ?? 0,
            updatedAt: payload.updatedAt ?? sub.updatedAt ?? 0,
          }
        : sub,
    )
  }

  function handleSubscriptionRefreshed(payload: SubscriptionRefreshPayload) {
    if (!payload?.id) {
      return
    }
    patchSubscriptionFromRefresh(payload)
    if (payload.id === refreshingSubscriptionId.value && payload.error && payload.ok === false) {
      showToast(payload.error)
    }
    if (payload.id === refreshingSubscriptionId.value) {
      clearRefreshingSubscription(payload.id)
    }
    if (payload.ok) {
      void refreshNodes(true)
    }
  }

  async function refreshSubscriptionTraffic(id: string, silent = false) {
    if (!silent) {
      if (switching.value) {
        showToast('正在切换订阅，请稍候')
        return
      }
      if (refreshingSubscriptionId.value && refreshingSubscriptionId.value !== id) {
        showToast('正在刷新其他订阅，请稍候')
        return
      }
      refreshingSubscriptionId.value = id
      armRefreshingSafetyClear(id)
      try {
        await CancelRefreshSubscriptionTraffic(id)
      } catch {
        // ignore
      }
    } else {
      const existing = refreshTasks.get(id)
      if (existing) {
        await existing.catch(() => {})
      }
    }

    const task = (async () => {
      try {
        const updated = await withTimeout(
          RefreshSubscriptionTraffic(id),
          GO_MS.refresh,
          '刷新订阅超时，请稍后重试',
        )
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
        if (!silent) {
          showToast('已刷新流量与节点', 'ok')
        }
      } catch (err) {
        if (!silent && !isRefreshCancelled(err)) {
          showToast(errorMessage(err))
        }
      } finally {
        if (!silent) {
          clearRefreshingSubscription(id)
        }
      }
    })()

    refreshTasks.set(id, task)
    try {
      await task
    } finally {
      if (refreshTasks.get(id) === task) {
        refreshTasks.delete(id)
      }
    }
  }

  async function cancelAllSubscriptionRefresh() {
    clearRefreshingSubscription()
    try {
      await CancelAllSubscriptionRefresh()
    } catch {
      // ignore cancel errors
    }
    await Promise.all([...refreshTasks.values()].map((task) => task.catch(() => {})))
  }

  async function cancelRefreshSubscriptionTraffic(id: string) {
    clearRefreshingSubscription(id)
    try {
      await CancelRefreshSubscriptionTraffic(id)
    } catch {
      // ignore cancel errors
    }
    const task = refreshTasks.get(id)
    if (task) {
      await task.catch(() => {})
    }
  }

  async function refreshAllSubscriptionTraffic() {
    for (const sub of subscriptions.value) {
      await refreshSubscriptionTraffic(sub.id, true)
    }
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

  async function refreshNodes(silent = false) {
    if (switching.value || refreshingSubscriptionId.value) {
      return
    }
    if (!connected.value) {
      nodes.value = []
      return
    }
    try {
      nodes.value =
        (await withTimeout(GetNodes(), GO_MS.nodes, '获取节点列表超时')) || []
      if (!nodeName.value || nodeName.value === 'DIRECT') {
        const selected = nodes.value.find((node) => node.selected)
        if (selected?.name && selected.name !== 'DIRECT') {
          nodeName.value = selected.name
        }
      }
    } catch (err) {
      nodes.value = []
      if (!silent) {
        showToast(errorMessage(err))
      }
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
    loadingSafety.arm()
    const before = subscriptions.value.length
    try {
      subscriptions.value = asSubscriptionList(
        await withTimeout(
          AddSubscription(url, remark),
          GO_MS.subscription,
          '添加订阅超时，请稍后重试',
        ),
      ) as Subscription[]
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
      loadingSafety.disarm()
    }
  }

  async function removeSubscription(id: string) {
    if (loading.value) {
      return
    }
    loading.value = true
    loadingSafety.arm()
    try {
      subscriptions.value = asSubscriptionList(
        await withTimeout(
          RemoveSubscription(id),
          GO_MS.subscription,
          '删除订阅超时，请稍后重试',
        ),
      ) as Subscription[]
      showToast('订阅已删除')
      await refresh()
    } catch (err) {
      showToast(errorMessage(err))
    } finally {
      loading.value = false
      loadingSafety.disarm()
    }
  }

  async function toggleSubscription(item: Subscription) {
    if (switching.value) {
      return
    }
    if (refreshingSubscriptionId.value) {
      showToast('正在刷新订阅，请稍候或点刷新按钮取消')
      return
    }
    void cancelAllSubscriptionRefresh().catch(() => {})
    switching.value = true
    switchingSafety.arm()
    const turningOff = Boolean(item.enabled && connected.value)
    const prevConnected = connected.value
    const prevNodeName = nodeName.value
    subscriptions.value = subscriptions.value.map((sub) => ({
      ...sub,
      enabled: turningOff ? false : sub.id === item.id,
    }))
    if (!turningOff) {
      connected.value = true
      nodes.value = []
    }
    try {
      const status = await withTimeout(
        UseSubscription(item.id),
        GO_MS.switchSub,
        '切换订阅超时，请查看日志或重试',
      )
      applyStatus(status)
      await refreshSubscriptions()
      void (async () => {
        for (let attempt = 0; attempt < 12; attempt += 1) {
          try {
            applyStatus(
              await withTimeout(GetStatus(), GO_MS.status, '读取状态超时'),
            )
          } catch {
            break
          }
          await refreshNodes(true)
          if (nodeName.value && nodeName.value !== 'DIRECT' && nodes.value.length > 0) {
            break
          }
          await sleep(500)
        }
        showToast(turningOff ? '已停用' : '已开始使用该订阅', turningOff ? 'err' : 'ok')
      })()
    } catch (err) {
      connected.value = prevConnected
      nodeName.value = prevNodeName
      showToast(errorMessage(err))
      await refresh()
    } finally {
      switching.value = false
      switchingSafety.disarm()
    }
  }

  async function updateSubscription(id: string, url: string, remark: string) {
    const nextUrl = url.trim()
    if (!nextUrl) {
      showToast('请填写订阅链接')
      return
    }
    const prev = subscriptions.value.find((s) => s.id === id)
    const urlChanged = prev ? prev.url.trim() !== nextUrl : true
    try {
      subscriptions.value = asSubscriptionList(
        await UpdateSubscription(id, urlChanged ? nextUrl : '', remark.trim()),
      ) as Subscription[]
      showToast('订阅已更新', 'ok')
      await refreshSubscriptions()
      if (connected.value) {
        await refreshNodes()
      }
    } catch (err) {
      showToast(errorMessage(err))
    }
  }

  async function updateRemark(id: string, remark: string) {
    await updateSubscription(id, subscriptions.value.find((s) => s.id === id)?.url || '', remark)
  }

  async function selectNode(name: string) {
    if (loading.value || switching.value || speedTesting.value) {
      return
    }
    loading.value = true
    loadingSafety.arm()
    try {
      applyStatus(
        await withTimeout(SelectNode(name), GO_MS.selectNode, '切换节点超时，请重试'),
      )
      await refreshNodes()
    } catch (err) {
      showToast(errorMessage(err))
    } finally {
      loading.value = false
      loadingSafety.disarm()
    }
  }

  async function speedTest() {
    if (speedTesting.value) {
      return
    }
    if (!connected.value) {
      await requireService()
      return
    }
    speedTesting.value = true
    speedTestingSafety.arm()
    try {
      await refreshNodes(true)
      applyStatus(
        await withTimeout(
          AutoSelectBestNode(),
          GO_MS.speedTest,
          '测速超时，请稍后重试',
        ),
      )
      await refreshNodes()
    } catch (err) {
      if (!errorMessage(err).includes('测速已取消')) {
        notifyError(err)
      }
    } finally {
      speedTesting.value = false
      speedTestingSafety.disarm()
    }
  }

  async function toggle() {
    if (loading.value) {
      return
    }
    loading.value = true
    loadingSafety.arm()
    try {
      applyStatus(
        await withTimeout(ToggleProxy(), GO_MS.toggle, '连接操作超时，请重试'),
      )
      await refreshSubscriptions()
      await refreshNodes()
    } catch (err) {
      notifyError(err)
      await refresh()
    } finally {
      loading.value = false
      loadingSafety.disarm()
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
    loadingSafety.arm()
    try {
      applyStatus(
        await withTimeout(SetTunMode(enabled), GO_MS.tun, '切换 TUN 超时，请重试'),
      )
      await refreshNodes()
    } catch (err) {
      showToast(errorMessage(err))
    } finally {
      loading.value = false
      loadingSafety.disarm()
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
      if (connected.value && (!nodeName.value || nodeName.value === 'DIRECT')) {
        if (switching.value || refreshingSubscriptionId.value) {
          return
        }
        try {
          applyStatus(await withTimeout(GetStatus(), GO_MS.status, '读取状态超时'))
        } catch {
          return
        }
        if (!nodeName.value || nodeName.value === 'DIRECT') {
          await refreshNodes(true)
          const selected = nodes.value.find((node) => node.selected)
          if (selected?.name && selected.name !== 'DIRECT') {
            nodeName.value = selected.name
          }
        }
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
    switching,
    refreshingSubscriptionId,
    subscriptionInteractionLocked,
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
    headerSubtitle,
    statusLabel,
    applyStatus,
    speedTesting,
    showToast,
    dismissModal,
    requireService,
    goHome,
    refresh,
    refreshNodes,
    refreshSubscriptionTraffic,
    cancelRefreshSubscriptionTraffic,
    cancelAllSubscriptionRefresh,
    handleSubscriptionRefreshed,
    refreshAllSubscriptionTraffic,
    addSubscription,
    onAddClick,
    removeSubscription,
    toggleSubscription,
    updateRemark,
    updateSubscription,
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
