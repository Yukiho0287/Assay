import type { components } from '@/api/schema'

export type CurrentUser = components['schemas']['CurrentUser']
export type VersionInfo = components['schemas']['VersionInfo']
export type PermissionMap = components['schemas']['PermissionMap']
export type User = components['schemas']['User']
export type UserCreate = components['schemas']['UserCreate']
export type UserUpdate = components['schemas']['UserUpdate']
export type Role = components['schemas']['Role']
export type RoleCreate = components['schemas']['RoleCreate']
export type RoleUpdate = components['schemas']['RoleUpdate']
export type UpdateStatus = components['schemas']['UpdateStatus']
export type ApiError = components['schemas']['Error']

// 统一请求封装：非 2xx 抛出带后端 error 文案的异常
export class RequestError extends Error {
  status: number

  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    headers: init?.body ? { 'Content-Type': 'application/json' } : undefined,
    ...init,
  })
  if (!res.ok) {
    let message = `请求失败（${res.status}）`
    try {
      const data = (await res.json()) as ApiError
      if (data.error) message = data.error
    } catch {
      // 响应无 JSON body 时保留默认文案
    }
    throw new RequestError(res.status, message)
  }
  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

export const systemApi = {
  version: () => request<VersionInfo>('/api/version'),
  updateStatus: () => request<UpdateStatus>('/api/system/update'),
  triggerDeploy: (tag: string) =>
    request<void>('/api/system/update/deploy', {
      method: 'POST',
      body: JSON.stringify({ tag }),
    }),
}

export const authApi = {
  login: (username: string, password: string) =>
    request<void>('/api/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    }),
  logout: () => request<void>('/api/auth/logout', { method: 'POST' }),
  me: () => request<CurrentUser>('/api/auth/me'),
  changePassword: (currentPassword: string, newPassword: string) =>
    request<void>('/api/auth/password', {
      method: 'PUT',
      body: JSON.stringify({ currentPassword, newPassword }),
    }),
}

export const usersApi = {
  list: () => request<User[]>('/api/users'),
  create: (body: UserCreate) =>
    request<User>('/api/users', { method: 'POST', body: JSON.stringify(body) }),
  update: (id: string, body: UserUpdate) =>
    request<User>(`/api/users/${id}`, { method: 'PATCH', body: JSON.stringify(body) }),
  remove: (id: string) => request<void>(`/api/users/${id}`, { method: 'DELETE' }),
}

export const rolesApi = {
  list: () => request<Role[]>('/api/roles'),
  create: (body: RoleCreate) =>
    request<Role>('/api/roles', { method: 'POST', body: JSON.stringify(body) }),
  update: (id: string, body: RoleUpdate) =>
    request<Role>(`/api/roles/${id}`, { method: 'PATCH', body: JSON.stringify(body) }),
  remove: (id: string) => request<void>(`/api/roles/${id}`, { method: 'DELETE' }),
}
