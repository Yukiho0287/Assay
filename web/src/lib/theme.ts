// 主题切换：document 根元素挂/摘 .dark 类（shadcn CSS 变量随之反色），localStorage 持久化
const THEME_KEY = 'assay-theme'

export function applyStoredTheme() {
  // 默认深色：仅在用户显式选过浅色时才用浅色
  document.documentElement.classList.toggle('dark', localStorage.getItem(THEME_KEY) !== 'light')
}

export function isDark(): boolean {
  return document.documentElement.classList.contains('dark')
}

export function toggleTheme(): boolean {
  const dark = !isDark()
  document.documentElement.classList.toggle('dark', dark)
  localStorage.setItem(THEME_KEY, dark ? 'dark' : 'light')
  return dark
}
