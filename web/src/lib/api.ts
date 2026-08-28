import type { components } from '@/api/schema'

export type CurrentUser = components['schemas']['CurrentUser']
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

export const authApi = {
  login: (username: string, password: string) =>
    request<void>('/api/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    }),
  logout: () => request<void>('/api/auth/logout', { method: 'POST' }),
  me: () => request<CurrentUser>('/api/auth/me'),
}
