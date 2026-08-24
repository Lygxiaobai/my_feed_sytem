<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { track } from '../analytics/track'
import AppShell from '../components/AppShell.vue'
import Skeleton from '../components/Skeleton.vue'
import UserAvatar from '../components/UserAvatar.vue'
import UserListSkeleton from '../components/UserListSkeleton.vue'
import VideoGridSkeleton from '../components/VideoGridSkeleton.vue'
import { ApiError } from '../api/client'
import * as accountApi from '../api/account'
import * as socialApi from '../api/social'
import type { Account, SocialRelation, Video } from '../api/types'
import * as videoApi from '../api/video'
import { useAuthStore } from '../stores/auth'
import { useDMStore } from '../stores/dm'
import { useSocialStore } from '../stores/social'
import { useToastStore } from '../stores/toast'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const social = useSocialStore()
const dm = useDMStore()
const toast = useToastStore()

const userId = computed(() => Number(route.params.id))
const myId = computed(() => auth.claims?.account_id ?? 0)
const isMe = computed(() => myId.value > 0 && myId.value === userId.value)

const state = reactive({
  loading: false,
  error: '',
  user: null as Account | null,
  videos: [] as Video[],
  followers: [] as SocialRelation[],
  vloggers: [] as SocialRelation[],
  socialLoading: false,
  socialError: '',
})
const followBusy = ref(false)
const drafts = ref<Video[]>([])
const draftsLoading = ref(false)

/**
 * 本人资料页不再把草稿箱做成并列 Tab，而是作品下的药丸。
 * 投稿页旧链 /u/:id?tab=drafts 仍映射到草稿筛选。
 */
type WorkFilter = 'public' | 'private' | 'drafts'

function parseWorkFilter(): WorkFilter {
  if (!isMe.value) return 'public'
  const works = String(Array.isArray(route.query.works) ? route.query.works[0] : route.query.works ?? '')
  const tab = String(Array.isArray(route.query.tab) ? route.query.tab[0] : route.query.tab ?? '')
  if (works === 'private' || works === 'drafts') return works
  if (tab === 'drafts' || tab === 'private') return tab
  return 'public'
}

const workFilter = computed<WorkFilter>(parseWorkFilter)

const isFollowing = computed(() => (auth.isLoggedIn ? social.isFollowing(userId.value) : false))
const totalReceivedLikes = computed(() => state.videos.reduce((sum, item) => sum + (item.likes_count ?? 0), 0))

async function loadProfile() {
  if (!Number.isFinite(userId.value) || userId.value <= 0) {
    state.error = '无效的用户 id'
    return
  }

  state.loading = true
  state.error = ''
  try {
    const [u, vids] = await Promise.all([accountApi.findById(userId.value), videoApi.listByAuthorId(userId.value)])
    state.user = u
    state.videos = vids
    if (isMe.value) await loadDrafts()
  } catch (e) {
    state.error = e instanceof ApiError ? e.message : String(e)
    state.user = null
    state.videos = []
  } finally {
    state.loading = false
  }

  await loadSocialCounts()
}

async function loadSocialCounts() {
  state.socialError = ''

  if (!auth.isLoggedIn) {
    state.socialLoading = false
    state.followers = []
    state.vloggers = []
    return
  }
  if (!Number.isFinite(userId.value) || userId.value <= 0) {
    state.socialLoading = false
    state.followers = []
    state.vloggers = []
    return
  }

  state.socialLoading = true
  try {
    const [followersRes, vloggersRes] = await Promise.all([
      socialApi.getAllFollowers(userId.value),
      socialApi.getAllVloggers(userId.value),
    ])
    state.followers = followersRes.followers
    state.vloggers = vloggersRes.vloggers
  } catch (e) {
    state.socialError = e instanceof ApiError ? e.message : String(e)
  } finally {
    state.socialLoading = false
  }
}

function applyFollowerOptimisticChange(shouldFollow: boolean) {
  const followerId = myId.value
  const user = state.user
  if (!followerId || !user) return

  if (shouldFollow) {
    if (state.followers.some((item) => item.follower_id === followerId)) return
    state.followers = state.followers.concat({
      id: 0,
      follower_id: followerId,
      vlogger_id: user.id,
      created_at: new Date().toISOString(),
      follower_username: auth.claims?.username,
      vlogger_username: user.username,
    })
    return
  }

  state.followers = state.followers.filter((item) => item.follower_id !== followerId)
}

async function syncSocialCountsUntil(shouldFollow: boolean) {
  const currentUserId = userId.value
  const currentFollowerId = myId.value
  if (!auth.isLoggedIn || !currentFollowerId || !Number.isFinite(currentUserId) || currentUserId <= 0) return

  for (let attempt = 0; attempt < 6; attempt += 1) {
    if (attempt > 0) {
      await new Promise<void>((resolve) => {
        window.setTimeout(resolve, 400)
      })
    }
    if (userId.value !== currentUserId) return

    try {
      const [followersRes, vloggersRes] = await Promise.all([
        socialApi.getAllFollowers(currentUserId),
        socialApi.getAllVloggers(currentUserId),
      ])
      if (followersRes.followers.some((item) => item.follower_id === currentFollowerId) === shouldFollow) {
        state.socialError = ''
        state.followers = followersRes.followers
        state.vloggers = vloggersRes.vloggers
        return
      }
    } catch {
      // Keep optimistic state and retry quietly.
    }
  }
}

async function toggleFollow() {
  if (isMe.value) return
  if (!auth.isLoggedIn) {
    toast.error('请先登录')
    await router.push('/account')
    return
  }
  if (followBusy.value || social.isPending(userId.value)) return

  const nextShouldFollow = !isFollowing.value
  followBusy.value = true
  try {
    applyFollowerOptimisticChange(nextShouldFollow)
    if (!nextShouldFollow) {
      await social.unfollow(userId.value)
      track('unfollow', { author_id: userId.value, from: 'profile' })
      toast.info('已取关')
    } else {
      await social.follow(userId.value, state.user?.username)
      track('follow', { author_id: userId.value, from: 'profile' })
      toast.success('已关注')
    }
    void syncSocialCountsUntil(nextShouldFollow)
  } catch (e) {
    applyFollowerOptimisticChange(!nextShouldFollow)
    const msg = e instanceof ApiError ? e.message : String(e)
    toast.error(msg)
  } finally {
    followBusy.value = false
  }
}

type ListTab = 'followers' | 'following'
const drawer = reactive({
  open: false,
  tab: 'followers' as ListTab,
})

function openFollowers() {
  drawer.tab = 'followers'
  drawer.open = true
}

function openFollowing() {
  drawer.tab = 'following'
  drawer.open = true
}

function closeDrawer() {
  drawer.open = false
}

const listTitle = computed(() => (drawer.tab === 'followers' ? '粉丝' : '关注'))
const listItems = computed(() => (drawer.tab === 'followers' ? state.followers : state.vloggers))

function relationUserId(item: SocialRelation) {
  return drawer.tab === 'followers' ? item.follower_id : item.vlogger_id
}

function relationUsername(item: SocialRelation) {
  return drawer.tab === 'followers'
    ? item.follower_username || `用户 #${relationUserId(item)}`
    : item.vlogger_username || `用户 #${relationUserId(item)}`
}

async function goUser(item: SocialRelation) {
  drawer.open = false
  await router.push(`/u/${relationUserId(item)}`)
}

async function goVideo(item: Video) {
  if (videoApi.isDraft(item)) {
    await router.push({ path: '/video', query: { draft: String(item.id) } })
    return
  }
  await router.push(`/video/${item.id}`)
}

async function setWorkFilter(next: WorkFilter) {
  const query = { ...route.query }
  delete query.tab
  if (next === 'public') delete query.works
  else query.works = next
  await router.replace({ query })
  if (next === 'drafts') await loadDrafts()
}

async function loadDrafts() {
  if (!isMe.value) {
    drafts.value = []
    return
  }
  draftsLoading.value = true
  try {
    drafts.value = await videoApi.listDrafts()
  } catch {
    drafts.value = []
  } finally {
    draftsLoading.value = false
  }
}

function videoBadge(item: Video) {
  if (item.audit_status === 'pending' || item.audit_status === 'reviewing') return '审核中'
  if (item.audit_status === 'rejected') return '未过审'
  return ''
}

const publicWorks = computed(() =>
  state.videos.filter((item) => !videoApi.isUnpublished(item) && !videoApi.isDraft(item)),
)
const privateWorks = computed(() => state.videos.filter((item) => videoApi.isUnpublished(item)))
const shownVideos = computed(() => {
  if (!isMe.value) return publicWorks.value
  if (workFilter.value === 'drafts') return drafts.value
  if (workFilter.value === 'private') return privateWorks.value
  return publicWorks.value
})
const listLoading = computed(() => {
  if (workFilter.value === 'drafts' && isMe.value) return state.loading || draftsLoading.value
  return state.loading
})
const worksEmptyHint = computed(() => {
  if (!isMe.value) return '暂无作品'
  if (workFilter.value === 'drafts') return '暂无草稿'
  if (workFilter.value === 'private') return '没有私密作品'
  return '暂无作品'
})

async function goMessage() {
  if (!auth.isLoggedIn) {
    toast.error('请先登录')
    await router.push('/account')
    return
  }
  if (!Number.isFinite(userId.value) || userId.value <= 0) return
  // 桌面用顶栏下拉展开会话，避免再跳进一整页。
  if (window.matchMedia('(min-width: 901px)').matches) {
    dm.openChat(userId.value)
    return
  }
  await router.push({ path: '/messages', query: { u: String(userId.value) } })
}

watch(
  () => route.params.id,
  async () => {
    drawer.open = false
    await loadProfile()
  },
)

watch(
  () => auth.isLoggedIn,
  async () => {
    await loadSocialCounts()
    if (isMe.value) await loadDrafts()
  },
)

watch(workFilter, async (next) => {
  if (next === 'drafts') await loadDrafts()
})

onMounted(loadProfile)
</script>

<template>
  <AppShell>
    <div class="card profile-card">
      <div class="profile-head">
        <UserAvatar :username="state.user?.username ?? 'User'" :id="state.user?.id ?? userId" :size="56" />
        <div class="profile-id">
          <div class="title" style="margin: 0">
            <Skeleton v-if="state.loading && !state.user" width="120px" height="18px" />
            <template v-else>{{ state.user?.username ?? '-' }}</template>
          </div>
          <div class="stats">
            <button
              class="stat"
              type="button"
              :disabled="!auth.isLoggedIn || state.loading || state.socialLoading"
              @click="openFollowers"
            >
              <strong>
                <Skeleton v-if="state.loading || (auth.isLoggedIn && state.socialLoading)" width="16px" height="14px" />
                <template v-else>{{ auth.isLoggedIn ? state.followers.length : '—' }}</template>
              </strong>
              粉丝
            </button>
            <button
              class="stat"
              type="button"
              :disabled="!auth.isLoggedIn || state.loading || state.socialLoading"
              @click="openFollowing"
            >
              <strong>
                <Skeleton v-if="state.loading || (auth.isLoggedIn && state.socialLoading)" width="16px" height="14px" />
                <template v-else>{{ auth.isLoggedIn ? state.vloggers.length : '—' }}</template>
              </strong>
              关注
            </button>
            <span class="stat static">
              <strong>
                <Skeleton v-if="state.loading" width="16px" height="14px" />
                <template v-else>{{ totalReceivedLikes }}</template>
              </strong>
              获赞
            </span>
          </div>
          <div v-if="!auth.isLoggedIn" class="subtle">登录后可查看粉丝/关注列表</div>
          <div v-else-if="state.socialError" class="subtle">社交信息加载失败：{{ state.socialError }}</div>
        </div>
        <div class="profile-actions">
          <button v-if="isMe" class="ghost compact" type="button" @click="router.push('/account')">我的账号</button>
          <template v-else>
            <button
              class="ghost compact"
              type="button"
              :disabled="!state.user || state.loading"
              @click="goMessage"
            >
              发私信
            </button>
            <button
              class="primary compact"
              type="button"
              :disabled="!state.user || state.loading || followBusy || social.isPending(userId)"
              @click="toggleFollow"
            >
              {{ isFollowing ? '已关注' : '关注' }}
            </button>
          </template>
        </div>
      </div>

      <div v-if="state.error" class="hint bad" style="margin-top: 12px">{{ state.error }}</div>

      <template v-else>
        <div class="tabs">
          <button class="tab active" type="button">
            作品
            <span class="tab-count">{{ state.loading ? '…' : publicWorks.length }}</span>
          </button>
        </div>

        <div v-if="isMe" class="work-filters">
          <button type="button" :class="{ active: workFilter === 'public' }" @click="setWorkFilter('public')">
            作品 {{ state.loading ? '…' : publicWorks.length }}
          </button>
          <button type="button" :class="{ active: workFilter === 'private' }" @click="setWorkFilter('private')">
            私密作品 {{ state.loading ? '…' : privateWorks.length }}
            <span class="lock" aria-hidden="true">🔒</span>
          </button>
          <button type="button" :class="{ active: workFilter === 'drafts' }" @click="setWorkFilter('drafts')">
            草稿 {{ draftsLoading ? '…' : drafts.length }}
          </button>
        </div>

        <VideoGridSkeleton v-if="listLoading" style="margin-top: 12px" />
        <div v-else-if="shownVideos.length === 0" class="hint" style="margin-top: 12px">{{ worksEmptyHint }}</div>

        <div v-else class="video-grid" style="margin-top: 12px">
          <button v-for="v in shownVideos" :key="v.id" class="video-card" type="button" @click="goVideo(v)">
            <img class="video-cover" :src="v.cover_url" :alt="videoApi.videoDisplayTitle(v)" loading="lazy" />
            <span v-if="isMe && workFilter === 'public' && videoBadge(v)" class="video-badge">{{ videoBadge(v) }}</span>
            <div class="video-meta">
              <div class="video-title">{{ videoApi.videoDisplayTitle(v) }}</div>
              <div class="video-sub subtle">
                ❤️ {{ v.likes_count }} · 💬 {{ v.comment_count }} · {{ new Date(v.created_at).toLocaleDateString() }}
              </div>
            </div>
          </button>
        </div>
      </template>
    </div>

    <div v-if="drawer.open" class="drawer-backdrop" @click.self="closeDrawer">
      <div class="drawer">
        <div class="drawer-head">
          <div class="drawer-title">{{ listTitle }}</div>
          <button class="drawer-x" type="button" @click="closeDrawer">×</button>
        </div>
        <div class="drawer-body">
          <UserListSkeleton v-if="state.socialLoading" />
          <div v-else-if="state.socialError" class="drawer-hint bad">{{ state.socialError }}</div>
          <div v-else-if="listItems.length === 0" class="drawer-hint">暂无</div>

          <button v-for="u in listItems" v-if="!state.socialLoading && !state.socialError" :key="u.id" class="user-row" type="button" @click="goUser(u)">
            <UserAvatar :username="relationUsername(u)" :id="relationUserId(u)" :size="40" />
            <div class="user-meta">
              <div class="user-name">{{ relationUsername(u) }}</div>
            </div>
          </button>
        </div>
      </div>
    </div>
  </AppShell>
</template>

<style scoped>
.ghost {
  border: 1px solid rgba(var(--fg), 0.14);
  background: var(--fill);
  color: rgba(var(--fg), 0.86);
  border-radius: 12px;
  padding: 10px 12px;
  cursor: pointer;
}

.ghost:hover {
  background: rgba(var(--fg), 0.1);
}

.ghost.compact,
.primary.compact {
  padding: 6px 12px;
  border-radius: 999px;
  font-size: 13px;
  min-height: 32px;
}

.profile-head {
  display: flex;
  align-items: center;
  gap: 14px;
}

.profile-id {
  min-width: 0;
  flex: 1;
  display: grid;
  gap: 6px;
}

.profile-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  flex-shrink: 0;
}

.stats {
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
  align-items: baseline;
}

.stat {
  border: 0;
  background: transparent;
  padding: 0;
  min-height: 0;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 13px;
  color: rgba(var(--fg), 0.55);
  cursor: pointer;
}

.stat:hover {
  background: transparent;
  color: rgba(var(--fg), 0.7);
}

.stat strong {
  font-size: 16px;
  font-weight: 700;
  color: rgba(var(--fg), 0.92);
}

.stat.static {
  cursor: default;
}

.stat:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}

.tabs {
  display: flex;
  gap: 20px;
  margin-top: 16px;
  border-bottom: 1px solid rgba(var(--fg), 0.08);
}

.tab {
  position: relative;
  border: 0;
  background: transparent;
  padding: 10px 0;
  min-height: 0;
  font-weight: 600;
  color: rgba(var(--fg), 0.55);
  cursor: pointer;
}

.tab:hover {
  background: transparent;
  color: rgba(var(--fg), 0.8);
}

.tab.active {
  color: rgba(var(--fg), 0.92);
}

.tab.active::after {
  content: '';
  position: absolute;
  left: 0;
  right: 0;
  bottom: -1px;
  height: 2px;
  background: #fe2c55;
  border-radius: 2px;
}

.tab-count {
  margin-left: 4px;
  font-weight: 500;
  color: rgba(var(--fg), 0.55);
}

.work-filters {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 12px;
}

.work-filters button {
  border: 0;
  border-radius: 999px;
  padding: 6px 12px;
  min-height: 0;
  font-size: 13px;
  background: rgba(var(--fg), 0.08);
  color: rgba(var(--fg), 0.62);
  cursor: pointer;
}

.work-filters button:hover {
  background: rgba(var(--fg), 0.12);
}

.work-filters button.active {
  background: rgba(254, 44, 85, 0.14);
  color: #fe2c55;
  font-weight: 600;
}

.work-filters .lock {
  margin-left: 2px;
  font-size: 11px;
}

@media (max-width: 640px) {
  .profile-head {
    flex-wrap: wrap;
  }

  .profile-actions {
    width: 100%;
    padding-left: 70px;
  }
}

.hint {
  color: rgba(var(--fg), 0.78);
}

.hint.bad {
  color: rgba(254, 44, 85, 0.92);
}

.video-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}

@media (max-width: 1100px) {
  .video-grid {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
}

@media (max-width: 800px) {
  .video-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

.video-card {
  position: relative;
  border: 1px solid rgba(var(--fg), 0.12);
  background: rgba(var(--fg), 0.05);
  border-radius: 16px;
  overflow: hidden;
  padding: 0;
  text-align: left;
  cursor: pointer;
}

.video-card:hover {
  background: rgba(var(--fg), 0.08);
}

.video-badge {
  position: absolute;
  top: 8px;
  left: 8px;
  padding: 2px 8px;
  border-radius: 999px;
  font-size: 11px;
  background: rgba(0, 0, 0, 0.62);
  color: #fff;
}

.video-cover {
  width: 100%;
  aspect-ratio: 9/12;
  object-fit: cover;
  display: block;
  background: rgba(0, 0, 0, 0.35);
}

.video-meta {
  padding: 10px 10px;
}

.video-title {
  font-weight: 800;
  font-size: 13px;
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}

.video-sub {
  margin-top: 6px;
  font-size: 12px;
}

.drawer-backdrop {
  position: fixed;
  inset: 0;
  background: var(--toast-bg);
  backdrop-filter: blur(10px);
  z-index: 120;
  display: grid;
  justify-items: center;
  align-items: center;
  padding: 16px;
}

.drawer {
  width: min(520px, calc(100vw - 18px));
  max-height: min(78vh, 720px);
  background: rgba(0, 0, 0, 0.65);
  border: 1px solid rgba(var(--fg), 0.12);
  border-radius: 18px;
  overflow: hidden;
  display: grid;
  grid-template-rows: auto 1fr;
}

.drawer-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 14px;
  border-bottom: 1px solid rgba(var(--fg), 0.1);
}

.drawer-title {
  font-weight: 900;
}

.drawer-x {
  width: 34px;
  height: 34px;
  border-radius: 12px;
  border: 1px solid rgba(var(--fg), 0.14);
  background: rgba(var(--fg), 0.06);
  color: rgba(var(--fg), 0.9);
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

.drawer-hint {
  color: rgba(var(--fg), 0.78);
  padding: 12px 0;
}

.drawer-hint.bad {
  color: rgba(254, 44, 85, 0.92);
}

.user-row {
  text-align: left;
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 12px;
  align-items: center;
  padding: 10px 10px;
  border-radius: 14px;
  border: 1px solid rgba(var(--fg), 0.1);
  background: rgba(var(--fg), 0.05);
  cursor: pointer;
}

.user-row:hover {
  background: rgba(var(--fg), 0.08);
}

.user-meta {
  min-width: 0;
}

.user-name {
  font-weight: 800;
}

.user-id {
  font-size: 12px;
  color: rgba(var(--fg), 0.6);
}

.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace;
}
</style>



