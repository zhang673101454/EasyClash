export function formatBytes(bytes: number): string {
  if (!bytes || bytes <= 0) {
    return '0'
  }
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit += 1
  }
  if (unit >= 3) {
    return `${value.toFixed(2)}${units[unit]}`
  }
  if (unit >= 2) {
    return `${value.toFixed(1)}${units[unit]}`
  }
  if (unit >= 1) {
    return `${value.toFixed(0)}${units[unit]}`
  }
  return `${Math.round(value)}${units[unit]}`
}

export function formatExpire(unixSec: number): string {
  if (!unixSec || unixSec <= 0) {
    return ''
  }
  const date = new Date(unixSec * 1000)
  const y = date.getFullYear()
  const m = String(date.getMonth() + 1).padStart(2, '0')
  const d = String(date.getDate()).padStart(2, '0')
  return `${y}-${m}-${d}`
}

export function formatRelativeTime(unixSec: number): string {
  if (!unixSec || unixSec <= 0) {
    return ''
  }
  const diffSec = Math.max(0, Math.floor(Date.now() / 1000 - unixSec))
  if (diffSec < 60) {
    return '刚刚'
  }
  if (diffSec < 3600) {
    return `${Math.floor(diffSec / 60)} 分钟前`
  }
  if (diffSec < 86400) {
    return `${Math.floor(diffSec / 3600)} 小时前`
  }
  return `${Math.floor(diffSec / 86400)} 天前`
}

export function trafficPercent(upload: number, download: number, total: number): number {
  if (!total || total <= 0) {
    return 0
  }
  const used = Math.max(0, upload) + Math.max(0, download)
  return Math.min(100, Math.max(0, (used / total) * 100))
}

export function hasTrafficInfo(item: { total?: number; upload?: number; download?: number }): boolean {
  const total = item.total || 0
  if (total > 0) {
    return true
  }
  return (item.upload || 0) + (item.download || 0) > 0
}
