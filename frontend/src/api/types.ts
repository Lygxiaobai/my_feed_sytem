export type MessageResponse = { message: string }

export type TokenResponse = { token: string }

export type Account = {
  id: number
  username: string
}

export type Video = {
  id: number
  author_id: number
  username: string
  title: string
  description?: string
  play_url: string
  cover_url: string
  created_at: string
  likes_count: number
  comment_count: number
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
  play_url: string
  cover_url: string
  create_time: number
  likes_count: number
  comment_count: number
  is_liked: boolean
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

export type CommentPublishResponse = MessageResponse & {
  comment: CommentReply | Comment
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
