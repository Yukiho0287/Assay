// 主题切换：document 根元素挂/摘 .dark 类（shadcn CSS 变量随之反色），localStorage 持久化
const THEME_KEY = 'assay-theme'

export function applyStoredTheme() {
  document.documentElement.classList.toggle('dark', localStorage.getItem(THEME_KEY) === 'dark')
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
