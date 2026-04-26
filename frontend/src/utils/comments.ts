import type { Comment, CommentReply } from '../api/types'

type PublishedComment = Comment | CommentReply

function isRootComment(comment: PublishedComment) {
  return comment.parent_comment_id === 0 && comment.root_comment_id === 0
}

function toRootComment(comment: PublishedComment): Comment {
  if ('replies' in comment && 'reply_count' in comment) {
    return {
      ...comment,
      replies: [...comment.replies],
    }
  }

  return {
    ...comment,
    reply_count: 0,
    replies: [],
  }
}

export function countComments(comments: Comment[]) {
  return comments.reduce((sum, comment) => sum + 1 + comment.replies.length, 0)
}

export function hasCommentID(comments: Comment[], commentID: number) {
  return comments.some((comment) => comment.id === commentID || comment.replies.some((reply) => reply.id === commentID))
}

export function insertPublishedComment(comments: Comment[], published: PublishedComment): Comment[] {
  if (hasCommentID(comments, published.id)) {
    return comments
  }

  if (isRootComment(published)) {
    return [...comments, toRootComment(published)]
  }

  const rootCommentID = published.root_comment_id || published.parent_comment_id
  return comments.map((comment) => {
    if (comment.id !== rootCommentID) {
      return comment
    }

    const nextReplies = [...comment.replies, { ...published }]
    return {
      ...comment,
      replies: nextReplies,
      reply_count: nextReplies.length,
    }
  })
}

export function removeCommentByID(comments: Comment[], commentID: number): Comment[] {
  const nextComments: Comment[] = []

  for (const comment of comments) {
    if (comment.id === commentID) {
      continue
    }

    const nextReplies = comment.replies.filter((reply) => reply.id !== commentID)
    if (nextReplies.length !== comment.replies.length) {
      nextComments.push({
        ...comment,
        replies: nextReplies,
        reply_count: nextReplies.length,
      })
      continue
    }

    nextComments.push(comment)
  }

  return nextComments
}
