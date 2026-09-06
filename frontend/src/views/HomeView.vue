<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { track } from '../analytics/track'
import { createWatchSession } from '../analytics/watch'
import { bindWatchProgress, flushWatchProgress, noteWatchProgress, unbindWatchProgress } from '../history/progress'
import AppIcon from '../components/AppIcon.vue'
import AppShell from '../components/AppShell.vue'
import CommentAuthor from '../components/CommentAuthor.vue'
import CommentListSkeleton from '../components/CommentListSkeleton.vue'
import DanmakuLayer from '../components/DanmakuLayer.vue'
import FeedStageSkeleton from '../components/FeedStageSkeleton.vue'
import ReportSheet from '../components/ReportSheet.vue'
import ShareSheet from '../components/ShareSheet.vue'
import TipSheet from '../components/TipSheet.vue'
import UserAvatar from '../components/UserAvatar.vue'
import VideoPlayer, { type VideoPlayerHandle } from '../components/VideoPlayer.vue'
import { ApiError } from '../api/client'
import * as commentApi from '../api/comment'
import * as feedApi from '../api/feed'
import * as likeApi from '../api/like'
import type { Comment, CommentReply, FeedVideoItem } from '../api/types'
import { useAuthStore } from '../stores/auth'
import { useSocialStore } from '../stores/social'
import { useToastStore } from '../stores/toast'
import { countComments, hasCommentID, insertPublishedComment, removeCommentByID } from '../utils/comments'

type TabKey = 'recommend' | 'hot' | 'following'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const social = useSocialStore()
const toast = useToastStore()

const tab = ref<TabKey>('recommend')
const scroller = ref<HTMLDivElement | null>(null)

const q = computed(() => (typeof route.query.q === 'string' ? route.query.q.trim().toLowerCase() : ''))
const canInteractWithLike = computed(() => auth.isLoggedIn)
const canInteractWithFollow = computed(() => auth.isLoggedIn)

const recommend = reactive({
  items: [] as FeedVideoItem[],
  loading: false,
  error: '',
  hasMore: false,
  excludeIds: [] as number[],
})

const hot = reactive({
  items: [] as FeedVideoItem[],
  loading: false,
  error: '',
  hasMore: false,
  nextLikesCountBefore: undefined as number | undefined,
  nextIdBefore: undefined as number | undefined,
})

const following = reactive({
  items: [] as FeedVideoItem[],
  loading: false,
  error: '',
  hasMore: false,
  nextTime: 0,
  nextId: 0,
})

const likeBusy = reactive<Record<string, boolean>>({})
const followBusy = reactive<Record<string, boolean>>({})
const watchSession = createWatchSession()

const muted = ref(true)
const danmakuEnabled = ref(true)
const activePlayTime = ref(0)
const activePlaying = ref(false)
const activeIndex = ref(0)
const tipSheet = ref<InstanceType<typeof TipSheet> | null>(null)
const tipTarget = ref<FeedVideoItem | null>(null)
const shareSheet = ref<InstanceType<typeof ShareSheet> | null>(null)
const reportSheet = ref<InstanceType<typeof ReportSheet> | null>(null)

function openGiveTip(item: FeedVideoItem) {
  tipTarget.value = item
  void nextTick(() => tipSheet.value?.openGive())
}

function openInboxTip(item: FeedVideoItem) {
  tipTarget.value = item
  void nextTick(() => tipSheet.value?.openInbox())
}
const playerMap = new Map<number, VideoPlayerHandle>()
let slideObserver: IntersectionObserver | null = null
let tapTimer: number | undefined
let playRequestId = 0

function readMutedPreference() {
  try {
    const saved = window.localStorage.getItem('feed.muted')
    return saved === null ? true : saved === 'true'
  } catch {
    return true
  }
}

function readDanmakuPreference() {
  try {
    const saved = window.localStorage.getItem('feed.danmaku')
    return saved === null ? true : saved === 'true'
  } catch {
    return true
  }
}

const currentState = computed(() => {
  if (tab.value === 'hot') return hot
  if (tab.value === 'following') return following
  return recommend
})

function routeTab(): TabKey {
  if (route.name === 'likes') return 'hot'
  if (route.name === 'following') return 'following'
  return 'recommend'
}

async function syncLikedState(items: FeedVideoItem[]) {
  if (!items.length) return
  if (!auth.isLoggedIn) {
    items.forEach((item) => {
      item.is_liked = false
    })
    return
  }

  try {
    const likedIds = await likeApi.listLikedVideoIds(items.map((item) => item.id))
    const likedSet = new Set(likedIds)
    items.forEach((item) => {
      item.is_liked = likedSet.has(item.id)
    })
  } catch {
    items.forEach((item) => {
      item.is_liked = false
    })
  }
}

const filteredItems = computed(() => {
  const items = currentState.value.items
  if (!q.value) return items
  return items.filter((v) => {
    if (v.title.toLowerCase().includes(q.value) || v.author.username.toLowerCase().includes(q.value)) return true
    return (v.tags ?? []).some((tag) => tag.toLowerCase().includes(q.value))
  })
})

const activeItem = computed(() => filteredItems.value[activeIndex.value] ?? null)
const myAccountId = computed(() => auth.claims?.account_id ?? 0)

function setPlayerRef(id: number, el: VideoPlayerHandle | null) {
  if (el) {
    el.setMuted(muted.value)
    playerMap.set(id, el)
  } else {
    playerMap.delete(id)
  }
}

function getScrollerHeight() {
  return scroller.value?.clientHeight ?? 0
}

function scrollToIndex(idx: number) {
  const el = scroller.value
  if (!el) return
  const h = getScrollerHeight()
  if (!h) return
  const next = Math.max(0, Math.min(idx, Math.max(0, filteredItems.value.length - 1)))
  el.scrollTo({ top: next * h, behavior: 'smooth' })
}

let scrollRaf = 0
function onScroll() {
  if (slideObserver) return
  if (!scroller.value) return
  if (scrollRaf) return
  scrollRaf = window.requestAnimationFrame(() => {
    scrollRaf = 0
    const el = scroller.value
    if (!el) return
    const h = el.clientHeight
    if (!h) return
    const idx = Math.round(el.scrollTop / h)
    if (idx !== activeIndex.value) activeIndex.value = idx
  })
}

function setupSlideObserver() {
  slideObserver?.disconnect()
  slideObserver = null
  if (!scroller.value || typeof IntersectionObserver === 'undefined') return

  slideObserver = new IntersectionObserver(
    (entries) => {
      const visible = entries
        .filter((entry) => entry.isIntersecting)
        .sort((a, b) => b.intersectionRatio - a.intersectionRatio)[0]
      if (!visible || visible.intersectionRatio < 0.65) return

      const nextIndex = Number((visible.target as HTMLElement).dataset.index)
      if (Number.isInteger(nextIndex) && nextIndex !== activeIndex.value) {
        activeIndex.value = nextIndex
      }
    },
    {
      root: scroller.value,
      threshold: [0.25, 0.5, 0.65, 0.8, 1],
    },
  )

  scroller.value.querySelectorAll<HTMLElement>('.slide').forEach((slide) => {
    slideObserver?.observe(slide)
  })
}

function pauseAllPlayers() {
  for (const player of playerMap.values()) player.pause()
}

async function playActive() {
  const requestId = ++playRequestId
  const item = activeItem.value
  if (!item) return
  pauseAllPlayers()
  await nextTick()
  if (requestId !== playRequestId) return
  await playerMap.get(item.id)?.play()
}

function toggleMute() {
  muted.value = !muted.value
  try {
    window.localStorage.setItem('feed.muted', String(muted.value))
  } catch {
    // 某些隐私模式会禁用 localStorage，静音功能本身仍需正常工作。
  }
  for (const player of playerMap.values()) player.setMuted(muted.value)
}

function togglePlayPause() {
  const item = activeItem.value
  if (!item) return
  void playerMap.get(item.id)?.toggle()
}

function toggleDanmaku() {
  danmakuEnabled.value = !danmakuEnabled.value
  try {
    window.localStorage.setItem('feed.danmaku', String(danmakuEnabled.value))
  } catch {
    // 隐私模式写不了 localStorage，当次会话的开关仍要生效。
  }
}

function onPlayerPlaying(videoId: number) {
  if (activeItem.value?.id === videoId) activePlaying.value = true
}

function onPlayerPaused(videoId: number) {
  if (activeItem.value?.id === videoId) activePlaying.value = false
  flushWatchProgress(videoId)
}

function onPlayerTime(videoId: number, seconds: number) {
  if (activeItem.value?.id === videoId) activePlayTime.value = seconds
  noteWatchProgress(videoId, seconds, playerMap.get(videoId)?.duration() ?? 0)
}

function onStageClick() {
  if (tapTimer !== undefined) window.clearTimeout(tapTimer)
  tapTimer = window.setTimeout(() => {
    tapTimer = undefined
    togglePlayPause()
  }, 240)
}

async function needLogin() {
  toast.error('请先登录')
  await router.push('/account')
}

async function loadRecommend(reset: boolean) {
  if (recommend.loading) return
  recommend.loading = true
  recommend.error = ''
  try {
    const res = await feedApi.listRecommend({
      limit: 10,
      exclude_ids: reset ? [] : recommend.excludeIds,
    })
    recommend.hasMore = res.has_more
    recommend.excludeIds = res.exclude_ids
    recommend.items = reset ? res.video_list : recommend.items.concat(res.video_list)
    await syncLikedState(res.video_list)
  } catch (e) {
    recommend.error = e instanceof ApiError ? e.message : String(e)
  } finally {
    recommend.loading = false
  }
}

async function loadHot(reset: boolean) {
  if (hot.loading) return
  hot.loading = true
  hot.error = ''
  try {
    const res = await feedApi.listLikesCount({
      limit: 10,
      likes_count_before: reset ? undefined : hot.nextLikesCountBefore,
      id_before: reset ? undefined : hot.nextIdBefore,
    })
    hot.hasMore = res.has_more
    hot.nextLikesCountBefore = res.next_likes_count_before
    hot.nextIdBefore = res.next_id_before
    hot.items = reset ? res.video_list : hot.items.concat(res.video_list)
    await syncLikedState(res.video_list)
  } catch (e) {
    hot.error = e instanceof ApiError ? e.message : String(e)
  } finally {
    hot.loading = false
  }
}

async function loadFollowing(reset: boolean) {
  if (!auth.isLoggedIn) {
    following.error = '登录后才能查看关注流'
    return
  }
  if (following.loading) return
  following.loading = true
  following.error = ''
  try {
    const res = await feedApi.listByFollowing({
      limit: 10,
      latest_time: reset ? 0 : following.nextTime,
      last_id: reset ? 0 : following.nextId,
    })
    following.hasMore = res.has_more
    following.nextTime = res.next_time
    following.nextId = res.next_id
    following.items = reset ? res.video_list : following.items.concat(res.video_list)
    await syncLikedState(res.video_list)
  } catch (e) {
    following.error = e instanceof ApiError ? e.message : String(e)
  } finally {
    following.loading = false
  }
}

async function ensureTabLoaded() {
  if (tab.value === 'recommend' && recommend.items.length === 0) await loadRecommend(true)
  if (tab.value === 'hot' && hot.items.length === 0) await loadHot(true)
  if (tab.value === 'following' && following.items.length === 0) await loadFollowing(true)
}

async function loadMoreIfNeeded() {
  const idx = activeIndex.value
  const items = filteredItems.value
  if (items.length === 0) return
  if (idx < items.length - 3) return

  if (tab.value === 'recommend' && recommend.hasMore) await loadRecommend(false)
  if (tab.value === 'hot' && hot.hasMore) await loadHot(false)
  if (tab.value === 'following' && following.hasMore) await loadFollowing(false)
}

async function toggleLike(item: FeedVideoItem) {
  if (!auth.isLoggedIn) return needLogin()
  const key = String(item.id)
  if (likeBusy[key]) return
  likeBusy[key] = true
  const previousLiked = item.is_liked
  const nextLiked = !previousLiked
  item.is_liked = nextLiked
  item.likes_count = Math.max(0, item.likes_count + (nextLiked ? 1 : -1))
  try {
    await likeApi.setLikedAndConfirm(item.id, nextLiked)
    track(nextLiked ? 'video_like' : 'video_unlike', { video_id: item.id, feed: tab.value })
  } catch (e) {
    item.is_liked = previousLiked
    item.likes_count = Math.max(0, item.likes_count + (previousLiked ? 1 : -1))
    const msg = e instanceof ApiError ? e.message : String(e)
    toast.error(msg)
  } finally {
    likeBusy[key] = false
  }
}

async function toggleFollow(authorId: number) {
  if (!auth.isLoggedIn) return needLogin()
  const key = String(authorId)
  if (followBusy[key] || social.isPending(authorId)) return
  followBusy[key] = true
  try {
    if (social.isFollowing(authorId)) {
      await social.unfollow(authorId)
      track('unfollow', { author_id: authorId, feed: tab.value })
      toast.info('已取关')
    } else {
      const author = currentState.value.items.find((item) => item.author.id === authorId)?.author
      await social.follow(authorId, author?.username)
      track('follow', { author_id: authorId, feed: tab.value })
      toast.success('已关注')
    }
  } catch (e) {
    const msg = e instanceof ApiError ? e.message : String(e)
    toast.error(msg)
  } finally {
    followBusy[key] = false
  }
}

function share(item: FeedVideoItem) {
  track('video_share', { video_id: item.id, feed: tab.value })
  shareSheet.value?.openFor(item.id)
}

function openReport(item: FeedVideoItem) {
  reportSheet.value?.openFor(item.id)
}

const drawer = reactive({
  open: false,
  video: null as FeedVideoItem | null,
  commentsLoading: false,
  commentsRefreshing: false,
  commentSubmitting: false,
  commentDeletingId: 0,
  error: '',
  comments: [] as Comment[],
  content: '',
  replyTarget: null as Comment | CommentReply | null,
  expandedReplies: {} as Record<number, boolean>,
})

const commentSyncAttempts = 6
const commentSyncDelayMs = 400

function clearReplyTarget() {
  drawer.replyTarget = null
}

function isRepliesOpen(commentId: number) {
  return !!drawer.expandedReplies[commentId]
}

function toggleReplies(commentId: number) {
  drawer.expandedReplies[commentId] = !drawer.expandedReplies[commentId]
}

function expandReplies(commentId: number) {
  if (commentId > 0) drawer.expandedReplies[commentId] = true
}

function applyComments(comments: Comment[]) {
  drawer.comments = comments
  if (drawer.video) {
    drawer.video.comment_count = countComments(comments)
  }
}

function wait(ms: number) {
  return new Promise<void>((resolve) => {
    window.setTimeout(resolve, ms)
  })
}

function closeDrawer() {
  drawer.open = false
  drawer.video = null
  drawer.commentsLoading = false
  drawer.commentsRefreshing = false
  drawer.commentSubmitting = false
  drawer.commentDeletingId = 0
  drawer.comments = []
  drawer.content = ''
  drawer.error = ''
  drawer.expandedReplies = {}
  clearReplyTarget()
}

async function openComments(item: FeedVideoItem) {
  if (drawer.video?.id !== item.id) {
    drawer.comments = []
  }
  drawer.open = true
  drawer.video = item
  drawer.content = ''
  clearReplyTarget()
  await loadComments()
}

async function loadComments() {
  if (!drawer.video) return
  const videoId = drawer.video.id
  drawer.commentsLoading = drawer.comments.length === 0
  drawer.commentsRefreshing = drawer.comments.length > 0
  drawer.error = ''
  try {
    const comments = await commentApi.listAll(videoId)
    if (!drawer.open || drawer.video?.id !== videoId) return
    applyComments(comments)
  } catch (e) {
    if (!drawer.open || drawer.video?.id !== videoId) return
    drawer.error = e instanceof ApiError ? e.message : String(e)
  } finally {
    if (drawer.video?.id === videoId) {
      drawer.commentsLoading = false
      drawer.commentsRefreshing = false
    }
  }
}

function isDrawerBusy() {
  return drawer.commentsLoading || drawer.commentsRefreshing || drawer.commentSubmitting || drawer.commentDeletingId !== 0
}

function startReply(target: Comment | CommentReply) {
  drawer.replyTarget = target
  const rootId = 'replies' in target ? target.id : target.root_comment_id || target.parent_comment_id
  expandReplies(rootId)
}

async function syncCommentsUntil(commentId: number, shouldExist: boolean) {
  const videoId = drawer.video?.id
  if (!videoId) return

  for (let attempt = 0; attempt < commentSyncAttempts; attempt += 1) {
    if (attempt > 0) {
      await wait(commentSyncDelayMs)
    }
    if (!drawer.open || drawer.video?.id !== videoId) {
      return
    }

    try {
      const comments = await commentApi.listAll(videoId)
      if (hasCommentID(comments, commentId) === shouldExist) {
        drawer.error = ''
        applyComments(comments)
        if (drawer.replyTarget && !hasCommentID(comments, drawer.replyTarget.id)) {
          clearReplyTarget()
        }
        return
      }
    } catch {
      // Ignore transient refresh failures and keep optimistic state.
    }
  }
}

async function publishComment() {
  if (!drawer.video) return
  if (!auth.isLoggedIn) return needLogin()
  const content = drawer.content.trim()
  if (!content) return
  const videoId = drawer.video.id
  drawer.commentSubmitting = true
  drawer.error = ''
  try {
    const res = await commentApi.publish(videoId, content, drawer.replyTarget?.id)
    if (!drawer.open || drawer.video?.id !== videoId) return
    drawer.content = ''
    clearReplyTarget()
    applyComments(insertPublishedComment(drawer.comments, res.comment))
    expandReplies(res.comment.root_comment_id || res.comment.parent_comment_id)
    void syncCommentsUntil(res.comment.id, true)
    track('comment_submit', { video_id: videoId, is_reply: !!res.comment.parent_comment_id })
    toast.success('评论已发布')
  } catch (e) {
    if (!drawer.open || drawer.video?.id !== videoId) return
    drawer.error = e instanceof ApiError ? e.message : String(e)
    toast.error(drawer.error)
  } finally {
    if (drawer.video?.id === videoId) drawer.commentSubmitting = false
  }
}

function canDeleteComment(c: Comment | CommentReply) {
  const myId = auth.claims?.account_id
  return !!myId && (myId === c.author_id || myId === drawer.video?.author.id)
}

async function deleteComment(commentId: number) {
  if (!drawer.video) return
  if (!auth.isLoggedIn) return needLogin()
  if (!window.confirm('确认删除这条评论？')) return
  const videoId = drawer.video.id
  drawer.commentDeletingId = commentId
  drawer.error = ''
  try {
    await commentApi.remove(commentId)
    if (!drawer.open || drawer.video?.id !== videoId) return
    const nextComments = removeCommentByID(drawer.comments, commentId)
    applyComments(nextComments)
    if (drawer.replyTarget && !hasCommentID(nextComments, drawer.replyTarget.id)) clearReplyTarget()
    void syncCommentsUntil(commentId, false)
    toast.info('评论已删除')
  } catch (e) {
    if (!drawer.open || drawer.video?.id !== videoId) return
    drawer.error = e instanceof ApiError ? e.message : String(e)
    toast.error(drawer.error)
  } finally {
    if (drawer.video?.id === videoId) drawer.commentDeletingId = 0
  }
}

async function onKeydown(e: KeyboardEvent) {
  const t = e.target as HTMLElement | null
  if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA')) return
  if (drawer.open) return

  if (e.key === 'ArrowDown') {
    e.preventDefault()
    scrollToIndex(activeIndex.value + 1)
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    scrollToIndex(activeIndex.value - 1)
  } else if (e.key === ' ') {
    e.preventDefault()
    togglePlayPause()
  } else if (e.key.toLowerCase() === 'm') {
    e.preventDefault()
    toggleMute()
  } else if (e.key.toLowerCase() === 'c') {
    if (activeItem.value) {
      e.preventDefault()
      await openComments(activeItem.value)
    }
  }
}

function onStageDoubleClick(item: FeedVideoItem) {
  if (tapTimer !== undefined) {
    window.clearTimeout(tapTimer)
    tapTimer = undefined
  }
  if (!auth.isLoggedIn) return
  void toggleLike(item)
}

watch(
  () => activeItem.value?.id,
  async (currentId, previousId) => {
    if (previousId && previousId !== currentId) {
      unbindWatchProgress(previousId)
      watchSession.end(playerMap.get(previousId) ?? undefined, { feed: tab.value })
    }
    activePlayTime.value = 0
    activePlaying.value = false
    await nextTick()
    if (currentId) bindWatchProgress(currentId, playerMap.get(currentId))
    await playActive()
    await loadMoreIfNeeded()
  },
)

watch(
  () => tab.value,
  async () => {
    unbindWatchProgress()
    watchSession.end(activeItem.value ? playerMap.get(activeItem.value.id) : undefined, { feed: tab.value })
    activeIndex.value = 0
    pauseAllPlayers()
    playerMap.clear()
    if (scroller.value) scroller.value.scrollTop = 0
    await ensureTabLoaded()
    await nextTick()
    setupSlideObserver()
    await playActive()
  },
)

watch(
  () => q.value,
  async () => {
    activeIndex.value = 0
    if (scroller.value) scroller.value.scrollTop = 0
    await nextTick()
    setupSlideObserver()
    await playActive()
  },
)

watch(
  () => filteredItems.value.length,
  async (len) => {
    if (len === 0) activeIndex.value = 0
    else if (activeIndex.value > len - 1) activeIndex.value = len - 1
    await nextTick()
    setupSlideObserver()
  },
)

watch(
  () => auth.isLoggedIn,
  async (v) => {
    if (tab.value === 'following' && v && following.items.length === 0) {
      await loadFollowing(true)
    }
    if (tab.value === 'recommend') {
      await loadRecommend(true)
    }
    if (!v) {
      await syncLikedState(recommend.items)
      await syncLikedState(hot.items)
      await syncLikedState(following.items)
    }
  },
)

watch(
  () => route.name,
  () => {
    const nextTab = routeTab()
    if (tab.value !== nextTab) {
      tab.value = nextTab
    }
  },
  { immediate: true },
)

onMounted(async () => {
  muted.value = readMutedPreference()
  danmakuEnabled.value = readDanmakuPreference()
  await ensureTabLoaded()
  await nextTick()
  setupSlideObserver()
  await playActive()
  window.addEventListener('keydown', onKeydown)
})

onBeforeUnmount(() => {
  if (tapTimer !== undefined) window.clearTimeout(tapTimer)
  unbindWatchProgress()
  watchSession.end(activeItem.value ? playerMap.get(activeItem.value.id) : undefined, { feed: tab.value })
  slideObserver?.disconnect()
  slideObserver = null
  pauseAllPlayers()
  playerMap.clear()
  window.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <AppShell full>
    <div class="page">
      <nav class="mobile-feed-tabs mobile-only" aria-label="移动端Feed切换">
        <RouterLink class="m-feed-tab" :class="{ on: tab === 'following' }" to="/following">关注</RouterLink>
        <RouterLink class="m-feed-tab" :class="{ on: tab === 'recommend' }" to="/">推荐</RouterLink>
        <RouterLink class="m-feed-tab" :class="{ on: tab === 'hot' }" to="/likes">点赞榜</RouterLink>
      </nav>
      <div ref="scroller" class="scroller" @scroll="onScroll">
        <section v-if="currentState.loading && currentState.items.length === 0" class="slide">
          <FeedStageSkeleton />
        </section>
        <div v-else-if="currentState.error && currentState.items.length === 0" class="center-hint bad">
          {{ currentState.error }}
        </div>
        <div v-else-if="filteredItems.length === 0" class="center-hint">没有匹配内容</div>

        <section
          v-for="(item, idx) in filteredItems"
          :key="`${tab}-${item.id}`"
          class="slide"
          :data-index="idx"
          :class="{ active: idx === activeIndex }"
        >
          <div class="stage" :class="{ 'has-composer': idx === activeIndex }" @click="onStageClick" @dblclick.prevent="onStageDoubleClick(item)">
            <VideoPlayer
              :ref="(el) => setPlayerRef(item.id, el as VideoPlayerHandle | null)"
              :src="item.play_url"
              :poster="item.cover_url"
              :active="idx === activeIndex"
              :enabled="Math.abs(idx - activeIndex) <= 1"
              :muted="muted"
              @playing="onPlayerPlaying(item.id); watchSession.play(item.id, { feed: tab })"
              @paused="onPlayerPaused(item.id)"
              @timeupdate="onPlayerTime(item.id, $event)"
            />
            <DanmakuLayer
              v-if="idx === activeIndex"
              :video-id="item.id"
              :current-time="activePlayTime"
              :playing="activePlaying"
              :enabled="danmakuEnabled"
              :muted="muted"
              @toggle-enabled="toggleDanmaku"
              @toggle-muted="toggleMute"
            />
            <div class="grad" />

            <div class="meta">
              <RouterLink class="author-link" :to="`/u/${item.author.id}`" @click.stop>
                <UserAvatar :username="item.author.username" :id="item.author.id" :size="40" />
                <span class="author-name">@{{ item.author.username }}</span>
              </RouterLink>
              <div class="title">{{ item.title }}</div>
              <div v-if="item.description" class="desc">{{ item.description }}</div>
            </div>

            <div class="actions">
              <button
                v-if="canInteractWithLike"
                class="act"
                type="button"
                :disabled="!!likeBusy[String(item.id)]"
                @click.stop="toggleLike(item)"
              >
                <span class="icon" :class="{ liked: item.is_liked }">
                  <AppIcon :name="item.is_liked ? 'heart-fill' : 'heart'" :size="28" />
                </span>
                <span class="count">{{ item.likes_count }}</span>
              </button>

              <div v-else class="act metric">
                <span class="icon">
                  <AppIcon name="heart" :size="28" />
                </span>
                <span class="count">{{ item.likes_count }}</span>
              </div>

              <button class="act" type="button" @click.stop="openComments(item)">
                <span class="icon">
                  <AppIcon name="comment" :size="26" />
                </span>
                <span class="count">{{ item.comment_count }}</span>
              </button>

              <button
                v-if="canInteractWithFollow && (!myAccountId || myAccountId !== item.author.id)"
                class="act"
                type="button"
                :disabled="!!followBusy[String(item.author.id)] || social.isPending(item.author.id)"
                @click.stop="toggleFollow(item.author.id)"
              >
                <span class="icon">
                  <AppIcon name="plus" :size="26" />
                </span>
                <span class="count">{{ social.isFollowing(item.author.id) ? '已关注' : '关注' }}</span>
              </button>

              <button class="act" type="button" @click.stop="share(item)">
                <span class="icon">
                  <AppIcon name="share" :size="26" />
                </span>
                <span class="count">分享</span>
              </button>

              <button
                v-if="!auth.isLoggedIn || myAccountId !== item.author.id"
                class="act"
                type="button"
                @click.stop="openGiveTip(item)"
              >
                <span class="icon">
                  <AppIcon name="coin" :size="26" />
                </span>
                <span class="count">打赏</span>
              </button>
              <button
                v-if="auth.isLoggedIn"
                class="act desktop-only"
                type="button"
                @click.stop="openInboxTip(item)"
              >
                <span class="icon">
                  <AppIcon name="list" :size="24" />
                </span>
                <span class="count">打赏记录</span>
              </button>

              <button
                v-if="!auth.isLoggedIn || myAccountId !== item.author.id"
                class="act desktop-only"
                type="button"
                @click.stop="openReport(item)"
              >
                <span class="icon">
                  <AppIcon name="flag" :size="24" />
                </span>
                <span class="count">举报</span>
              </button>
            </div>
          </div>
        </section>
        <section v-if="currentState.loading && currentState.items.length > 0" class="slide">
          <FeedStageSkeleton />
        </section>
      </div>

      <div v-if="drawer.open" class="drawer-backdrop" @click.self="closeDrawer">
        <div class="drawer">
          <div class="drawer-head">
            <div class="drawer-title">{{ drawer.video?.title ?? '评论' }}</div>
            <button class="drawer-x" type="button" @click="closeDrawer">×</button>
          </div>

          <div class="drawer-body">
            <CommentListSkeleton v-if="drawer.commentsLoading && drawer.comments.length === 0" />
            <div v-else-if="drawer.error" class="drawer-hint bad">{{ drawer.error }}</div>
            <div v-else-if="drawer.comments.length === 0" class="drawer-hint">暂无评论</div>

            <div class="comment" v-for="c in drawer.comments" :key="c.id">
              <CommentAuthor :username="c.username" :author-id="c.author_id" :created-at="c.created_at">
                <div class="comment-content">{{ c.content }}</div>
                <div class="comment-actions comment-actions-left">
                  <button class="chip" type="button" :disabled="isDrawerBusy()" @click="startReply(c)">回复</button>
                  <button v-if="canDeleteComment(c)" class="chip danger" type="button" :disabled="isDrawerBusy()" @click="deleteComment(c.id)">
                    删除
                  </button>
                  <button v-if="c.replies.length > 0" class="chip" type="button" @click="toggleReplies(c.id)">
                    {{ isRepliesOpen(c.id) ? '收起' : `展开 ${c.replies.length} 条回复` }}
                  </button>
                </div>
              </CommentAuthor>

              <div v-if="c.replies.length > 0 && isRepliesOpen(c.id)" class="reply-list">
                <div class="reply" v-for="reply in c.replies" :key="reply.id">
                  <CommentAuthor :username="reply.username" :author-id="reply.author_id" :created-at="reply.created_at" :size="28">
                    <div class="comment-content">
                      <span v-if="reply.reply_to_username" class="reply-prefix">回复 @{{ reply.reply_to_username }}：</span>{{ reply.content }}
                    </div>
                    <div class="comment-actions comment-actions-left">
                      <button class="chip" type="button" :disabled="isDrawerBusy()" @click="startReply(reply)">回复</button>
                      <button v-if="canDeleteComment(reply)" class="chip danger" type="button" :disabled="isDrawerBusy()" @click="deleteComment(reply.id)">
                        删除
                      </button>
                    </div>
                  </CommentAuthor>
                </div>
              </div>
            </div>
          </div>

          <div class="drawer-foot">
            <div v-if="drawer.replyTarget" class="reply-banner">
              <span>回复 @{{ drawer.replyTarget.username }}</span>
              <button class="chip" type="button" :disabled="isDrawerBusy()" @click="clearReplyTarget">取消</button>
            </div>
            <textarea v-model="drawer.content" :placeholder="drawer.replyTarget ? `回复 @${drawer.replyTarget.username}…` : '说点什么…'" :disabled="isDrawerBusy()" />
            <div class="row" style="justify-content: space-between; margin-top: 8px">
              <button class="chip" type="button" :disabled="isDrawerBusy()" @click="loadComments">刷新</button>
              <button class="chip primary" type="button" :disabled="isDrawerBusy() || !drawer.content.trim()" @click="publishComment">
                发送
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
    <TipSheet
      v-if="tipTarget"
      ref="tipSheet"
      :video-id="tipTarget.id"
      :author-username="tipTarget.author.username"
      :is-author="!!myAccountId && myAccountId === tipTarget.author.id"
    />
    <ShareSheet ref="shareSheet" />
    <ReportSheet ref="reportSheet" />
  </AppShell>
</template>

<style scoped>
.page {
  flex: 1 1 0%;
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
  padding: 12px 20px 20px;
  box-sizing: border-box;
  overflow: hidden;
}

.scroller {
  flex: 1 1 0%;
  height: 100%;
  min-height: 0;
  overflow-y: auto;
  scroll-snap-type: y mandatory;
  scroll-behavior: smooth;
  scrollbar-width: none;
  -ms-overflow-style: none;
  border-radius: 16px;
  background: #000;
  display: flex;
  flex-direction: column;
}

.scroller::-webkit-scrollbar {
  width: 0;
  height: 0;
}

.center-hint {
  height: calc(100% - 60px);
  display: grid;
  place-items: center;
  color: rgba(255, 255, 255, 0.78);
}

.center-hint.bad {
  color: rgba(254, 44, 85, 0.92);
}

.slide {
  height: 100%;
  min-height: 100%;
  box-sizing: border-box;
  scroll-snap-align: start;
  scroll-snap-stop: always;
  display: flex;
}

.stage {
  flex: 1;
  min-width: 0;
  min-height: 0;
  width: 100%;
  height: 100%;
  position: relative;
  overflow: hidden;
  background: #000;
}

.video {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  background: rgba(0, 0, 0, 0.4);
}

.grad {
  position: absolute;
  inset: 0;
  background: linear-gradient(to top, rgba(0, 0, 0, 0.68), rgba(0, 0, 0, 0.12) 40%, rgba(0, 0, 0, 0) 70%);
  pointer-events: none;
}

.meta {
  position: absolute;
  left: 20px;
  bottom: 18px;
  max-width: min(620px, calc(100% - 108px));
  color: #fff;
}

.stage.has-composer .meta {
  bottom: 72px;
}

.author-link {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  font-weight: 700;
  letter-spacing: 0.2px;
  margin-bottom: 8px;
  text-decoration: none;
  color: #fff;
}

.author-link:hover {
  text-decoration: none;
}

.author-name {
  font-size: 15px;
  text-shadow: 0 1px 8px rgba(0, 0, 0, 0.65);
}

.title {
  font-size: 16px;
  font-weight: 700;
  margin-bottom: 6px;
  text-shadow: 0 1px 8px rgba(0, 0, 0, 0.65);
}

.desc {
  color: rgba(255, 255, 255, 0.74);
  font-size: 13px;
  line-height: 1.35;
}

.actions {
  position: absolute;
  right: 16px;
  bottom: 18px;
  z-index: 3;
  display: grid;
  gap: 16px;
}

.stage.has-composer .actions {
  bottom: 72px;
}

.act {
  appearance: none;
  width: 52px;
  border: 0;
  background: transparent;
  color: rgba(255, 255, 255, 0.96);
  padding: 0;
  cursor: pointer;
  display: grid;
  gap: 4px;
  justify-items: center;
}

.act:hover {
  color: #fff;
}

.metric {
  cursor: default;
}

.act:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}

.icon {
  width: 40px;
  height: 40px;
  display: grid;
  place-items: center;
  filter: drop-shadow(0 2px 6px rgba(0, 0, 0, 0.55));
}

.icon.liked {
  color: rgba(254, 44, 85, 1);
}

.count {
  font-size: 12px;
  font-weight: 600;
  color: rgba(255, 255, 255, 0.92);
  text-shadow: 0 1px 4px rgba(0, 0, 0, 0.75);
}

.chip {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 7px 10px;
  border-radius: 999px;
  border: 1px solid rgba(255, 255, 255, 0.14);
  background: rgba(0, 0, 0, 0.28);
  color: rgba(255, 255, 255, 0.86);
  font-size: 12px;
  text-decoration: none;
}

.chip.primary {
  border-color: rgba(254, 44, 85, 0.45);
  background: rgba(254, 44, 85, 0.14);
}

.chip.danger {
  border-color: rgba(254, 44, 85, 0.55);
  background: rgba(254, 44, 85, 0.12);
}

.drawer .chip {
  border-color: var(--border);
  background: var(--fill);
  color: var(--text);
}

.drawer .chip.primary {
  border-color: transparent;
  background: #fe2c55;
  color: #fff;
}

.drawer .chip.danger {
  border-color: rgba(254, 44, 85, 0.35);
  background: rgba(254, 44, 85, 0.1);
  color: #fe2c55;
}

.drawer-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.55);
  backdrop-filter: blur(10px);
  z-index: 120;
  display: grid;
  justify-items: end;
}

.drawer {
  width: min(420px, calc(100vw - 18px));
  height: 100vh;
  background: var(--surface);
  color: var(--text);
  border-left: 1px solid var(--border);
  box-shadow: var(--shadow);
  display: grid;
  grid-template-rows: auto 1fr auto;
}

.drawer-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 14px;
  border-bottom: 1px solid var(--border);
}

.drawer-title {
  font-weight: 800;
  font-size: 14px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.drawer-x {
  width: 34px;
  height: 34px;
  min-height: 0;
  border-radius: 10px;
  border: 1px solid var(--border);
  background: var(--fill);
  color: var(--text);
  cursor: pointer;
  font-size: 20px;
  line-height: 1;
}

.drawer-body {
  overflow: auto;
  padding: 12px 14px;
  display: grid;
  gap: 10px;
}

.drawer-foot {
  border-top: 1px solid var(--border);
  padding: 12px 14px;
}

.drawer-foot textarea {
  width: 100%;
  min-height: 82px;
  resize: none;
  border-radius: 12px;
  border: 1px solid var(--border);
  background: var(--input-bg);
  color: var(--text);
  padding: 10px 12px;
  outline: none;
}

.drawer-hint {
  color: var(--muted);
  padding: 12px 0;
}

.drawer-hint.bad {
  color: rgba(254, 44, 85, 0.92);
}

.comment {
  border: 1px solid var(--border);
  background: var(--fill);
  border-radius: 12px;
  padding: 10px 10px;
}

.reply-list {
  margin-top: 10px;
  display: grid;
  gap: 8px;
}

.reply {
  border-left: 2px solid rgba(254, 44, 85, 0.25);
  padding-left: 10px;
}

.comment-content {
  margin-top: 8px;
  font-size: 13px;
  line-height: 1.35;
  color: var(--text);
  white-space: pre-wrap;
  word-break: break-word;
}

.reply-prefix {
  color: rgba(254, 44, 85, 0.9);
}

.comment-actions {
  margin-top: 10px;
  display: flex;
  justify-content: flex-end;
}

.comment-actions-left {
  justify-content: flex-start;
  gap: 8px;
}

.reply-banner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 8px;
  padding: 8px 10px;
  border-radius: 12px;
  background: var(--fill);
  color: var(--muted);
  font-size: 12px;
}

@media (max-width: 900px) {
  .page {
    padding: 0;
    position: relative;
    height: 100%;
    min-height: 0;
    flex: 1 1 0%;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .mobile-feed-tabs {
    position: absolute;
    top: calc(8px + env(safe-area-inset-top, 0px));
    left: 50%;
    transform: translateX(-50%);
    z-index: 25;
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 4px 14px;
    background: rgba(0, 0, 0, 0.35);
    backdrop-filter: blur(12px);
    -webkit-backdrop-filter: blur(12px);
    border: 1px solid rgba(255, 255, 255, 0.12);
    border-radius: 999px;
    pointer-events: auto;
    white-space: nowrap;
    flex-shrink: 0;
  }

  .m-feed-tab {
    color: rgba(255, 255, 255, 0.68);
    font-size: 14px;
    font-weight: 600;
    text-decoration: none;
    padding: 4px 6px;
    position: relative;
    transition: color 150ms ease;
    white-space: nowrap;
    flex-shrink: 0;
  }

  .m-feed-tab.on {
    color: #ffffff;
    font-weight: 800;
  }

  .m-feed-tab.on::after {
    content: '';
    position: absolute;
    left: 15%;
    right: 15%;
    bottom: 0px;
    height: 2px;
    border-radius: 2px;
    background: #fe2c55;
  }

  .scroller {
    border-radius: 0;
    height: 100%;
    min-height: 0;
  }

  .chip {
    padding: 6px 9px;
    font-size: 12px;
  }

  .meta {
    left: 12px;
    right: 70px;
    bottom: calc(14px + env(safe-area-inset-bottom, 0px));
    max-width: none;
  }

  .stage.has-composer .meta {
    bottom: calc(20px + env(safe-area-inset-bottom, 0px));
  }

  .actions {
    right: 8px;
    bottom: calc(14px + env(safe-area-inset-bottom, 0px));
    gap: 10px;
  }

  .stage.has-composer .actions {
    bottom: calc(20px + env(safe-area-inset-bottom, 0px));
  }

  .act {
    width: 44px;
    gap: 2px;
  }

  .icon {
    width: 36px;
    height: 36px;
  }

  .count {
    font-size: 11px;
  }

  .drawer-backdrop {
    justify-items: center;
    align-items: end;
    padding-bottom: var(--bottom-nav-h, 56px);
  }

  .drawer {
    width: 100%;
    max-width: 100vw;
    height: min(70dvh, 560px);
    border-left: none;
    border-top: 1px solid rgba(255, 255, 255, 0.12);
    border-radius: 18px 18px 0 0;
    overflow: hidden;
  }

  .drawer-foot {
    padding-bottom: calc(12px + env(safe-area-inset-bottom, 0px));
  }
}
</style>
