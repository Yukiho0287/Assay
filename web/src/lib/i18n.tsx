import { createContext, useCallback, useContext, useMemo, useState } from 'react'

// 轻量字典式 i18n：zh 为唯一真相源，en 的键由类型系统强制与 zh 对齐
const zh = {
  'brand.subtitle': 'LLM 渠道检测',
  'nav.overview': '总览',
  'nav.channels': '渠道',
  'nav.quality': '质量检测',
  'nav.stability': '稳定性检测',
  'nav.settings': '设置',
  'sidebar.collapse': '收起侧栏',
  'sidebar.expand': '展开侧栏',
  'sidebar.logout': '退出',
  'header.github': '打开 GitHub 仓库',
  'header.theme': '切换深浅色',
  'header.lang': 'Switch to English',

  'login.desc': 'LLM 渠道检测平台，请登录',
  'login.username': '用户名',
  'login.password': '密码',
  'login.submit': '登录',
  'login.pending': '登录中…',
  'login.networkError': '网络错误，请重试',

  'dash.desc': '平台概况与最近检测动态（待实现）。',
  'dash.version': '服务版本',

  'channels.desc': '保存渠道接入信息与模型条目，检测任务从这里选取检测对象（渠道 × 模型条目）。',
  'channels.create': '新建渠道',
  'channels.name': '名称',
  'channels.baseUrl': 'Base URL',
  'channels.baseUrlHint': '填到版本段（如 https://api.example.com/v1），平台按协议自动拼接末段',
  'channels.apiKey': 'API key',
  'channels.apiKeyHint': '写后不可回读，仅展示脱敏前缀',
  'channels.apiKeyEditHint': 'API key（留空则不修改）',
  'channels.protocols': '支持协议',
  'channels.currency': '定价币种',
  'channels.note': '备注',
  'channels.modelCount': '模型数',
  'channels.connectivity': '连通性',
  'channels.untested': '未测试',
  'channels.disabled': '已停用',
  'channels.createdAt': '创建时间',
  'channels.empty': '还没有渠道，点击右上角新建。',
  'channels.notFound': '渠道不存在或已被删除',
  'channels.basicInfo': '基本信息',
  'channels.keyPrefix': '凭证前缀',
  'channels.edit': '编辑渠道',
  'channels.enable': '启用',
  'channels.disable': '停用',
  'channels.disabledHint': '停用后不可选入新检测任务，配置与凭证保留',
  'channels.deleteTitle': '删除渠道',
  'channels.deleteDesc':
    '确认删除该渠道？渠道与全部模型条目将被永久删除，API key 不可恢复；已有检测任务持有参数快照，不受影响。',

  'models.title': '模型条目',
  'models.desc': '渠道下的可测模型与定价（每 1M token），检测对象 = 渠道 × 模型条目。',
  'models.add': '添加模型',
  'models.name': '模型名',
  'models.inputPrice': '输入价',
  'models.outputPrice': '输出价',
  'models.cachedInputPrice': '缓存读价',
  'models.priceHint': '价格可选（每 1M token）；输入/输出价须成对填写，缓存读价需先填前两者',
  'models.unpriced': '未定价',
  'models.editTitle': '编辑模型条目',
  'models.deleteTitle': '删除模型条目',
  'models.deleteDesc': '确认删除该模型条目？已有检测任务持有参数快照，不受影响。',
  'models.empty': '还没有模型条目。',

  'test.title': '连通测试',
  'test.desc': '对渠道声明的每个协议发送 max_tokens=1 的最小真实请求（产生最低限度费用），记录首字延迟。',
  'test.model': '探测模型',
  'test.run': '发起测试',
  'test.running': '测试中…',
  'test.lastResult': '最近一次测试',
  'test.protocol': '协议',
  'test.ttft': '首字延迟',
  'test.ok': '正常',
  'test.fail': '失败',
  'test.testedAt': '测试时间',
  'test.noModels': '先添加模型条目才能发起连通测试',
  'proto.openai_chat': 'OpenAI Chat',
  'proto.openai_responses': 'OpenAI Responses',
  'proto.anthropic_messages': 'Anthropic Messages',

  'quality.desc': '勾选检测项对渠道发起质量检测（tokenizer、缓存计账、协议一致性等），查看任务历史与报告。待实现。',
  'stability.desc': '并发、RPM、首字延迟等稳定性指标检测，支持选取已保存渠道作为对照。待实现。',

  'forbidden.title': '无权访问',
  'forbidden.desc': '当前账号没有该模块的访问权限，请联系管理员调整角色。',

  'settings.desc': '个人设置与平台管理。',
  'personal.title': '个人设置',
  'personal.desc': '修改自己的登录密码，修改成功后其他设备会被下线。',
  'personal.currentPassword': '当前密码',
  'personal.newPassword': '新密码（至少 8 位）',
  'personal.confirmPassword': '确认新密码',
  'personal.mismatch': '两次输入的新密码不一致',
  'personal.submit': '修改密码',
  'personal.success': '密码已修改，其他设备已下线',

  'users.title': '用户管理',
  'users.desc': '平台不开放注册，账号在此创建并分配角色。',
  'users.create': '新建用户',
  'users.username': '用户名',
  'users.role': '角色',
  'users.createdAt': '创建时间',
  'users.actions': '操作',
  'users.password': '初始密码（至少 8 位）',
  'users.editTitle': '编辑用户',
  'users.resetPassword': '重置密码（留空则不修改）',
  'users.deleteTitle': '删除用户',
  'users.deleteDesc': '确认删除该用户？其全部会话将立即失效，该操作不可撤销。',

  'roles.title': '角色管理',
  'roles.desc': '粗粒度权限：按模块控制页面与接口的访问；内置角色不可修改。',
  'roles.create': '新建角色',
  'roles.name': '角色名',
  'roles.permissions': '可访问模块',
  'roles.builtIn': '内置',
  'roles.editTitle': '编辑角色',
  'roles.deleteTitle': '删除角色',
  'roles.deleteDesc': '确认删除该角色？该操作不可撤销。',
  'perm.channels': '渠道',
  'perm.quality': '质量检测',
  'perm.stability': '稳定性检测',
  'perm.users': '用户与角色',
  'perm.system': '系统管理',

  'update.title': '在线更新',
  'update.desc': '检查 GitHub 最新发布版本，一键触发蓝绿部署。',
  'update.current': '当前版本',
  'update.latest': '最新版本',
  'update.publishedAt': '发布于',
  'update.notes': '更新说明',
  'update.check': '重新检查',
  'update.checking': '检查中…',
  'update.upToDate': '已是最新版本',
  'update.available': '发现新版本',
  'update.deploy': '立即更新',
  'update.deploying': '已触发部署，等待新版本上线（无迁移时零停机，约 1-2 分钟）…',
  'update.done': '更新完成，页面即将刷新',
  'update.timeout': '等待超时：部署可能仍在进行，请稍后重新打开本窗口确认版本',
  'update.noToken': '服务器未配置 ASSAY_GITHUB_TOKEN，无法在线检查与更新。请在服务器 /opt/assay/env 中配置后重启服务。',

  'common.back': '返回',
  'common.cancel': '取消',
  'common.create': '创建',
  'common.save': '保存',
  'common.edit': '编辑',
  'common.delete': '删除',
  'common.loading': '加载中…',
} as const

export type DictKey = keyof typeof zh

const en: Record<DictKey, string> = {
  'brand.subtitle': 'LLM Channel Testing',
  'nav.overview': 'Overview',
  'nav.channels': 'Channels',
  'nav.quality': 'Quality',
  'nav.stability': 'Stability',
  'nav.settings': 'Settings',
  'sidebar.collapse': 'Collapse sidebar',
  'sidebar.expand': 'Expand sidebar',
  'sidebar.logout': 'Log out',
  'header.github': 'Open GitHub repository',
  'header.theme': 'Toggle theme',
  'header.lang': '切换为中文',

  'login.desc': 'Sign in to the LLM channel testing platform',
  'login.username': 'Username',
  'login.password': 'Password',
  'login.submit': 'Sign in',
  'login.pending': 'Signing in…',
  'login.networkError': 'Network error, please retry',

  'dash.desc': 'Platform overview and recent activity (coming soon).',
  'dash.version': 'Server version',

  'channels.desc':
    'Store channel endpoints and model entries; test tasks pick their targets (channel × model entry) here.',
  'channels.create': 'New channel',
  'channels.name': 'Name',
  'channels.baseUrl': 'Base URL',
  'channels.baseUrlHint':
    'Up to the version segment (e.g. https://api.example.com/v1); the platform appends the protocol-specific suffix',
  'channels.apiKey': 'API key',
  'channels.apiKeyHint': 'Write-only; only a masked prefix is shown afterwards',
  'channels.apiKeyEditHint': 'API key (leave empty to keep)',
  'channels.protocols': 'Protocols',
  'channels.currency': 'Pricing currency',
  'channels.note': 'Note',
  'channels.modelCount': 'Models',
  'channels.connectivity': 'Connectivity',
  'channels.untested': 'Untested',
  'channels.disabled': 'Disabled',
  'channels.createdAt': 'Created',
  'channels.empty': 'No channels yet. Create one from the top right.',
  'channels.notFound': 'Channel not found or deleted',
  'channels.basicInfo': 'Basic info',
  'channels.keyPrefix': 'Key prefix',
  'channels.edit': 'Edit channel',
  'channels.enable': 'Enable',
  'channels.disable': 'Disable',
  'channels.disabledHint': 'Disabled channels cannot be picked for new tasks; config and key are kept',
  'channels.deleteTitle': 'Delete channel',
  'channels.deleteDesc':
    'Delete this channel? The channel and all model entries are removed permanently and the API key cannot be recovered. Existing tasks keep their parameter snapshots.',

  'models.title': 'Model entries',
  'models.desc': 'Testable models and pricing (per 1M tokens); a test target = channel × model entry.',
  'models.add': 'Add model',
  'models.name': 'Model name',
  'models.inputPrice': 'Input',
  'models.outputPrice': 'Output',
  'models.cachedInputPrice': 'Cached input',
  'models.priceHint':
    'Prices optional (per 1M tokens); input/output must be set together, cached input requires both',
  'models.unpriced': 'Unpriced',
  'models.editTitle': 'Edit model entry',
  'models.deleteTitle': 'Delete model entry',
  'models.deleteDesc': 'Delete this model entry? Existing tasks keep their parameter snapshots.',
  'models.empty': 'No model entries yet.',

  'test.title': 'Connectivity test',
  'test.desc':
    'Sends a minimal real request (max_tokens=1, minimal cost) per declared protocol and records the time to first token.',
  'test.model': 'Probe model',
  'test.run': 'Run test',
  'test.running': 'Testing…',
  'test.lastResult': 'Last test',
  'test.protocol': 'Protocol',
  'test.ttft': 'TTFT',
  'test.ok': 'OK',
  'test.fail': 'Failed',
  'test.testedAt': 'Tested at',
  'test.noModels': 'Add a model entry before running a connectivity test',
  'proto.openai_chat': 'OpenAI Chat',
  'proto.openai_responses': 'OpenAI Responses',
  'proto.anthropic_messages': 'Anthropic Messages',

  'quality.desc':
    'Run quality probes against channels (tokenizer, cache billing, protocol conformance) and browse reports. Coming soon.',
  'stability.desc':
    'Stability metrics such as concurrency, RPM and TTFT, with saved channels as control groups. Coming soon.',

  'forbidden.title': 'Access denied',
  'forbidden.desc': 'Your account has no access to this module. Ask an administrator to adjust your role.',

  'settings.desc': 'Personal settings and platform administration.',
  'personal.title': 'Personal settings',
  'personal.desc': 'Change your password. Other sessions are signed out on success.',
  'personal.currentPassword': 'Current password',
  'personal.newPassword': 'New password (min 8 chars)',
  'personal.confirmPassword': 'Confirm new password',
  'personal.mismatch': 'The two new passwords do not match',
  'personal.submit': 'Change password',
  'personal.success': 'Password changed; other sessions signed out',

  'users.title': 'Users',
  'users.desc': 'Registration is closed; accounts are created here with a role.',
  'users.create': 'New user',
  'users.username': 'Username',
  'users.role': 'Role',
  'users.createdAt': 'Created',
  'users.actions': 'Actions',
  'users.password': 'Initial password (min 8 chars)',
  'users.editTitle': 'Edit user',
  'users.resetPassword': 'Reset password (leave empty to keep)',
  'users.deleteTitle': 'Delete user',
  'users.deleteDesc': 'Delete this user? All their sessions become invalid immediately. This cannot be undone.',

  'roles.title': 'Roles',
  'roles.desc': 'Coarse-grained permissions: per-module page and API access. Built-in roles are immutable.',
  'roles.create': 'New role',
  'roles.name': 'Role name',
  'roles.permissions': 'Accessible modules',
  'roles.builtIn': 'Built-in',
  'roles.editTitle': 'Edit role',
  'roles.deleteTitle': 'Delete role',
  'roles.deleteDesc': 'Delete this role? This cannot be undone.',
  'perm.channels': 'Channels',
  'perm.quality': 'Quality',
  'perm.stability': 'Stability',
  'perm.users': 'Users & roles',
  'perm.system': 'System',

  'update.title': 'Online update',
  'update.desc': 'Check the latest GitHub release and trigger a blue-green deployment.',
  'update.current': 'Current version',
  'update.latest': 'Latest version',
  'update.publishedAt': 'Published',
  'update.notes': 'Release notes',
  'update.check': 'Check again',
  'update.checking': 'Checking…',
  'update.upToDate': 'Already up to date',
  'update.available': 'New version available',
  'update.deploy': 'Update now',
  'update.deploying': 'Deployment triggered; waiting for the new version (zero downtime without migrations, ~1-2 min)…',
  'update.done': 'Update complete; reloading…',
  'update.timeout': 'Timed out: the deployment may still be running. Reopen this dialog later to verify.',
  'update.noToken':
    'ASSAY_GITHUB_TOKEN is not configured on the server, so online update is unavailable. Configure it in /opt/assay/env and restart.',

  'common.back': 'Back',
  'common.cancel': 'Cancel',
  'common.create': 'Create',
  'common.save': 'Save',
  'common.edit': 'Edit',
  'common.delete': 'Delete',
  'common.loading': 'Loading…',
}

const dictionaries = { zh, en } as const
export type Lang = keyof typeof dictionaries

const LANG_KEY = 'assay-lang'

function initialLang(): Lang {
  return localStorage.getItem(LANG_KEY) === 'en' ? 'en' : 'zh'
}

interface I18nValue {
  lang: Lang
  setLang: (lang: Lang) => void
  t: (key: DictKey) => string
}

const I18nContext = createContext<I18nValue | null>(null)

export function I18nProvider({ children }: { children: React.ReactNode }) {
  const [lang, setLangState] = useState<Lang>(initialLang)

  const setLang = useCallback((next: Lang) => {
    localStorage.setItem(LANG_KEY, next)
    document.documentElement.lang = next === 'zh' ? 'zh-CN' : 'en'
    setLangState(next)
  }, [])

  const value = useMemo<I18nValue>(
    () => ({ lang, setLang, t: (key) => dictionaries[lang][key] }),
    [lang, setLang],
  )

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>
}

export function useI18n(): I18nValue {
  const ctx = useContext(I18nContext)
  if (!ctx) throw new Error('useI18n 必须在 I18nProvider 内使用')
  return ctx
}
