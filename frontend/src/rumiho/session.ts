import { activateInstance, deactivateInstance } from '@/store/setup'
import { ref } from 'vue'

export interface InstanceSettings {
  subscriptionURL: string
  mode: 'rule' | 'global' | 'direct'
  updateHours: number
  publicURL: string
}
export interface InstanceState {
  username: string
  settings: InstanceSettings
  nodeCount: number
  subscriptionGroupCount: number
  usesSubscriptionRules: boolean
  lastUpdated: string
  lastError: string
  coreOnline: boolean
  version?: { version: string }
  access: {
    publicURL: string
    proxyURL: string
  }
}

// Authentication and proxy credentials live in memory, never in browser storage.
export const instance = ref<InstanceState | null>(null)
let pendingSession: Promise<boolean> | undefined

export function expireSession() {
  instance.value = null
  deactivateInstance()
  if (location.hash !== '#/login') location.replace('/#/login')
}

export async function request<T>(
  path: string,
  method = 'GET',
  body?: unknown,
  redirect = true,
): Promise<T> {
  const response = await fetch(`/api${path}`, {
    method,
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-Rumiho-Request': '1' },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: AbortSignal.timeout(90000),
    cache: 'no-store',
  })
  if (response.status === 401 && redirect) expireSession()
  const data = await response.json()
  if (!response.ok) throw new Error(data.error || '请求失败，请稍后重试')
  return data as T
}

export async function refreshInstance() {
  const state = await request<InstanceState>('/state')
  instance.value = state
  return state
}

export function ensureSession(): Promise<boolean> {
  if (instance.value) return Promise.resolve(true)
  pendingSession ??= request<InstanceState>('/state', 'GET', undefined, false)
    .then((state) => {
      instance.value = state
      activateInstance()
      return true
    })
    .catch(() => false)
    .finally(() => {
      pendingSession = undefined
    })
  return pendingSession
}

export async function login(username: string, password: string) {
  await request('/login', 'POST', { username, password }, false)
  const state = await refreshInstance()
  activateInstance()
  return state
}

export async function logout() {
  await request('/logout', 'POST')
  expireSession()
}
