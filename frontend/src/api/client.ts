import { dashboardMock } from './mock'
import type { DashboardSnapshot } from './types'
import type { AuthResponse } from './types'
import type { PublishedStrategy, StrategyPerformance } from './types'
import type { AdminStrategy, AdminStrategyInput, StrategyDefinition } from './types'

const useMock = import.meta.env.VITE_USE_MOCK === 'true'

export async function getDashboard(signal?: AbortSignal): Promise<DashboardSnapshot> {
  if (useMock) {
    await new Promise((resolve) => window.setTimeout(resolve, 250))
    return dashboardMock
  }

  const response = await fetch('/api/v1/dashboard', { signal, credentials: 'include' })
  if (!response.ok) {
    throw new Error(`管理 API 请求失败 (${response.status})`)
  }
  return response.json() as Promise<DashboardSnapshot>
}

export class APIError extends Error {
  constructor(public status: number, message: string) { super(message) }
}

async function apiRequest<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  if (init.body) headers.set('Content-Type', 'application/json')
  const csrfCookieName = import.meta.env.VITE_CSRF_COOKIE_NAME ?? 'alphaflow_csrf'
  const csrf = document.cookie.split('; ').find((item) => item.startsWith(`${csrfCookieName}=`))?.split('=')[1]
  if (csrf) headers.set('X-CSRF-Token', decodeURIComponent(csrf))
  const response = await fetch(path, { ...init, headers, credentials: 'include' })
  if (!response.ok) {
    const body = await response.json().catch(() => null) as { error?: { message?: string } } | null
    throw new APIError(response.status, body?.error?.message ?? `请求失败 (${response.status})`)
  }
  if (response.status === 204) return undefined as T
  return response.json() as Promise<T>
}

export function login(email: string, password: string): Promise<AuthResponse> {
  return apiRequest('/api/v1/auth/login', { method: 'POST', body: JSON.stringify({ email, password }) })
}

export function getCurrentUser(signal?: AbortSignal): Promise<AuthResponse> {
  return apiRequest('/api/v1/auth/me', { signal })
}

export function logout(): Promise<void> {
  return apiRequest('/api/v1/auth/logout', { method: 'POST' })
}
export function listStrategies(signal?:AbortSignal):Promise<{strategies:PublishedStrategy[]}>{return apiRequest('/api/v1/strategies',{signal})}
export function listStrategyPerformance(id:string,signal?:AbortSignal):Promise<{performance:StrategyPerformance[]}>{return apiRequest(`/api/v1/strategies/${encodeURIComponent(id)}/performance`,{signal})}
export function listAdminStrategies(signal?:AbortSignal):Promise<{strategies:AdminStrategy[]}>{return apiRequest('/api/v1/admin/strategies',{signal})}
export function createAdminStrategy(input:AdminStrategyInput):Promise<AdminStrategy>{return apiRequest('/api/v1/admin/strategies',{method:'POST',body:JSON.stringify(input)})}
export function updateAdminStrategy(id:string,input:AdminStrategyInput):Promise<AdminStrategy>{return apiRequest(`/api/v1/admin/strategies/${encodeURIComponent(id)}`,{method:'PATCH',body:JSON.stringify(input)})}
export function listStrategyDefinitions(signal?:AbortSignal):Promise<{definitions:StrategyDefinition[]}>{return apiRequest('/api/v1/admin/strategy-definitions',{signal})}
export function createAdminStrategyVersion(id:string,version:string):Promise<AdminStrategy>{return apiRequest(`/api/v1/admin/strategies/${encodeURIComponent(id)}/versions`,{method:'POST',body:JSON.stringify({version})})}
