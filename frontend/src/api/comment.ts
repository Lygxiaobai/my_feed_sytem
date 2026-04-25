import { postJson } from './client'
import type { BackendCommentListEnvelope, Comment, CommentPublishResponse, MessageResponse } from './types'

export async function listAll(videoId: number) {
  const res = await postJson<BackendCommentListEnvelope>('/comment/listAll', { video_id: videoId })
  return res.comments as Comment[]
}

export function publish(videoId: number, content: string, parentCommentId?: number) {
  return postJson<CommentPublishResponse>(
    '/comment/publish',
    {
      video_id: videoId,
      content,
      parent_comment_id: parentCommentId ?? 0,
    },
    { authRequired: true },
  )
}

export function remove(commentId: number) {
  return postJson<MessageResponse>('/comment/delete', { comment_id: commentId }, { authRequired: true })
}
