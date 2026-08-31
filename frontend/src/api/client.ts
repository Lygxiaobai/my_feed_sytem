import { useAuthStore } from '../stores/auth'

export class ApiError extends Error {
  status: number
  /** 后端返回的五位业务错误码，例如 A0230（登录已过期）。 */
  code: string
  /** 本次请求的唯一标识，与后端日志中的 request_id 一致，用于报障时溯源。 */
  requestId: string
  payload?: unknown

  constructor(
    message: string,
    status: number,
    options?: { code?: string; requestId?: string; payload?: unknown },
  ) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = options?.code ?? ''
    this.requestId = options?.requestId ?? ''
    this.payload = options?.payload
  }
}

/** 用户主动取消导致的中断，调用方据此静默复位，不当作失败提示。 */
export class AbortedError extends Error {
  constructor() {
    super('已取消')
    this.name = 'AbortedError'
  }
}

/**
 * 后端统一响应结构。
 * code 为 "00000" 表示成功，其余为五位业务错误码；
 * message 是可直接展示给用户的提示（后端已剥离内部错误细节）；
 * data 是真正的业务数据；requestId 用于把前端报错对应到后端日志。
 */
type Envelope = {
  code?: string
  message?: string
  data?: unknown
  requestId?: string
}

/** 成功错误码，与后端 response.Success 保持一致。 */
const SUCCESS_CODE = '00000'

const API_BASE = (import.meta.env.VITE_API_BASE as string | undefined) ?? '/api'

function getDefaultErrorMessage(status: number) {
  return `请求失败 (${status})`
}

function getMissingTokenMessage() {
  return '请先登录'
}

/**
 * 统一拆信封。
 * 放在这一层处理，是为了让上层 api 模块与 types.ts 保持原样——
 * data 的内部形状（{video}、{videos}、{comments} 等）没有变化。
 */
function unwrap<T>(raw: unknown, status: number): T {
  const envelope = raw && typeof raw === 'object' ? (raw as Envelope) : undefined

  // 网关或反向代理返回的非 JSON 错误页不带 code，按 HTTP 状态处理。
  if (!envelope || typeof envelope.code !== 'string') {
    if (status < 200 || status >= 300) {
      throw new ApiError(getDefaultErrorMessage(status), status)
    }
    return raw as T
  }

  if (envelope.code !== SUCCESS_CODE) {
    throw new ApiError(envelope.message || getDefaultErrorMessage(status), status, {
      code: envelope.code,
      requestId: envelope.requestId,
      payload: envelope.data,
    })
  }

  return envelope.data as T
}

function apiOrigin() {
  return new URL(API_BASE, window.location.origin).origin
}

export function resolveAssetUrl(url?: string) {
  if (!url) return ''
  if (/^https?:\/\//i.test(url)) return url
  return new URL(url, apiOrigin()).toString()
}

export async function getJson<T>(path: string, options?: { authRequired?: boolean }): Promise<T> {
  const auth = useAuthStore()
  const token = auth.token

  if (options?.authRequired && !token) {
    throw new ApiError(getMissingTokenMessage(), 401)
  }

  const headers: Record<string, string> = {}
  if (token) headers.Authorization = `Bearer ${token}`

  const res = await fetch(`${API_BASE}${path}`, { method: 'GET', headers })
  const data = parseResponseBody(await res.text())
  if (res.status === 401) {
    auth.clearToken()
  }
  return unwrap<T>(data, res.status)
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

  const data = parseResponseBody(await res.text())

  // 401 无论出现在信封内还是网关层，都要清掉本地 token。
  if (res.status === 401) {
    auth.clearToken()
  }

  return unwrap<T>(data, res.status)
}

/** 关页或切后台时尽量把最后一次进度送出去，失败静默。 */
export function postJsonKeepalive(path: string, body: unknown) {
  const auth = useAuthStore()
  const token = auth.token
  if (!token) return

  void fetch(`${API_BASE}${path}`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${token}`,
    },
    body: JSON.stringify(body ?? {}),
    keepalive: true,
  }).catch(() => {
    // 页面正在卸载，失败只能等下次心跳或本地缓存。
  })
}

function parseResponseBody(text: string) {
  if (!text) return null
  try {
    return JSON.parse(text) as unknown
  } catch {
    return text as unknown
  }
}

export type FormUploadStage = 'sending' | 'confirming' | 'done'

export type FormUploadProgress = {
  percent: number
  loaded: number
  total: number
  stage: FormUploadStage
}

/**
 * 浏览器把字节交给网卡，并不等于服务端已经收完、落盘并回了响应。
 * 发送阶段最多报到这个值，100% 留给 xhr.onload。
 */
export const UPLOAD_SEND_PERCENT_CAP = 95

export function mapUploadSendPercent(loaded: number, total: number): number {
  if (total <= 0 || loaded <= 0) return 0
  return Math.min(UPLOAD_SEND_PERCENT_CAP, Math.round((loaded / total) * UPLOAD_SEND_PERCENT_CAP))
}

/**
 * 带上传进度的表单提交。
 * 这里用 XMLHttpRequest 而不是 fetch，是因为 fetch 无法上报请求体的发送进度，
 * 而大体积视频上传必须给用户真实的百分比反馈。
 */
export function postFormWithProgress<T>(
  path: string,
  body: FormData,
  options?: {
    authRequired?: boolean
    headers?: Record<string, string>
    baseUrl?: string
    onProgress?: (progress: FormUploadProgress) => void
    signal?: AbortSignal
  },
): Promise<T> {
  const auth = useAuthStore()
  const token = auth.token

  if (options?.authRequired && !token) {
    return Promise.reject(new ApiError(getMissingTokenMessage(), 401))
  }
  if (options?.signal?.aborted) {
    return Promise.reject(new AbortedError())
  }

  return new Promise<T>((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    const baseUrl = options?.baseUrl?.replace(/\/$/, '') || API_BASE
    xhr.open('POST', `${baseUrl}${path}`)
    if (token) xhr.setRequestHeader('Authorization', `Bearer ${token}`)
    if (options?.headers) {
      for (const [name, value] of Object.entries(options.headers)) {
        if (value) xhr.setRequestHeader(name, value)
      }
    }

    const onProgress = options?.onProgress
    let loaded = 0
    let total = 0
    let stalled = false
    let lastActive = Date.now()
    // 浏览器发完之后还要等 Cloudflare 回源。这段没有 onprogress，不能再用 90 秒发送停顿去杀请求。
    let awaitingAck = false
    const emitProgress = (stage: FormUploadStage, percent: number) => {
      onProgress?.({ percent, loaded, total, stage })
    }
    const touch = () => {
      lastActive = Date.now()
    }

    xhr.upload.onprogress = (event) => {
      touch()
      if (!onProgress || !event.lengthComputable || event.total <= 0) return
      loaded = event.loaded
      total = event.total
      emitProgress('sending', mapUploadSendPercent(loaded, total))
    }
    // 浏览器发完请求体之后，还要等网关转发和业务落盘。这时绝不能报 100%。
    xhr.upload.onload = () => {
      touch()
      awaitingAck = true
      if (onProgress) emitProgress('confirming', total > 0 ? UPLOAD_SEND_PERCENT_CAP : 90)
    }

    const signal = options?.signal
    const abort = () => xhr.abort()
    signal?.addEventListener('abort', abort)
    // 发送阶段 90 秒完全没进度才中止；确认阶段放到 Cloudflare 回源窗口之后。
    const stallTimer = window.setInterval(() => {
      const limit = awaitingAck ? 120_000 : 90_000
      if (Date.now() - lastActive < limit) return
      stalled = true
      xhr.abort()
    }, 5_000)
    const cleanup = () => {
      window.clearInterval(stallTimer)
      signal?.removeEventListener('abort', abort)
    }

    xhr.onload = () => {
      cleanup()
      emitProgress('done', 100)
      if (!xhr.responseText && xhr.status >= 400) {
        reject(new ApiError('上传中断，请重试', xhr.status))
        return
      }
      const data = parseResponseBody(xhr.responseText)
      if (xhr.status === 401) auth.clearToken()
      try {
        resolve(unwrap<T>(data, xhr.status))
      } catch (error) {
        reject(error)
      }
    }

    xhr.onerror = () => {
      cleanup()
      reject(new ApiError('网络异常，请检查连接后重试', 0))
    }

    xhr.ontimeout = () => {
      cleanup()
      reject(new ApiError('上传超时，请重试', 0))
    }

    xhr.onabort = () => {
      cleanup()
      if (stalled) {
        reject(new ApiError('上传超时，请检查网络后重试', 0))
        return
      }
      reject(new AbortedError())
    }

    xhr.send(body)
  })
}

/**
 * 把一个分片直接 PUT 到对象存储的预签名 URL，返回该分片的 ETag。
 *
 * 与 postFormWithProgress 的三点关键差别：
 *   1. 不带 Authorization 头。预签名 URL 自身即凭据，多一个头会让 SigV4 校验失败。
 *   2. body 是裸 Blob，不包 FormData。签名覆盖的是整个请求体，multipart 边界会破坏它。
 *   3. 响应不是应用的 JSON 信封，而是 S3 的空响应；要的是 ETag 响应头。
 *      读得到 ETag 依赖对象存储回 Access-Control-Expose-Headers，Silo 默认已包含。
 */
export function putBinaryWithProgress(
  url: string,
  body: Blob,
  options?: {
    onProgress?: (progress: FormUploadProgress) => void
    signal?: AbortSignal
  },
): Promise<string> {
  if (options?.signal?.aborted) {
    return Promise.reject(new AbortedError())
  }

  return new Promise<string>((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open('PUT', url)

    const onProgress = options?.onProgress
    let loaded = 0
    let total = body.size
    let stalled = false
    let lastActive = Date.now()
    let awaitingAck = false
    const emitProgress = (stage: FormUploadStage, percent: number) => {
      onProgress?.({ percent, loaded, total, stage })
    }

    xhr.upload.onprogress = (event) => {
      lastActive = Date.now()
      if (!onProgress || !event.lengthComputable || event.total <= 0) return
      loaded = event.loaded
      total = event.total
      emitProgress('sending', mapUploadSendPercent(loaded, total))
    }
    xhr.upload.onload = () => {
      lastActive = Date.now()
      awaitingAck = true
      if (onProgress) emitProgress('confirming', total > 0 ? UPLOAD_SEND_PERCENT_CAP : 90)
    }

    const signal = options?.signal
    const abort = () => xhr.abort()
    signal?.addEventListener('abort', abort)
    const stallTimer = window.setInterval(() => {
      const limit = awaitingAck ? 120_000 : 90_000
      if (Date.now() - lastActive < limit) return
      stalled = true
      xhr.abort()
    }, 5_000)
    const cleanup = () => {
      window.clearInterval(stallTimer)
      signal?.removeEventListener('abort', abort)
    }

    xhr.onload = () => {
      cleanup()
      if (xhr.status < 200 || xhr.status >= 300) {
        reject(new ApiError('分片上传失败，请重试', xhr.status))
        return
      }
      const etag = xhr.getResponseHeader('ETag') || xhr.getResponseHeader('etag')
      if (!etag) {
        // 拿不到 ETag 就无法拼装。多半是跨域没暴露该响应头，属于部署问题而非用户操作问题。
        reject(new ApiError('上传校验失败，请重试', 0))
        return
      }
      emitProgress('done', 100)
      resolve(etag)
    }

    xhr.onerror = () => {
      cleanup()
      reject(new ApiError('网络异常，请检查连接后重试', 0))
    }

    xhr.ontimeout = () => {
      cleanup()
      reject(new ApiError('上传超时，请重试', 0))
    }

    xhr.onabort = () => {
      cleanup()
      if (stalled) {
        reject(new ApiError('上传超时，请检查网络后重试', 0))
        return
      }
      reject(new AbortedError())
    }

    xhr.send(body)
  })
}
