<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'

import { track } from '../analytics/track'
import AppShell from '../components/AppShell.vue'
import Skeleton from '../components/Skeleton.vue'
import UserAvatar from '../components/UserAvatar.vue'
import UserListSkeleton from '../components/UserListSkeleton.vue'
import VideoGridSkeleton from '../components/VideoGridSkeleton.vue'
import { ApiError } from '../api/client'
import * as accountApi from '../api/account'
import * as adminApi from '../api/admin'
import * as historyApi from '../api/history'
import type { AuditStatus, HistoryItem, HistoryStatus, SocialRelation, Video } from '../api/types'
import * as videoApi from '../api/video'
import { formatWatchClock } from '../history/rules'
import { useAuthStore } from '../stores/auth'
import { useSocialStore } from '../stores/social'
import { useToastStore } from '../stores/toast'
import * as webauthn from '../webauthn'

const router = useRouter()
const auth = useAuthStore()
const social = useSocialStore()
const toast = useToastStore()

const busy = ref(false)
const emailForm = reactive({ email: '', code: '' })
const sendingCode = ref(false)
const adminAllowed = ref(false)
const countdown = ref(0)
let countdownTimer: number | undefined
const passkeyReady = ref(false)
const passkeyBusy = ref(false)
let passkeyAbort: AbortController | undefined

const me = computed(() => ({
  id: auth.claims?.account_id ?? 0,
  username: auth.claims?.username ?? '',
}))

const myVideos = reactive({
  loading: false,
  error: '',
  items: [] as Video[],
})

const totalReceivedLikes = computed(() => myVideos.items.reduce((sum, item) => sum + (item.likes_count ?? 0), 0))

type VideoTab = 'works' | 'likes' | 'history'
const videoTab = ref<VideoTab>('works')
const historyStatus = ref<HistoryStatus>('unfinished')

const historyList = reactive({
  loading: false,
  loadingMore: false,
  error: '',
  items: [] as HistoryItem[],
  nextCursor: '',
  hasMore: false,
})
let historyReq = 0

let myVideosReq = 0
async function loadMyVideos() {
  const id = me.value.id
  if (!auth.isLoggedIn || !id) {
    myVideos.items = []
    myVideos.error = ''
    myVideos.loading = false
    return
  }
  if (myVideos.loading) return

  const req = ++myVideosReq
  myVideos.loading = true
  myVideos.error = ''
  try {
    const vids = await videoApi.listByAuthorId(id)
    if (req !== myVideosReq) return
    myVideos.items = vids
  } catch (e) {
    if (req !== myVideosReq) return
    myVideos.error = e instanceof ApiError ? e.message : String(e)
    myVideos.items = []
  } finally {
    if (req === myVideosReq) myVideos.loading = false
  }
}

const likedVideos = reactive({
  loading: false,
  loaded: false,
  error: '',
  items: [] as Video[],
})

const likedVideoCountText = computed(() => {
  if (!auth.isLoggedIn) return '0'
  if (likedVideos.loading || !likedVideos.loaded) return '…'
  return String(likedVideos.items.length)
})

let likedVideosReq = 0

async function loadLikedVideos() {
  if (!auth.isLoggedIn) {
    likedVideos.loading = false
    likedVideos.loaded = false
    likedVideos.error = ''
    likedVideos.items = []
    return
  }
  if (likedVideos.loading) return

  const req = ++likedVideosReq
  likedVideos.loading = true
  likedVideos.error = ''
  try {
    const vids = await videoApi.listLiked()
    if (req !== likedVideosReq) return
    likedVideos.items = vids
    likedVideos.loaded = true
  } catch (e) {
    if (req !== likedVideosReq) return
    likedVideos.error = e instanceof ApiError ? e.message : String(e)
    likedVideos.items = []
    likedVideos.loaded = false
  } finally {
    if (req === likedVideosReq) likedVideos.loading = false
  }
}

async function goVideo(id: number) {
  await router.push(`/video/${id}`)
}

/**
 * 把审核状态映射成角标文案。
 *
 * 只在自己的作品列表里展示——他人看到的列表本就只有已过审内容。
 * 被拒时刻意只给通用文案，不展示命中了什么规则：一旦回显，
 * 就能通过反复修改试探出词库边界。
 */
function auditBadge(status?: AuditStatus): { text: string; tone: string } | null {
  switch (status) {
    case 'pending':
      return { text: '审核中', tone: 'wait' }
    case 'reviewing':
      return { text: '人工复审中', tone: 'wait' }
    case 'rejected':
      return { text: '未通过', tone: 'bad' }
    default:
      // approved 或字段缺失时不显示角标，避免正常内容平白多出干扰元素。
      return null
  }
}

function openWorksVideos() {
  videoTab.value = 'works'
  void loadMyVideos()
}

function openLikedVideos() {
  videoTab.value = 'likes'
  void loadLikedVideos()
}

function openHistory(status: HistoryStatus = historyStatus.value) {
  videoTab.value = 'history'
  historyStatus.value = status
  void loadHistory(true)
}

async function loadHistory(reset: boolean) {
  if (!auth.isLoggedIn) {
    historyList.items = []
    historyList.error = ''
    historyList.loading = false
    historyList.loadingMore = false
    historyList.nextCursor = ''
    historyList.hasMore = false
    return
  }
  if (historyList.loading || historyList.loadingMore) return

  const req = ++historyReq
  if (reset) historyList.loading = true
  else historyList.loadingMore = true
  historyList.error = ''
  try {
    const res = await historyApi.listHistory(historyStatus.value, reset ? '' : historyList.nextCursor)
    if (req !== historyReq) return
    historyList.items = reset ? res.items : historyList.items.concat(res.items)
    historyList.nextCursor = res.next_cursor
    historyList.hasMore = res.has_more
  } catch (e) {
    if (req !== historyReq) return
    historyList.error = e instanceof ApiError ? e.message : String(e)
    if (reset) historyList.items = []
  } finally {
    if (req === historyReq) {
      historyList.loading = false
      historyList.loadingMore = false
    }
  }
}

function historyProgressPercent(item: HistoryItem) {
  if (item.duration_ms <= 0) return 0
  return Math.min(100, Math.round((item.position_ms / item.duration_ms) * 100))
}

function stopPasskeyAutofill() {
  passkeyAbort?.abort()
  passkeyAbort = undefined
}

async function completePasskeyLogin(sessionId: string, cred: PublicKeyCredential) {
  if (busy.value) return
  busy.value = true
  try {
    const res = await accountApi.finishPasskeyLogin(sessionId, webauthn.encodeCredential(cred))
    auth.setToken(res.token)
    track('login')
    toast.success('登录成功')
    await social.refreshMine()
    await Promise.all([loadMyVideos(), loadLikedVideos(), loadHistory(true)])
  } finally {
    busy.value = false
  }
}

async function startPasskeyAutofill() {
  stopPasskeyAutofill()
  if (!passkeyReady.value || auth.isLoggedIn) return
  if (!(await webauthn.conditionalMediationAvailable())) return
  const controller = new AbortController()
  passkeyAbort = controller
  try {
    const began = await accountApi.beginPasskeyLogin()
    if (passkeyAbort !== controller) return
    const cred = await webauthn.getPasskey(began.options, { conditional: true, signal: controller.signal })
    if (passkeyAbort !== controller) return
    await completePasskeyLogin(began.session_id, cred)
  } catch (e) {
    if (passkeyAbort !== controller || webauthn.isPasskeyCanceled(e)) return
    toast.error(e instanceof ApiError ? e.message : '通行密钥登录失败')
  }
}

async function onPasskeyLogin() {
  if (busy.value || passkeyBusy.value) return
  if (!passkeyReady.value) {
    toast.error(webauthn.passkeyUnavailableTip)
    return
  }
  stopPasskeyAutofill()
  passkeyBusy.value = true
  try {
    const began = await accountApi.beginPasskeyLogin()
    const cred = await webauthn.getPasskey(began.options)
    await completePasskeyLogin(began.session_id, cred)
  } catch (e) {
    if (webauthn.isPasskeyCanceled(e)) {
      toast.info('已取消')
      return
    }
    toast.error(e instanceof ApiError ? e.message : '通行密钥登录失败')
  } finally {
    passkeyBusy.value = false
    if (!auth.isLoggedIn) void startPasskeyAutofill()
  }
}

onMounted(() => {
  passkeyReady.value = webauthn.passkeySupported()
  if (!auth.isLoggedIn && passkeyReady.value) void startPasskeyAutofill()
})

onUnmounted(() => {
  stopPasskeyAutofill()
  if (countdownTimer !== undefined) window.clearInterval(countdownTimer)
})

function startCountdown() {
  countdown.value = 60
  if (countdownTimer !== undefined) window.clearInterval(countdownTimer)
  countdownTimer = window.setInterval(() => {
    countdown.value -= 1
    if (countdown.value <= 0 && countdownTimer !== undefined) {
      window.clearInterval(countdownTimer)
      countdownTimer = undefined
    }
  }, 1000)
}

async function onSendCode() {
  const email = emailForm.email.trim()
  if (!email) {
    toast.error('请输入邮箱')
    return
  }
  if (sendingCode.value || countdown.value > 0) return
  sendingCode.value = true
  try {
    await accountApi.sendEmailCode(email)
    startCountdown()
    toast.success('验证码已发送')
  } catch (e) {
    const msg = e instanceof ApiError ? e.message : String(e)
    toast.error(msg)
  } finally {
    sendingCode.value = false
  }
}

async function onEmailLogin() {
  if (busy.value) return
  stopPasskeyAutofill()
  const email = emailForm.email.trim()
  const code = emailForm.code.trim()
  if (!email || !code) {
    toast.error('请输入邮箱和验证码')
    return
  }

  busy.value = true
  try {
    const res = await accountApi.verifyEmail(email, code)
    auth.setToken(res.token)
    if (res.created) track('register')
    track('login')
    toast.success(res.created ? '注册并登录成功' : '登录成功')
    await social.refreshMine()
    await Promise.all([loadMyVideos(), loadLikedVideos(), loadHistory(true)])
  } catch (e) {
    const msg = e instanceof ApiError ? e.message : String(e)
    toast.error(msg)
  } finally {
    busy.value = false
  }
}

function onOauthSoon() {
  toast.info('即将开放')
}

async function goSettings() {
  await router.push('/settings')
}

async function goWallet() {
  await router.push('/wallet')
}

async function goAdmin() {
  await router.push('/admin')
}

async function loadAdminAccess() {
  if (!auth.isLoggedIn) {
    adminAllowed.value = false
    return
  }
  try {
    const access = await adminApi.adminAccess()
    adminAllowed.value = access.allowed
  } catch {
    adminAllowed.value = false
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
const listItems = computed(() => (drawer.tab === 'followers' ? social.followers : social.vloggers))
const drawerLoading = computed(() => (drawer.tab === 'followers' ? social.followersLoading : social.vloggersLoading))
const drawerError = computed(() => (drawer.tab === 'followers' ? social.followersError : social.vloggersError))
const socialErrorHint = computed(() => social.followersError || social.vloggersError)

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

watch(
  () => auth.isLoggedIn,
  (v) => {
    if (!v) {
      drawer.open = false
      myVideosReq += 1
      likedVideosReq += 1
      myVideos.loading = false
      myVideos.items = []
      myVideos.error = ''

      likedVideos.loading = false
      likedVideos.loaded = false
      likedVideos.items = []
      likedVideos.error = ''

      historyReq += 1
      historyList.loading = false
      historyList.loadingMore = false
      historyList.items = []
      historyList.error = ''
      historyList.nextCursor = ''
      historyList.hasMore = false
      historyStatus.value = 'unfinished'

      videoTab.value = 'works'
      adminAllowed.value = false
      if (passkeyReady.value) void startPasskeyAutofill()
    } else {
      stopPasskeyAutofill()
    }
  },
)

watch(
  () => me.value.id,
  (id) => {
    if (auth.isLoggedIn && id) {
      void loadMyVideos()
      void loadLikedVideos()
      void loadAdminAccess()
    } else {
      adminAllowed.value = false
    }
  },
  { immediate: true },
)
</script>

<template>
  <AppShell>
    <div v-if="!auth.isLoggedIn" class="login-wrap">
      <div class="card login-card">
        <p class="title">登录 / 注册</p>
        <div class="grid" style="margin-top: 10px">
          <div>
            <label>邮箱</label>
            <input v-model.trim="emailForm.email" type="email" autocomplete="username webauthn" />
          </div>
          <div>
            <label>验证码</label>
            <div class="code-row">
              <input
                v-model.trim="emailForm.code"
                inputmode="numeric"
                maxlength="6"
                autocomplete="one-time-code"
                @keydown.enter="onEmailLogin"
              />
              <button class="ghost" type="button" :disabled="sendingCode || countdown > 0" @click="onSendCode">
                {{ countdown > 0 ? `${countdown}s` : '发送验证码' }}
              </button>
            </div>
          </div>
          <button class="primary" type="button" :disabled="busy" @click="onEmailLogin">登录 / 注册</button>
        </div>

        <button
          class="ghost passkey-btn"
          type="button"
          :disabled="busy || passkeyBusy"
          @click="onPasskeyLogin"
        >
          使用通行密钥登录
        </button>

        <div class="oauth-row">
          <button class="ghost oauth" type="button" @click="onOauthSoon">微信登录</button>
          <button class="ghost oauth" type="button" @click="onOauthSoon">QQ 登录</button>
        </div>

        <button class="linkish" type="button" @click="router.push('/account/password')">账号密码登录</button>
      </div>
    </div>

    <template v-else>
      <div class="card profile-card">
        <div class="profile-head">
          <UserAvatar :username="me.username" :id="me.id" :size="56" />
          <div class="profile-id">
            <div class="title" style="margin: 0">{{ me.username }}</div>
            <div class="stats">
              <button class="stat" type="button" :disabled="social.followersLoading" @click="openFollowers">
                <strong>
                  <Skeleton v-if="social.followersLoading" width="16px" height="14px" />
                  <template v-else>{{ social.followerCount }}</template>
                </strong>
                粉丝
              </button>
              <button class="stat" type="button" :disabled="social.vloggersLoading" @click="openFollowing">
                <strong>
                  <Skeleton v-if="social.vloggersLoading" width="16px" height="14px" />
                  <template v-else>{{ social.followingCount }}</template>
                </strong>
                关注
              </button>
              <span class="stat static">
                <strong>
                  <Skeleton v-if="myVideos.loading" width="16px" height="14px" />
                  <template v-else>{{ totalReceivedLikes }}</template>
                </strong>
                获赞
              </span>
            </div>
            <div v-if="socialErrorHint" class="subtle">社交信息加载失败：{{ socialErrorHint }}</div>
          </div>
        </div>

        <div class="tools">
          <button class="ghost compact" type="button" @click="router.push('/checkin')">签到</button>
          <button class="ghost compact" type="button" @click="router.push('/lottery')">抽奖</button>
          <button class="ghost compact" type="button" @click="goWallet">钱包</button>
          <button v-if="adminAllowed" class="ghost compact" type="button" @click="goAdmin">管理后台</button>
          <button class="ghost compact" type="button" @click="goSettings">设置</button>
        </div>

        <div class="tabs" role="tablist">
          <button type="button" role="tab" :class="{ active: videoTab === 'works' }" @click="openWorksVideos">
            作品
            <span class="tab-count">{{ myVideos.loading ? '…' : myVideos.items.length }}</span>
          </button>
          <button type="button" role="tab" :class="{ active: videoTab === 'likes' }" @click="openLikedVideos">
            点赞视频
            <span class="tab-count">{{ likedVideoCountText }}</span>
          </button>
          <button type="button" role="tab" :class="{ active: videoTab === 'history' }" @click="openHistory()">历史</button>
        </div>

        <template v-if="videoTab === 'history'">
          <div class="filters">
            <button type="button" :class="{ active: historyStatus === 'unfinished' }" @click="openHistory('unfinished')">
              未看完
            </button>
            <button type="button" :class="{ active: historyStatus === 'completed' }" @click="openHistory('completed')">
              已看完
            </button>
          </div>
          <VideoGridSkeleton v-if="historyList.loading" style="margin-top: 12px" />
          <div v-else-if="historyList.error" class="hint bad" style="margin-top: 12px">{{ historyList.error }}</div>
          <div v-else-if="historyList.items.length === 0" class="hint" style="margin-top: 12px">
            {{ historyStatus === 'unfinished' ? '还没有未看完的视频' : '还没有已看完的视频' }}
          </div>
          <div v-else class="video-grid" style="margin-top: 12px">
            <button v-for="item in historyList.items" :key="item.video_id" class="video-card" type="button" @click="goVideo(item.video_id)">
              <span class="cover-wrap">
                <img class="video-cover" :src="item.video.cover_url" :alt="item.video.title" loading="lazy" />
                <span v-if="item.completed" class="watch-done">已看完</span>
                <span v-else class="watch-bar" aria-hidden="true">
                  <i :style="{ width: `${historyProgressPercent(item)}%` }" />
                </span>
              </span>
              <div class="video-meta">
                <div class="video-title">{{ item.video.title }}</div>
                <div class="video-sub subtle">
                  {{ item.completed ? '已看完' : `看到 ${formatWatchClock(item.position_ms)}` }}
                  · {{ item.video.username }}
                </div>
              </div>
            </button>
          </div>
          <button
            v-if="historyList.hasMore"
            class="ghost"
            type="button"
            style="margin-top: 12px"
            :disabled="historyList.loadingMore"
            @click="loadHistory(false)"
          >
            {{ historyList.loadingMore ? '加载中…' : '加载更多' }}
          </button>
        </template>
        <template v-else-if="videoTab === 'works'">
          <VideoGridSkeleton v-if="myVideos.loading" style="margin-top: 12px" />
          <div v-else-if="myVideos.error" class="hint bad" style="margin-top: 12px">{{ myVideos.error }}</div>
          <div v-else-if="myVideos.items.length === 0" class="hint" style="margin-top: 12px">暂无作品</div>

          <div v-else class="video-grid" style="margin-top: 12px">
            <button v-for="v in myVideos.items" :key="v.id" class="video-card" type="button" @click="goVideo(v.id)">
              <img class="video-cover" :src="v.cover_url" :alt="v.title" loading="lazy" />
              <span v-if="auditBadge(v.audit_status)" class="audit-badge" :class="auditBadge(v.audit_status)?.tone">
                {{ auditBadge(v.audit_status)?.text }}
              </span>
              <div class="video-meta">
                <div class="video-title">{{ v.title }}</div>
                <div class="video-sub subtle">❤️ {{ v.likes_count }} · 💬 {{ v.comment_count }} · {{ new Date(v.created_at).toLocaleDateString() }}</div>
              </div>
            </button>
          </div>
        </template>
        <template v-else>
          <VideoGridSkeleton v-if="likedVideos.loading" style="margin-top: 12px" />
          <div v-else-if="likedVideos.error" class="hint bad" style="margin-top: 12px">{{ likedVideos.error }}</div>
          <div v-else-if="likedVideos.items.length === 0" class="hint" style="margin-top: 12px">暂无点赞视频</div>

          <div v-else class="video-grid" style="margin-top: 12px">
            <button v-for="v in likedVideos.items" :key="v.id" class="video-card" type="button" @click="goVideo(v.id)">
              <img class="video-cover" :src="v.cover_url" :alt="v.title" loading="lazy" />
              <div class="video-meta">
                <div class="video-title">{{ v.title }}</div>
                <div class="video-sub subtle">❤️ {{ v.likes_count }} · 💬 {{ v.comment_count }} · {{ new Date(v.created_at).toLocaleDateString() }}</div>
              </div>
            </button>
          </div>
        </template>
      </div>
    </template>

    <div v-if="drawer.open" class="drawer-backdrop" @click.self="closeDrawer">
      <div class="drawer">
        <div class="drawer-head">
          <div class="drawer-title">{{ listTitle }}</div>
          <button class="drawer-x" type="button" @click="closeDrawer">×</button>
        </div>
        <div class="drawer-body">
          <UserListSkeleton v-if="drawerLoading" />
          <div v-else-if="drawerError" class="drawer-hint bad">{{ drawerError }}</div>
          <div v-else-if="listItems.length === 0" class="drawer-hint">暂无</div>

          <button v-for="u in listItems" v-if="!drawerLoading && !drawerError" :key="u.id" class="user-row" type="button" @click="goUser(u)">
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
.login-wrap {
  display: grid;
  justify-items: center;
  align-content: start;
  padding: clamp(56px, 14vh, 160px) 16px 40px;
}

.login-card {
  width: min(420px, 100%);
}

.code-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px;
}

.passkey-btn {
  margin-top: 14px;
  width: 100%;
  text-align: center;
}

.oauth-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
  margin-top: 14px;
}

.oauth {
  text-align: center;
}

.linkish {
  margin-top: 14px;
  border: 0;
  background: transparent;
  color: rgba(var(--fg), 0.62);
  padding: 0;
  cursor: pointer;
  text-align: left;
}

.linkish:hover {
  color: rgba(var(--fg), 0.9);
}

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

.ghost.compact {
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
  display: grid;
  gap: 6px;
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

.tools {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 14px;
}

.tabs {
  display: flex;
  flex-wrap: wrap;
  gap: 20px;
  margin-top: 16px;
  border-bottom: 1px solid rgba(var(--fg), 0.08);
}

.tabs button {
  border: 0;
  background: transparent;
  border-radius: 0;
  padding: 10px 0;
  min-height: 0;
  color: rgba(var(--fg), 0.45);
  font-weight: 600;
  cursor: pointer;
  position: relative;
}

.tabs button:hover {
  background: transparent;
  color: rgba(var(--fg), 0.8);
}

.tabs button.active {
  color: rgba(var(--fg), 0.92);
}

.tabs button.active::after {
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
  color: rgba(var(--fg), 0.4);
}

.tabs button.active .tab-count {
  color: rgba(var(--fg), 0.55);
}

.filters {
  display: flex;
  gap: 16px;
  margin-top: 12px;
}

.filters button {
  border: 0;
  background: transparent;
  padding: 0;
  min-height: 0;
  color: rgba(var(--fg), 0.45);
  cursor: pointer;
  font-size: 13px;
}

.filters button:hover {
  background: transparent;
  color: rgba(var(--fg), 0.8);
}

.filters button.active {
  color: rgba(var(--fg), 0.9);
  font-weight: 700;
}

.hint {
  color: rgba(var(--fg), 0.78);
}

.hint.bad {
  color: rgba(254, 44, 85, 0.92);
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
  border: 1px solid rgba(var(--fg), 0.12);
  background: rgba(var(--fg), 0.05);
  border-radius: 16px;
  overflow: hidden;
  cursor: pointer;
  padding: 0;
  text-align: left;
  position: relative;
}

.cover-wrap {
  position: relative;
  display: block;
}

.watch-bar {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  height: 3px;
  background: rgba(var(--fg), 0.2);
}

.watch-bar > i {
  display: block;
  height: 100%;
  background: #fe2c55;
}

.watch-done {
  position: absolute;
  top: 8px;
  right: 8px;
  padding: 3px 8px;
  border-radius: 999px;
  font-size: 11px;
  line-height: 1.4;
  background: var(--toast-bg);
  color: rgba(var(--fg), 0.92);
  border: 1px solid rgba(var(--fg), 0.18);
}

.audit-badge {
  position: absolute;
  top: 8px;
  left: 8px;
  padding: 3px 8px;
  border-radius: 999px;
  font-size: 11px;
  line-height: 1.4;
  backdrop-filter: blur(8px);
  border: 1px solid rgba(var(--fg), 0.18);
}

.audit-badge.wait {
  background: var(--toast-bg);
  color: rgba(var(--fg), 0.9);
}

.audit-badge.bad {
  background: rgba(254, 44, 85, 0.85);
  color: #fff;
  border-color: rgba(254, 44, 85, 0.6);
}

.video-card:hover {
  background: rgba(var(--fg), 0.08);
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



