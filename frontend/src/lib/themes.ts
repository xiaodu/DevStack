export type ThemeMode = 'light' | 'dark'

export interface ThemeOption {
  id: string
  name: string
  en: string
  mode: ThemeMode
}

// 与 src/style.css 中 @plugin "daisyui" 的 themes 列表保持一致。
export const THEMES: ThemeOption[] = [
  { id: 'light', name: '明亮', en: 'Light', mode: 'light' },
  { id: 'corporate', name: '企业', en: 'Corporate', mode: 'light' },
  { id: 'emerald', name: '翡翠', en: 'Emerald', mode: 'light' },
  { id: 'nord', name: '北欧', en: 'Nord', mode: 'light' },
  { id: 'winter', name: '冬雪', en: 'Winter', mode: 'light' },
  { id: 'silk', name: '丝缎', en: 'Silk', mode: 'light' },
  { id: 'caramellatte', name: '焦糖', en: 'Caramellate', mode: 'light' },
  { id: 'dark', name: '深黑', en: 'Dark', mode: 'dark' },
  { id: 'dim', name: '柔和暗色', en: 'Dim', mode: 'dark' },
  { id: 'night', name: '夜空', en: 'Night', mode: 'dark' },
  { id: 'business', name: '商务暗灰', en: 'Business', mode: 'dark' },
  { id: 'dracula', name: '德古拉', en: 'Dracula', mode: 'dark' },
  { id: 'coffee', name: '咖啡', en: 'Coffee', mode: 'dark' },
]

export const DEFAULT_THEME = 'light'

export const LIGHT_THEMES = THEMES.filter((t) => t.mode === 'light')
export const DARK_THEMES = THEMES.filter((t) => t.mode === 'dark')

export function findTheme(id?: string): ThemeOption {
  return THEMES.find((t) => t.id === id) ?? THEMES[0]
}

// 同时写入 data-theme（DaisyUI 变量）与 data-theme-mode（项目暗色适配选择器），
// 新主题只要在 THEMES 中标注 mode 即可获得正确的侧边栏、边框与原生控件配色。
export function applyTheme(theme?: string) {
  if (typeof document === 'undefined') return
  const option = findTheme(theme)
  const root = document.documentElement
  root.setAttribute('data-theme', option.id)
  root.setAttribute('data-theme-mode', option.mode)
  root.style.colorScheme = option.mode
}
