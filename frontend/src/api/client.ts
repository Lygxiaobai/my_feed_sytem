import { useAuthStore } from '../stores/auth'

export class ApiError extends Error {
  status: number
  payload?: unknown

  constructor(message: string, status: number, payload?: unknown) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.payload = payload
  }
}

type ApiErrorBody = { error?: string; message?: string }

const API_BASE = (import.meta.env.VITE_API_BASE as string | undefined) ?? '/api'

function getDefaultErrorMessage(status: number) {
  return `请求失败 (${status})`
}

function getMissingTokenMessage() {
  return '需要先登录（缺少 token）'
}

function parseResponseMessage(data: unknown, status: number) {
  const body = data && typeof data === 'object' ? (data as ApiErrorBody) : undefined
  return body?.error ? String(body.error) : body?.message ? String(body.message) : getDefaultErrorMessage(status)
}

function apiOrigin() {
  return new URL(API_BASE, window.location.origin).origin
}

export function resolveAssetUrl(url?: string) {
  if (!url) return ''
  if (/^https?:\/\//i.test(url)) return url
  return new URL(url, apiOrigin()).toString()
}

export async function postJson<T>(
  path: string,
  body: unknown,
  options?: { authRequired?: boolean; headers?: Record<string, string> },
): Promise<T> {
  const auth = useAuthStore()
  const token = auth.token

  if (options?.authRequired && !token) {
    throw new ApiError(getMissingTokenMessage(), 401)
  }

  const headers: Record<string, string> = { 'Content-Type': 'application/json', ...(options?.headers ?? {}) }
  if (token) headers.Authorization = `Bearer ${token}`

  const res = await fetch(`${API_BASE}${path}`, {
    method: 'POST',
    headers,
    body: JSON.stringify(body ?? {}),
  })

  const text = await res.text()
  let data: unknown = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = text
    }
  }

  if (!res.ok) {
    if (res.status === 401) {
      auth.clearToken()
    }
    throw new ApiError(parseResponseMessage(data, res.status), res.status, data)
  }

  return data as T
}

export async function postForm<T>(path: string, body: FormData, options?: { authRequired?: boolean }): Promise<T> {
  const auth = useAuthStore()
  const token = auth.token

  if (options?.authRequired && !token) {
    throw new ApiError(getMissingTokenMessage(), 401)
  }

  const headers: Record<string, string> = {}
  if (token) headers.Authorization = `Bearer ${token}`

  const res = await fetch(`${API_BASE}${path}`, {
    method: 'POST',
    headers,
    body,
  })

  const text = await res.text()
  let data: unknown = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = text
    }
  }

  if (!res.ok) {
    if (res.status === 401) {
      auth.clearToken()
    }
    throw new ApiError(parseResponseMessage(data, res.status), res.status, data)
  }

  return data as T
}
