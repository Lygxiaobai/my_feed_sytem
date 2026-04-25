import { postForm, postJson, resolveAssetUrl } from './client'
import type { BackendVideoEnvelope, BackendVideosEnvelope, Video } from './types'

function normalizeVideo(video: Video): Video {
  return {
    ...video,
    play_url: resolveAssetUrl(video.play_url),
    cover_url: resolveAssetUrl(video.cover_url),
    comment_count: video.comment_count ?? 0,
  }
}

export async function publishVideo(
  input: { title: string; description: string; play_url: string; cover_url: string },
  options?: { idempotencyKey?: string },
) {
  const res = await postJson<BackendVideoEnvelope>('/video/publish', input, {
    authRequired: true,
    headers: options?.idempotencyKey ? { 'Idempotency-Key': options.idempotencyKey } : undefined,
  })
  return normalizeVideo(res.video)
}

export type UploadResponse = { url: string; play_url?: string; cover_url?: string }

export type UploadedAsset = UploadResponse & {
  asset_url: string
  play_asset_url?: string
  cover_asset_url?: string
}

function normalizeUploadResponse(res: UploadResponse): UploadedAsset {
  return {
    ...res,
    asset_url: resolveAssetUrl(res.url),
    play_asset_url: res.play_url ? resolveAssetUrl(res.play_url) : undefined,
    cover_asset_url: res.cover_url ? resolveAssetUrl(res.cover_url) : undefined,
  }
}

export async function uploadVideo(file: File) {
  const fd = new FormData()
  fd.append('file', file)
  const res = await postForm<UploadResponse>('/video/uploadVideo', fd, { authRequired: true })
  return normalizeUploadResponse(res)
}

export async function uploadCover(file: File) {
  const fd = new FormData()
  fd.append('file', file)
  const res = await postForm<UploadResponse>('/video/uploadCover', fd, { authRequired: true })
  return normalizeUploadResponse(res)
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
