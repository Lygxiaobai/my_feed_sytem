import { postJson, resolveAssetUrl } from './client'
import type {
  BackendFeedVideo,
  BackendListByFollowingResponse,
  BackendListByPopularityResponse,
  BackendListLatestResponse,
  BackendListLikesCountResponse,
  FeedVideoItem,
  ListByFollowingResponse,
  ListByPopularityResponse,
  ListLatestResponse,
  ListLikesCountResponse,
} from './types'

function normalizeFeedVideo(item: BackendFeedVideo): FeedVideoItem {
  return {
    id: item.id,
    author: {
      id: item.author_id,
      username: item.username,
    },
    title: item.title,
    description: item.description,
    play_url: resolveAssetUrl(item.play_url),
    cover_url: resolveAssetUrl(item.cover_url),
    create_time: Math.floor(new Date(item.created_at).getTime() / 1000),
    likes_count: item.likes_count,
    comment_count: item.comment_count ?? 0,
    is_liked: false,
    popularity: item.popularity,
  }
}

export async function listLatest(input: { limit: number; latest_time: number; last_id?: number }) {
  const res = await postJson<BackendListLatestResponse>('/feed/listLatest', {
    limit: input.limit,
    latest_time: input.latest_time,
    last_id: input.last_id ?? 0,
  })

  return {
    video_list: res.videos.map(normalizeFeedVideo),
    next_time: res.next_time,
    next_id: res.next_id,
    has_more: res.has_more,
  } satisfies ListLatestResponse
}

export async function listLikesCount(input: { limit: number; likes_count_before?: number; id_before?: number }) {
  const body: Record<string, unknown> = { limit: input.limit }
  if (typeof input.likes_count_before === 'number' || typeof input.id_before === 'number') {
    body.likes_count_before = input.likes_count_before ?? 0
    body.id_before = input.id_before ?? 0
  }

  const res = await postJson<BackendListLikesCountResponse>('/feed/listLikesCount', body)
  return {
    video_list: res.videos.map(normalizeFeedVideo),
    next_likes_count_before: res.next_likes_count_before,
    next_id_before: res.next_id_before,
    has_more: res.has_more,
  } satisfies ListLikesCountResponse
}

export async function listByPopularity(input: { limit: number; as_of: number; offset: number }) {
  const res = await postJson<BackendListByPopularityResponse>('/feed/listByPopularity', input)
  return {
    video_list: res.videos.map(normalizeFeedVideo),
    as_of: res.as_of,
    next_offset: res.next_offset,
    has_more: res.has_more,
  } satisfies ListByPopularityResponse
}

export async function listByFollowing(input: { limit: number; latest_time: number; last_id?: number }) {
  const res = await postJson<BackendListByFollowingResponse>(
    '/feed/listByFollowing',
    {
      limit: input.limit,
      latest_time: input.latest_time,
      last_id: input.last_id ?? 0,
    },
    { authRequired: true },
  )

  return {
    video_list: res.videos.map(normalizeFeedVideo),
    next_time: res.next_time,
    next_id: res.next_id,
    has_more: res.has_more,
  } satisfies ListByFollowingResponse
}
