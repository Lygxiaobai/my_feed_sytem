import { postJson } from './client'
import type { IsLikedResponse, ListLikedVideoIDsResponse, MessageResponse } from './types'

export function like(videoId: number) {
  return postJson<MessageResponse>('/like/like', { video_id: videoId }, { authRequired: true })
}

export function unlike(videoId: number) {
  return postJson<MessageResponse>('/like/unlike', { video_id: videoId }, { authRequired: true })
}

export function isLiked(videoId: number) {
  return postJson<IsLikedResponse>('/like/isLiked', { video_id: videoId }, { authRequired: true })
}

export async function listLikedVideoIds(videoIds: number[]) {
  if (videoIds.length === 0) return []
  const res = await postJson<ListLikedVideoIDsResponse>(
    '/like/listLikedVideoIDs',
    { video_ids: videoIds },
    { authRequired: true },
  )
  return res.video_ids
}
