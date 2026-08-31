export type SubscriptionItem = {
  id: string
  url: string
  remark: string
  enabled: boolean
}

function pickString(raw: Record<string, unknown>, keys: string[]): string {
  for (const key of keys) {
    const value = raw[key]
    if (typeof value === 'string') {
      return value
    }
  }
  return ''
}

export function normalizeSubscription(raw: unknown): SubscriptionItem | null {
  if (!raw || typeof raw !== 'object') {
    return null
  }
  const rec = raw as Record<string, unknown>
  const id = pickString(rec, ['id', 'ID', 'Id'])
  const url = pickString(rec, ['url', 'URL', 'Url'])
  if (!id && !url) {
    return null
  }
  return {
    id,
    url,
    remark: pickString(rec, ['remark', 'Remark']),
    enabled: Boolean(rec.enabled ?? rec.Enabled),
  }
}

export function asSubscriptionList(raw: unknown): SubscriptionItem[] {
  if (raw == null || raw === '') {
    return []
  }
  if (typeof raw === 'string') {
    try {
      return asSubscriptionList(JSON.parse(raw))
    } catch {
      return []
    }
  }
  if (Array.isArray(raw)) {
    return raw
      .map(normalizeSubscription)
      .filter((item): item is SubscriptionItem => item !== null)
  }
  if (typeof raw !== 'object') {
    return []
  }
  const rec = raw as Record<string, unknown>
  for (const key of ['items', 'Items', 'subscriptions', 'Subscriptions']) {
    if (rec[key] != null) {
      const nested = asSubscriptionList(rec[key])
      if (nested.length > 0) {
        return nested
      }
    }
  }
  const single = normalizeSubscription(raw)
  if (single) {
    return [single]
  }
  const values = Object.values(rec)
  if (values.length > 0 && values.every((value) => value && typeof value === 'object')) {
    return asSubscriptionList(values)
  }
  return []
}

type GoBindings = {
  go?: {
    main?: {
      App?: {
        GetSubscriptions?: unknown
      }
    }
  }
}

export function goBindingsReady(): boolean {
  try {
    return typeof (window as GoBindings).go?.main?.App?.GetSubscriptions === 'function'
  } catch {
    return false
  }
}

export function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => {
    window.setTimeout(resolve, ms)
  })
}

export async function waitForGoBindings(timeoutMs = 8000): Promise<boolean> {
  const start = Date.now()
  while (!goBindingsReady()) {
    if (Date.now() - start >= timeoutMs) {
      return false
    }
    await sleep(50)
  }
  return true
}
