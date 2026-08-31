import { WindowSetBackgroundColour } from '../../wailsjs/runtime/runtime'

export type Theme = 'dark' | 'light'

const KEY = 'easy-clash-theme'
const LEGACY_KEY = 'simple-proxy-theme'

export function getTheme(): Theme {
  try {
    const current = localStorage.getItem(KEY)
    if (current === 'light' || current === 'dark') {
      return current
    }
    const legacy = localStorage.getItem(LEGACY_KEY)
    if (legacy === 'light' || legacy === 'dark') {
      localStorage.setItem(KEY, legacy)
      return legacy
    }
  } catch {
    /* ignore */
  }
  return 'dark'
}

function syncWindowBackground(_theme: Theme) {
  try {
    WindowSetBackgroundColour(0, 0, 0, 0)
  } catch {
    /* 浏览器预览时没有 Wails runtime */
  }
}

export function applyTheme(theme: Theme) {
  document.documentElement.dataset.theme = theme
  syncWindowBackground(theme)
  try {
    localStorage.setItem(KEY, theme)
  } catch {
    /* ignore */
  }
}

export function toggleTheme(): Theme {
  const next: Theme = getTheme() === 'dark' ? 'light' : 'dark'
  applyTheme(next)
  return next
}

