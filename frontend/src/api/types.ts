export type TokenResponse = { token: string; created?: boolean }

export type PasskeyBeginResponse = {
  session_id: string
  options: unknown
}

export type PasskeyItem = {
  id: number
  name: string
  created_at: string
  last_used_at?: string | null
}

export type PasskeyListResponse = {
  items: PasskeyItem[]
}

export type Account = {
  id: number
  username: string
}

/** 内容审核状态，与后端 audit.Status 一致。 */
export type AuditStatus = 'pending' | 'reviewing' | 'approved' | 'rejected'

/** 作者侧生命周期，与后端 video.Lifecycle 一致；和审核状态正交。 */
export type VideoLifecycle = 'draft' | 'published' | 'unpublished'

export type Video = {
  id: number
  author_id: number
  username: string
  title: string
  description?: string
  tags?: string[]
  play_url: string
  cover_url: string
  created_at: string
  likes_count: number
  comment_count: number
  /** 仅作者本人查看自己的作品时才会看到非 approved 的值。 */
  audit_status?: AuditStatus
  lifecycle?: VideoLifecycle
  deleted_at?: string | null
}

export type CommentReply = {
  id: number
  username: string
  video_id: number
  author_id: number
  root_comment_id: number
  parent_comment_id: number
  reply_to_user_id: number
  reply_to_username: string
  content: string
  created_at: string
  updated_at?: string
}

export type Comment = {
  id: number
  username: string
  video_id: number
  author_id: number
  root_comment_id: number
  parent_comment_id: number
  reply_to_user_id: number
  reply_to_username: string
  content: string
  created_at: string
  updated_at?: string
  reply_count: number
  replies: CommentReply[]
}

export type SocialRelation = {
  id: number
  follower_id: number
  vlogger_id: number
  created_at: string
  follower_username?: string
  vlogger_username?: string
}

export type FeedAuthor = {
  id: number
  username: string
}

export type FeedVideoItem = {
  id: number
  author: FeedAuthor
  title: string
  description?: string
  tags?: string[]
  play_url: string
  cover_url: string
  create_time: number
  likes_count: number
  comment_count: number
  is_liked: boolean
  popularity?: number
}

export type BackendAccountEnvelope = {
  account: Account
}

export type BackendVideoEnvelope = {
  video: Video
}

export type BackendVideosEnvelope = {
  videos: Video[]
}

export type BackendCommentListEnvelope = {
  comments: Comment[]
}

export type CommentPublishResponse = {
  comment: CommentReply | Comment
}

export type DanmakuItem = {
  id: number
  video_id: number
  author_id: number
  username: string
  content: string
  offset_ms: number
  created_at: string
}

export type DanmakuListEnvelope = {
  items: DanmakuItem[]
}

export type DanmakuSendResponse = {
  item: DanmakuItem
}

export type HistoryStatus = 'unfinished' | 'completed'

export type WatchProgress = {
  video_id: number
  position_ms: number
  duration_ms: number
  completed: boolean
  resume_ms: number
}

export type HistoryItem = WatchProgress & {
  last_watched_at: string
  video: Video
}

export type HistoryListResponse = {
  items: HistoryItem[]
  next_cursor: string
  has_more: boolean
}

export type HistoryUpsertResponse = {
  saved: boolean
  item?: HistoryItem | null
}

export type GetAllFollowersResponse = {
  followers: SocialRelation[]
}

export type GetAllVloggersResponse = {
  vloggers: SocialRelation[]
}

export type IsLikedResponse = {
  is_liked: boolean
}

export type ListLikedVideoIDsResponse = {
  video_ids: number[]
}

export type BackendFeedVideo = {
  id: number
  author_id: number
  username: string
  title: string
  description?: string
  tags?: string[]
  play_url: string
  cover_url: string
  likes_count: number
  comment_count: number
  popularity?: number
  created_at: string
  updated_at?: string
}

export type ListLatestResponse = {
  video_list: FeedVideoItem[]
  next_time: number
  next_id: number
  has_more: boolean
}

export type ListLikesCountResponse = {
  video_list: FeedVideoItem[]
  next_likes_count_before?: number
  next_id_before?: number
  has_more: boolean
}

export type ListByPopularityResponse = {
  video_list: FeedVideoItem[]
  as_of: number
  next_offset: number
  has_more: boolean
}

export type ListByFollowingResponse = {
  video_list: FeedVideoItem[]
  next_time: number
  next_id: number
  has_more: boolean
}

export type ListRecommendResponse = {
  video_list: FeedVideoItem[]
  exclude_ids: number[]
  has_more: boolean
}

export type BackendListLatestResponse = {
  videos: BackendFeedVideo[]
  next_time: number
  next_id: number
  has_more: boolean
}

export type BackendListLikesCountResponse = {
  videos: BackendFeedVideo[]
  next_likes_count_before?: number
  next_id_before?: number
  has_more: boolean
}

export type BackendListByPopularityResponse = {
  videos: BackendFeedVideo[]
  as_of: number
  next_offset: number
  has_more: boolean
}

export type BackendListByFollowingResponse = {
  videos: BackendFeedVideo[]
  next_time: number
  next_id: number
  has_more: boolean
}

export type BackendListRecommendResponse = {
  videos: BackendFeedVideo[]
  exclude_ids: number[]
  has_more: boolean
}
