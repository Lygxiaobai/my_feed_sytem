import {
  AbortedError,
  ApiError,
  mapUploadSendPercent,
  postFormWithProgress,
  postJson,
  resolveAssetUrl,
  type FormUploadProgress,
} from './client'
import type { BackendVideoEnvelope, BackendVideosEnvelope, Video } from './types'

/** 与后端 media.defaultMaxVideoBytes（256 MiB）保持一致，超出前端直接拒绝。 */
export const MAX_VIDEO_BYTES = 256 * 1024 * 1024

/** 与 videos.title / description 列宽一致，输入框与提交前截断共用。 */
export const MAX_TITLE_CHARS = 128
export const MAX_DESCRIPTION_CHARS = 1000
export const MAX_VIDEO_TAGS = 7
export const MAX_TAG_CHARS = 32

/** 与后端 tag.Infer 一致：#话题 + 标题/描述里的短词。 */
const hashTagPattern = /#([^\s#]{1,32})/g
const leftoverSplitter = /[#\s,，.。;；!！?？、/|]+/
const maxPhraseChars = 16

export function inferTags(title: string, description: string): string[] {
  const out: string[] = []
  const seen = new Set<string>()
  const push = (raw: string) => {
    const label = raw.trim()
    if (!label) return
    const key = label.toLowerCase()
    if (seen.has(key)) return
    seen.add(key)
    out.push([...label].slice(0, MAX_TAG_CHARS).join(''))
  }
  hashTagPattern.lastIndex = 0
  let match: RegExpExecArray | null
  const combined = `${title} ${description}`
  while ((match = hashTagPattern.exec(combined)) !== null) {
    push(match[1] ?? '')
    if (out.length >= MAX_VIDEO_TAGS) return out
  }
  for (const part of [title, description]) {
    const stripped = part.replace(/#([^\s#]{1,32})/g, ' ')
    for (const token of stripped.split(leftoverSplitter)) {
      const label = token.trim()
      if (!label || [...label].length > maxPhraseChars) continue
      push(label)
      if (out.length >= MAX_VIDEO_TAGS) return out
    }
  }
  return out
}

export function formatFileSize(bytes: number) {
  const mb = bytes / 1024 / 1024
  if (mb >= 1) return `${mb.toFixed(1)} MB`
  return `${Math.max(1, Math.round(bytes / 1024))} KB`
}

/** 选择文件时立即校验，避免把注定被服务端拒绝的文件整份上传完才失败。 */
export function validateVideoFile(file: File): string | null {
  if (file.size <= 0) return '文件内容为空，请重新选择'
  // 部分系统给出的 type 为空，此时交由后端与 accept 属性兜底。
  if (file.type && !file.type.startsWith('video/')) return '只能上传视频文件'
  if (file.size > MAX_VIDEO_BYTES) {
    return `视频不能超过 ${formatFileSize(MAX_VIDEO_BYTES)}，当前 ${formatFileSize(file.size)}`
  }
  return null
}

/**
 * 生成发布请求的幂等键。
 * crypto.randomUUID 只在安全上下文（HTTPS / localhost）下存在，本站以 HTTP 提供服务，
 * 直接调用会抛 TypeError 导致发布失败，因此必须降级到 getRandomValues 手工拼 v4 UUID。
 */
export function createIdempotencyKey() {
  const cryptoApi = globalThis.crypto as Crypto | undefined

  if (typeof cryptoApi?.randomUUID === 'function') {
    return cryptoApi.randomUUID()
  }

  if (typeof cryptoApi?.getRandomValues === 'function') {
    const bytes = new Uint8Array(16)
    cryptoApi.getRandomValues(bytes)
    // 按 RFC 4122 置版本号 4 与变体位。
    bytes[6] = ((bytes[6] ?? 0) & 0x0f) | 0x40
    bytes[8] = ((bytes[8] ?? 0) & 0x3f) | 0x80
    const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
    return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
  }

  return `${Date.now().toString(16)}-${Math.random().toString(16).slice(2, 10)}`
}

function normalizeVideo(video: Video): Video {
  return {
    ...video,
    play_url: resolveAssetUrl(video.play_url),
    cover_url: resolveAssetUrl(video.cover_url),
    comment_count: video.comment_count ?? 0,
  }
}

export async function publishVideo(
  input: {
    title: string
    description: string
    tags?: string[]
    play_url: string
    cover_url: string
    draft_id?: number
  },
  options?: { idempotencyKey?: string },
) {
  const res = await postJson<BackendVideoEnvelope>('/video/publish', input, {
    authRequired: true,
    headers: options?.idempotencyKey ? { 'Idempotency-Key': options.idempotencyKey } : undefined,
  })
  return normalizeVideo(res.video)
}

export async function saveDraft(input: {
  id?: number
  title: string
  description: string
  tags?: string[]
  play_url: string
  cover_url: string
}) {
  const res = await postJson<BackendVideoEnvelope>('/video/saveDraft', input, { authRequired: true })
  return normalizeVideo(res.video)
}

export async function listDrafts() {
  const res = await postJson<BackendVideosEnvelope>('/video/listDrafts', {}, { authRequired: true })
  return res.videos.map(normalizeVideo)
}

export async function unpublishVideo(id: number) {
  const res = await postJson<BackendVideoEnvelope>('/video/unpublish', { id }, { authRequired: true })
  return normalizeVideo(res.video)
}

export async function relistVideo(id: number) {
  const res = await postJson<BackendVideoEnvelope>('/video/relist', { id }, { authRequired: true })
  return normalizeVideo(res.video)
}

export async function deleteVideo(id: number) {
  await postJson<{ deleted: boolean }>('/video/delete', { id }, { authRequired: true })
}

export function videoDisplayTitle(video: Pick<Video, 'title'>) {
  const title = video.title?.trim()
  return title || '未命名'
}

export function isDraft(video: Pick<Video, 'lifecycle'>) {
  return video.lifecycle === 'draft'
}

export function isUnpublished(video: Pick<Video, 'lifecycle'>) {
  return video.lifecycle === 'unpublished'
}

export function authorManageKind(video: Pick<Video, 'lifecycle'>): AuthorManageKind {
  if (isDraft(video)) return 'draft'
  if (isUnpublished(video)) return 'unpublished'
  return 'published'
}

export type AuthorManageKind = 'draft' | 'unpublished' | 'published'

export type MediaTaskStatus = 'processing' | 'ready' | 'failed'

export type VideoUploadTask = {
  id: number
  status: MediaTaskStatus
  play_url?: string
  cover_url?: string
  content_type: string
  error_message?: string
  created_at: string
  updated_at: string
}

type UploadPartResponse = {
  session_id: string
  received: number
  task?: VideoUploadTask
}

/** 与后端 media.UploadPartBytes 一致。单段必须能在 Cloudflare 约 100 秒回源窗口内走完。 */
export const UPLOAD_PART_BYTES = 1024 * 1024
export const UPLOAD_PART_CONCURRENCY = 2

function userFacingMediaError(message?: string) {
  const text = message?.trim() || ''
  if (!text || /transcode|ffmpeg|signal:|libx264|killed/i.test(text)) {
    return '视频处理失败，请重新上传'
  }
  return text
}

function normalizeUploadTask(task: VideoUploadTask): VideoUploadTask {
  return {
    ...task,
    play_url: task.play_url ? resolveAssetUrl(task.play_url) : undefined,
    cover_url: task.cover_url ? resolveAssetUrl(task.cover_url) : undefined,
    error_message: task.error_message ? userFacingMediaError(task.error_message) : undefined,
  }
}

export async function uploadVideo(
  file: File,
  options?: { onProgress?: (progress: FormUploadProgress) => void; signal?: AbortSignal },
) {
  return uploadVideoByParts(file, options)
}

async function uploadVideoByParts(
  file: File,
  options?: { onProgress?: (progress: FormUploadProgress) => void; signal?: AbortSignal },
) {
  const init = await postJson<{
    session_id: string
    part_bytes: number
    part_concurrency: number
    part_count: number
    part_origin?: string
  }>('/video/uploadInit', { total: file.size }, { authRequired: true })
  const partSize = init.part_bytes > 0 ? init.part_bytes : UPLOAD_PART_BYTES
  const sessionId = init.session_id
  const count = init.part_count > 0 ? init.part_count : Math.ceil(file.size / partSize)
  const concurrency = Math.max(
    1,
    Math.min(init.part_concurrency || UPLOAD_PART_CONCURRENCY, count),
  )
  if (!sessionId || count <= 0) throw new Error('上传未开始，请重试')

  const loadedParts = new Array<number>(count).fill(0)
  const emit = () => {
    if (!options?.onProgress) return
    const loaded = loadedParts.reduce((sum, n) => sum + n, 0)
    options.onProgress({
      percent: mapUploadSendPercent(loaded, file.size),
      loaded,
      total: file.size,
      stage: loaded >= file.size ? 'confirming' : 'sending',
    })
  }

  const putPart = async (index: number) => {
    const offset = index * partSize
    const end = Math.min(offset + partSize, file.size)
    const fd = new FormData()
    fd.append('file', file.slice(offset, end), file.name)
    return postFormWithProgress<UploadPartResponse>('/video/uploadPart', fd, {
      authRequired: true,
      baseUrl: init.part_origin ? `${init.part_origin.replace(/\/$/, '')}/api` : undefined,
      headers: {
        'X-Upload-Session': sessionId,
        'X-Upload-Index': String(index),
        'X-Upload-Count': String(count),
      },
      signal: options?.signal,
      onProgress: (progress) => {
        if (progress.stage === 'done') return
        loadedParts[index] = progress.stage === 'confirming' ? end - offset : progress.loaded
        emit()
      },
    })
  }

  const runPart = async (index: number) => {
    try {
      return await putPart(index)
    } catch (error) {
      if (error instanceof AbortedError) throw error
      // 超时或空 4xx 立刻重试会再开一套连接，把 Cloudflare 窗口挤得更死。
      if (error instanceof ApiError) throw error
      return await putPart(index)
    }
  }

  let cursor = 0
  let task: VideoUploadTask | undefined
  const workers = Array.from({ length: concurrency }, async () => {
    while (true) {
      const index = cursor
      cursor += 1
      if (index >= count) return
      const res = await runPart(index)
      if (res.task) task = res.task
    }
  })
  await Promise.all(workers)
  if (!task) throw new Error('上传未完成，请重试')
  return normalizeUploadTask(task)
}

export function getVideoUploadTask(taskId: number) {
  return postJson<{ task: VideoUploadTask }>(
    '/video/mediaTaskStatus',
    { task_id: taskId },
    { authRequired: true },
  ).then((res) => normalizeUploadTask(res.task))
}

/** 可被 signal 中断的等待，取消时立刻结束轮询而不是空转到下一次 tick。 */
function waitFor(ms: number, signal?: AbortSignal) {
  return new Promise<void>((resolve, reject) => {
    if (signal?.aborted) {
      reject(new AbortedError())
      return
    }
    let timer = 0
    const onAbort = () => {
      window.clearTimeout(timer)
      reject(new AbortedError())
    }
    timer = window.setTimeout(() => {
      signal?.removeEventListener('abort', onAbort)
      resolve()
    }, ms)
    signal?.addEventListener('abort', onAbort, { once: true })
  })
}

export async function waitForVideoUpload(
  taskId: number,
  options?: { intervalMs?: number; maxAttempts?: number; signal?: AbortSignal },
) {
  const intervalMs = options?.intervalMs ?? 1000
  const maxAttempts = options?.maxAttempts ?? 180
  const signal = options?.signal

  for (let attempt = 0; attempt < maxAttempts; attempt += 1) {
    if (signal?.aborted) throw new AbortedError()

    const task = await getVideoUploadTask(taskId)
    if (task.status === 'ready' && task.play_url && task.cover_url) return task
    if (task.status === 'failed') {
      throw new Error(userFacingMediaError(task.error_message))
    }
    await waitFor(intervalMs, signal)
  }

  throw new Error('视频处理超时，请稍后重试')
}

export async function listByAuthorId(authorId: number) {
  const res = await postJson<BackendVideosEnvelope>('/video/listByAuthorID', { author_id: authorId })
  return res.videos.map(normalizeVideo)
}

export async function listLiked() {
  const res = await postJson<BackendVideosEnvelope>('/video/listLiked', {}, { authRequired: true })
  return res.videos.map(normalizeVideo)
}

export async function getDetail(id: number) {
  const res = await postJson<BackendVideoEnvelope>('/video/getDetail', { id })
  return normalizeVideo(res.video)
}

export type ShareInfo = {
  video_id: number
  code: string
  title: string
  username: string
  cover_url: string
}

export async function getShareInfo(id: number) {
  const res = await postJson<{ share: ShareInfo }>('/video/share', { id })
  return res.share
}

/**
 * 用口令拼出可分享的整段文案。
 *
 * 在前端拼而不是让后端返回整串，是因为链接前缀必须跟随用户当前入口：
 * 站点同时有 HTTPS 域名和明文 IP 两个入口，location.origin 天然正确，
 * 后端要做到这点就得去信任 X-Forwarded-* 请求头。
 */
export function buildShareText(share: ShareInfo) {
  const url = `${location.origin}/s/${share.code}`
  return `${share.code}:/ 复制打开本站，看看【${share.username}】的作品\n${share.title}\n${url}`
}

export function buildShareUrl(share: ShareInfo) {
  return `${location.origin}/s/${share.code}`
}

/** 把任意粘贴文本交给后端识别。解析规则由服务端独占，前端不重复实现。 */
export async function resolveShare(text: string) {
  const res = await postJson<BackendVideoEnvelope>('/video/resolveShare', { text })
  return normalizeVideo(res.video)
}

/**
 * 判断一段文本该不该按分享口令处理。
 *
 * 分成两档，因为解析失败后的处理方式不同：
 * - 'certain'：带 `口令:/` 标记或 `/s/xxx` 链接，用户意图明确。解析失败要报错，
 *   拿整段分享文案去做全文搜索毫无意义。
 * - 'maybe'：整个输入恰好是 8 位字母数字。可能是口令，也可能就是个搜索词
 *   （"baseball" 也是 8 位），解析失败时要静默退回搜索。
 */
export function shareTextConfidence(text: string): 'certain' | 'maybe' | 'none' {
  const trimmed = text.trim()
  if (!trimmed) return 'none'
  if (/[0-9A-Za-z]{8}:\//.test(trimmed) || /\/s\/[0-9A-Za-z]{8}/.test(trimmed)) return 'certain'
  if (/^[0-9A-Za-z]{8}$/.test(trimmed)) return 'maybe'
  return 'none'
}

const SHARE_SEEN_KEY = 'feed.share.seen'
const SHARE_SKIP_KEY = 'feed.share.skip'

function shareTextKey(text: string) {
  return text.trim().slice(0, 512)
}

/** 自己刚复制的口令不要立刻当成「别人分享来的」弹识别卡片。 */
export function rememberCopiedShare(text: string) {
  const key = shareTextKey(text)
  if (!key) return
  try {
    sessionStorage.setItem(SHARE_SKIP_KEY, key)
    sessionStorage.setItem(SHARE_SEEN_KEY, key)
  } catch {
    // 隐私模式写不了也没关系，最多自己弹出一次识别卡片。
  }
}

export function clipboardShareAlreadyHandled(text: string) {
  const key = shareTextKey(text)
  try {
    return sessionStorage.getItem(SHARE_SKIP_KEY) === key || sessionStorage.getItem(SHARE_SEEN_KEY) === key
  } catch {
    return false
  }
}

export function rememberHandledShare(text: string) {
  const key = shareTextKey(text)
  if (!key) return
  try {
    sessionStorage.setItem(SHARE_SEEN_KEY, key)
  } catch {
    // 忽略：只影响本会话是否重复提示。
  }
}
