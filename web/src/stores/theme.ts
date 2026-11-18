import { create } from 'zustand'

/** 主题模式：纸墨亮（纸）/ 纸墨暗（墨）。 */
export type ThemeMode = 'light' | 'dark'

/** data-theme 属性挂载点：documentElement（dark.css 按 [data-theme='dark'] 生效）。 */
export function applyTheme(mode: ThemeMode): void {
  if (mode === 'dark') {
    document.documentElement.dataset.theme = 'dark'
  } else {
    delete document.documentElement.dataset.theme
  }
}

/** localStorage 持久化键（index.html 头部内联脚本同键预置，避免首帧闪烁）。 */
export const THEME_STORAGE_KEY = 'ui-theme'

export interface ThemeState {
  readonly mode: ThemeMode
  /** 从 DOM 预置状态同步进 store（index.html 内联脚本已写 data-theme）。 */
  initFromDocument: () => void
  setMode: (mode: ThemeMode) => void
  toggle: () => void
}

function readInitialMode(): ThemeMode {
  try {
    const stored = window.localStorage.getItem(THEME_STORAGE_KEY)
    if (stored === 'dark' || stored === 'light') return stored
  } catch {
    // localStorage 不可用（隐私模式等）：跟随系统
  }
  if (window.matchMedia !== undefined && window.matchMedia('(prefers-color-scheme: dark)').matches) {
    return 'dark'
  }
  return 'light'
}

/** 主题 store（UI 升级新增）：模块级单例，与其它 store 同风格；不含业务逻辑。 */
export const themeStore = create<ThemeState>()((set, get) => ({
  mode: 'light',
  initFromDocument: () => {
    const mode = readInitialMode()
    applyTheme(mode)
    set({ mode })
  },
  setMode: (mode) => {
    applyTheme(mode)
    try {
      window.localStorage.setItem(THEME_STORAGE_KEY, mode)
    } catch {
      // 持久化失败不阻塞切换
    }
    set({ mode })
  },
  toggle: () => {
    const next: ThemeMode = get().mode === 'dark' ? 'light' : 'dark'
    get().setMode(next)
  },
}))
