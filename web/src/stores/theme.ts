import { reactive, watchEffect } from 'vue'

export type Theme = 'light' | 'dark'

const STORAGE_KEY = 'ttyfish-theme'

/**
 * 主题存在 localStorage 里。
 *
 * 这是纯显示偏好，没有任何凭证成分——令牌依然只在内存里，
 * 所以这里用 localStorage 是安全的（刷新也不会掉登录，登录靠内存令牌）。
 */
function initialTheme(): Theme {
  try {
    const saved = localStorage.getItem(STORAGE_KEY)
    if (saved === 'light' || saved === 'dark') {
      return saved
    }
  } catch {
    // 隐私模式下 localStorage 会抛异常，忽略即可
  }
  // 默认深色
  return 'dark'
}

const state = reactive<{ theme: Theme }>({ theme: initialTheme() })

/** 把主题写到 <html class="dark"> 上，Element Plus 靠这个类切暗色变量。 */
function apply(): void {
  const root = document.documentElement
  root.classList.toggle('dark', state.theme === 'dark')
  root.style.colorScheme = state.theme
  try {
    localStorage.setItem(STORAGE_KEY, state.theme)
  } catch {
    // 存不了就只影响"下次打开还记得"，不影响当前使用
  }
}

apply()
watchEffect(apply)

export function useTheme() {
  return {
    state,
    toggle(): void {
      state.theme = state.theme === 'dark' ? 'light' : 'dark'
    },
    set(theme: Theme): void {
      state.theme = theme
    },
  }
}
