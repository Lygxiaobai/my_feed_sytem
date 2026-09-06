<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'

import { track } from '../analytics/track'
import { ApiError } from '../api/client'
import * as videoApi from '../api/video'
import { useAuthStore } from '../stores/auth'
import { useDMStore } from '../stores/dm'
import { useNotificationStore } from '../stores/notification'
import { useSocialStore } from '../stores/social'
import { useToastStore } from '../stores/toast'
import { useThemeStore } from '../stores/theme'
import AppIcon, { type AppIconName } from './AppIcon.vue'
import MessagePanel from './MessagePanel.vue'
import NotificationPanel from './NotificationPanel.vue'
import ThemePicker from './ThemePicker.vue'
import Toaster from './Toaster.vue'
import UserAvatar from './UserAvatar.vue'

type HeaderAction = {
  key: string
  label: string
  icon: AppIconName
  to: string
  auth?: boolean
}

type NavLink = {
  to: string
  label: string
  icon: AppIconName
  exact?: boolean
}

const headerActions: HeaderAction[] = [
  { key: 'wallet', label: '充积分', icon: 'coin', to: '/wallet', auth: true },
  { key: 'notify', label: '通知', icon: 'bell', to: '/notifications', auth: true },
  { key: 'messages', label: '消息', icon: 'chat', to: '/messages', auth: true },
  { key: 'publish', label: '投稿', icon: 'plus-box', to: '/video', auth: true },
]

const browseLinks: NavLink[] = [
  { to: '/', label: '推荐', icon: 'home', exact: true },
  { to: '/following', label: '关注', icon: 'user-plus' },
  { to: '/likes', label: '点赞榜', icon: 'heart' },
  { to: '/hot', label: '热榜', icon: 'fire' },
]

const mineLinks: NavLink[] = [
  { to: '/video', label: '发布', icon: 'plus-box', exact: true },
  { to: '/account', label: '账号', icon: 'user' },
]

const props = defineProps<{ full?: boolean }>()

const auth = useAuthStore()
const social = useSocialStore()
const notif = useNotificationStore()
const dm = useDMStore()
const toast = useToastStore()
const theme = useThemeStore()
const router = useRouter()
const route = useRoute()

const isHomeTabActive = computed(() => route.path === '/' || route.path === '/following' || route.path === '/likes')

const search = ref(typeof route.query.q === 'string' ? route.query.q : '')
const searchOpen = ref(false)
const notifyOpen = ref(false)
const themeOpen = ref(false)
const notifyWrap = ref<HTMLElement | null>(null)
const messageWrap = ref<HTMLElement | null>(null)
const asideThemeWrap = ref<HTMLElement | null>(null)
const themeButtonLabel = computed(() => {
  if (theme.preference === 'system') return '外观：跟随系统'
  return theme.resolved === 'dark' ? '外观：深色' : '外观：浅色'
})
const unreadLabel = computed(() => (notif.unread > 99 ? '99+' : String(notif.unread)))
const dmUnreadLabel = computed(() => (dm.unread > 99 ? '99+' : String(dm.unread)))

function bindNotifyWrap(el: unknown) {
  notifyWrap.value = el instanceof HTMLElement ? el : null
}

function bindMessageWrap(el: unknown) {
  messageWrap.value = el instanceof HTMLElement ? el : null
}

function bindAsideThemeWrap(el: unknown) {
  asideThemeWrap.value = el instanceof HTMLElement ? el : null
}

function navOn(link: NavLink) {
  if (link.exact) return route.path === link.to
  return route.path === link.to || route.path.startsWith(`${link.to}/`)
}

function toggleNotify() {
  if (!auth.isLoggedIn) {
    void router.push('/account')
    return
  }
  notifyOpen.value = !notifyOpen.value
  if (notifyOpen.value) {
    themeOpen.value = false
    dm.closePanel()
  }
}

function toggleMessages() {
  if (!auth.isLoggedIn) {
    void router.push('/account')
    return
  }
  if (dm.panelOpen) {
    dm.closePanel()
    return
  }
  notifyOpen.value = false
  themeOpen.value = false
  dm.openInbox()
}

function toggleThemeMenu() {
  themeOpen.value = !themeOpen.value
  if (themeOpen.value) {
    notifyOpen.value = false
    dm.closePanel()
  }
}

function onDocumentPointerDown(event: PointerEvent) {
  const target = event.target
  if (!(target instanceof Node)) return
  if (notifyWrap.value && !notifyWrap.value.contains(target)) notifyOpen.value = false
  if (messageWrap.value && !messageWrap.value.contains(target)) dm.closePanel()
  const inAside = asideThemeWrap.value?.contains(target)
  if (!inAside) themeOpen.value = false
}

watch(
  () => route.query.q,
  (v) => {
    search.value = typeof v === 'string' ? v : ''
  },
)

watch(
  () => route.fullPath,
  () => {
    searchOpen.value = false
    notifyOpen.value = false
    themeOpen.value = false
    dm.closePanel()
  },
)

watch(
  () => auth.isLoggedIn,
  (v) => {
    if (v) {
      void social.refreshMine()
      notif.startPolling()
      dm.startPolling()
    } else {
      social.clear()
      notif.clear()
      dm.clear()
      notifyOpen.value = false
    }
  },
  { immediate: true },
)

watch(
  () => dm.panelOpen,
  (open) => {
    if (open) notifyOpen.value = false
  },
)

onMounted(() => {
  document.addEventListener('pointerdown', onDocumentPointerDown)
})

onUnmounted(() => {
  document.removeEventListener('pointerdown', onDocumentPointerDown)
  notif.stopPolling()
  dm.stopPolling()
})

/**
 * 搜索框只做分享识别的后备：用户主动点搜索，且输入碰巧是口令时再解析。
 * 正常路径在 ShareInbox——页面粘贴或剪贴板识别，不进搜索。
 */
async function onSearch() {
  const q = search.value.trim()
  searchOpen.value = false
  if (!q) {
    await router.push({ path: '/', query: {} })
    return
  }

  const confidence = videoApi.shareTextConfidence(q)
  if (confidence !== 'none') {
    try {
      const video = await videoApi.resolveShare(q)
      search.value = ''
      await router.push(`/video/${video.id}`)
      return
    } catch (e) {
      // 口令形态明确却解析不出来，多半是内容已下架或口令抄错，直接告诉用户；
      // 只是「碰巧 8 位」的普通搜索词则静默退回搜索。
      if (confidence === 'certain') {
        toast.error(e instanceof ApiError ? e.message : '口令无效或内容已下架')
        return
      }
    }
  }

  track('search', { query: q })
  await router.push({ path: '/', query: { q } })
}

async function goLogin() {
  await router.push('/account')
}

function headerActionTo(action: HeaderAction) {
  if (action.auth && !auth.isLoggedIn) return '/account'
  return action.to
}
</script>

<template>
  <div class="dy-shell" :class="{ full: props.full }">
    <header class="dy-topbar">
      <RouterLink class="dy-brand" to="/">
        <img class="dy-mark" src="/favicon.svg" alt="" width="22" height="22" />
        <span class="dy-brand-text">ShortVideo</span>
      </RouterLink>

      <div class="dy-search desktop-only">
        <span class="dy-search-icon" aria-hidden="true">
          <AppIcon name="search" :size="16" />
        </span>
        <input
          v-model="search"
          class="dy-search-input"
          aria-label="搜索"
          placeholder="搜索你感兴趣的内容"
          @keydown.enter="onSearch"
        />
        <button class="dy-search-go" type="button" @click="onSearch">搜索</button>
      </div>

      <div class="dy-top-actions">
        <button class="dy-icon-btn mobile-only" type="button" aria-label="搜索" @click="searchOpen = !searchOpen">
          <AppIcon name="search" :size="18" />
        </button>
        <RouterLink
          class="dy-icon-btn mobile-only dy-mobile-notify"
          :to="auth.isLoggedIn ? '/notifications' : '/account'"
          aria-label="通知"
        >
          <AppIcon name="bell" :size="18" />
          <span v-if="auth.isLoggedIn && notif.unread > 0" class="dy-badge">{{ unreadLabel }}</span>
        </RouterLink>
        <RouterLink
          class="dy-icon-btn mobile-only dy-mobile-notify"
          :to="auth.isLoggedIn ? '/messages' : '/account'"
          aria-label="消息"
        >
          <AppIcon name="chat" :size="18" />
          <span v-if="auth.isLoggedIn && dm.unread > 0" class="dy-badge">{{ dmUnreadLabel }}</span>
        </RouterLink>
        <template v-for="action in headerActions" :key="action.key">
          <div
            v-if="action.key === 'notify'"
            class="dy-notify-wrap desktop-only"
            :ref="bindNotifyWrap"
          >
            <button
              class="dy-head-act"
              :class="{ on: notifyOpen || route.path === '/notifications' }"
              type="button"
              :aria-label="action.label"
              :title="action.label"
              @click="toggleNotify"
            >
              <span class="dy-head-icon" aria-hidden="true">
                <AppIcon :name="action.icon" :size="20" />
                <span v-if="auth.isLoggedIn && notif.unread > 0" class="dy-badge">{{ unreadLabel }}</span>
              </span>
            </button>
            <NotificationPanel v-if="notifyOpen" class="dy-notify-drop" variant="dropdown" @close="notifyOpen = false" />
          </div>
          <div
            v-else-if="action.key === 'messages'"
            class="dy-notify-wrap desktop-only"
            :ref="bindMessageWrap"
          >
            <button
              class="dy-head-act"
              :class="{ on: dm.panelOpen || route.path === '/messages' }"
              type="button"
              :aria-label="action.label"
              :title="action.label"
              @click="toggleMessages"
            >
              <span class="dy-head-icon" aria-hidden="true">
                <AppIcon :name="action.icon" :size="20" />
                <span v-if="auth.isLoggedIn && dm.unread > 0" class="dy-badge">{{ dmUnreadLabel }}</span>
              </span>
            </button>
            <MessagePanel v-if="dm.panelOpen" class="dy-notify-drop" variant="dropdown" @close="dm.closePanel()" />
          </div>
          <RouterLink
            v-else-if="action.key === 'publish'"
            class="dy-upload desktop-only"
            :class="{ on: route.path === action.to }"
            :to="headerActionTo(action)"
          >
            <AppIcon :name="action.icon" :size="16" />
            <span>投稿</span>
          </RouterLink>
          <RouterLink
            v-else
            class="dy-head-act desktop-only"
            :class="{ on: route.path === action.to }"
            :to="headerActionTo(action)"
            :title="action.label"
            :aria-label="action.label"
          >
            <span class="dy-head-icon" aria-hidden="true">
              <AppIcon :name="action.icon" :size="20" />
            </span>
          </RouterLink>
        </template>
        <RouterLink class="dy-head-avatar" to="/account" :title="auth.isLoggedIn ? '账号' : '登录'">
          <UserAvatar
            :username="auth.isLoggedIn ? (auth.claims?.username ?? '') : '登录'"
            :id="auth.claims?.account_id"
            :size="36"
          />
        </RouterLink>
      </div>
    </header>

    <aside class="dy-aside desktop-only">
      <div class="dy-aside-scroll">
        <nav class="dy-nav" aria-label="浏览">
          <RouterLink
            v-for="link in browseLinks"
            :key="link.to"
            class="dy-nav-link"
            :class="{ on: navOn(link) }"
            :to="link.to"
          >
            <AppIcon :name="link.icon" :size="20" />
            <span>{{ link.label }}</span>
          </RouterLink>
        </nav>

        <nav class="dy-nav" aria-label="我的">
          <RouterLink
            v-for="link in mineLinks"
            :key="link.to"
            class="dy-nav-link"
            :class="{ on: navOn(link) }"
            :to="link.to"
          >
            <AppIcon :name="link.icon" :size="20" />
            <span>{{ link.label }}</span>
          </RouterLink>
        </nav>
      </div>

      <div class="dy-aside-foot">
        <button
          v-if="!auth.isLoggedIn"
          class="dy-login"
          type="button"
          @click="goLogin"
        >
          登录
        </button>
        <div class="dy-aside-tools">
          <RouterLink class="dy-foot-link" :class="{ on: route.path === '/settings' }" to="/settings">
            <AppIcon name="gear" :size="18" />
            <span>设置</span>
          </RouterLink>
          <div class="dy-aside-theme" :ref="bindAsideThemeWrap">
            <button
              class="dy-foot-btn"
              type="button"
              :aria-label="themeButtonLabel"
              :aria-expanded="themeOpen"
              aria-haspopup="true"
              @click="toggleThemeMenu"
            >
              <AppIcon :name="theme.resolved === 'dark' ? 'moon' : 'sun'" :size="18" />
            </button>
            <div v-if="themeOpen" class="dy-theme-drop aside">
              <ThemePicker compact @picked="themeOpen = false" />
            </div>
          </div>
        </div>
      </div>
    </aside>

    <div class="dy-main">
      <div v-if="searchOpen" class="dy-mobile-search mobile-only">
        <input
          v-model="search"
          class="dy-search-input"
          aria-label="搜索"
          placeholder="搜索你感兴趣的内容"
          autofocus
          @keydown.enter="onSearch"
        />
        <button class="dy-search-go" type="button" @click="onSearch">搜索</button>
        <button class="dy-search-cancel" type="button" @click="searchOpen = false">取消</button>
      </div>

      <div class="dy-content" :class="props.full ? 'full' : 'padded'">
        <template v-if="props.full">
          <slot />
        </template>
        <template v-else>
          <div class="container">
            <slot />
          </div>
        </template>
      </div>
    </div>

    <nav class="dy-bottom-nav mobile-only" aria-label="底部导航">
      <RouterLink class="dy-tab" :class="{ on: isHomeTabActive }" to="/">
        <AppIcon name="home" :size="20" />
        <span>首页</span>
      </RouterLink>
      <RouterLink class="dy-tab" :class="{ on: route.path === '/hot' }" to="/hot">
        <AppIcon name="fire" :size="20" />
        <span>热榜</span>
      </RouterLink>
      <RouterLink class="dy-tab publish" to="/video">
        <span class="dy-tab-publish">+</span>
        <span>发布</span>
      </RouterLink>
      <RouterLink class="dy-tab" :class="{ on: route.path.startsWith('/account') }" to="/account">
        <AppIcon name="user" :size="20" />
        <span>我的</span>
      </RouterLink>
    </nav>



    <Toaster />
  </div>
</template>

<style scoped>
.dy-shell {
  height: var(--app-height, 100dvh);
  min-height: var(--app-height, 100dvh);
  display: grid;
  grid-template-columns: 220px minmax(0, 1fr);
  grid-template-rows: var(--topbar-h, 64px) minmax(0, 1fr);
  background: var(--bg);
  overflow: hidden;
}

.dy-topbar {
  grid-column: 1 / -1;
  grid-row: 1;
  height: var(--topbar-h, 64px);
  display: grid;
  grid-template-columns: 220px minmax(200px, 1fr) auto;
  align-items: center;
  padding: 0 20px 0 16px;
  padding-top: var(--safe-top, 0px);
  box-sizing: border-box;
  background: var(--chrome);
  border-bottom: 1px solid var(--border);
  z-index: 30;
}

.dy-brand {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  text-decoration: none;
  color: var(--text);
  min-width: 0;
}

.dy-brand:hover {
  text-decoration: none;
}

.dy-mark {
  width: 22px;
  height: 22px;
  flex: none;
  border-radius: 7px;
  display: block;
}

.dy-brand-text {
  font-weight: 800;
  font-size: 18px;
  letter-spacing: 0.2px;
  white-space: nowrap;
}

.dy-search {
  justify-self: center;
  width: min(560px, 100%);
  height: 40px;
  display: grid;
  grid-template-columns: 36px 1fr auto;
  align-items: center;
  background: var(--fill);
  border-radius: 999px;
  overflow: hidden;
}

.dy-search-icon {
  display: grid;
  place-items: center;
  color: var(--muted);
}

.dy-search-input {
  width: 100%;
  height: 100%;
  border: 0;
  background: transparent;
  color: var(--text);
  padding: 0 4px 0 0;
  outline: none;
  font-size: 14px;
}

.dy-search-input:focus {
  border: 0;
  box-shadow: none;
}

.dy-search-go {
  height: 100%;
  min-height: 0;
  padding: 0 16px;
  border: 0;
  border-radius: 0;
  background: transparent;
  color: var(--muted);
  font-size: 14px;
  font-weight: 600;
}

.dy-search-go:hover {
  color: var(--text);
  background: transparent;
}

.dy-top-actions {
  display: flex;
  justify-content: flex-end;
  align-items: center;
  gap: 4px;
  min-width: 0;
}

.dy-head-act {
  width: 40px;
  height: 40px;
  border-radius: 999px;
  text-decoration: none;
  color: var(--text);
  display: grid;
  place-items: center;
  appearance: none;
  border: 0;
  background: transparent;
  cursor: pointer;
  font: inherit;
  padding: 0;
}

.dy-head-act:hover,
.dy-head-act.on {
  background: var(--fill);
}

.dy-head-icon {
  width: 20px;
  height: 20px;
  display: grid;
  place-items: center;
  line-height: 1;
  position: relative;
}

.dy-upload {
  height: 36px;
  margin-left: 6px;
  padding: 0 12px 0 10px;
  border-radius: 8px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--text);
  text-decoration: none;
  font-size: 14px;
  font-weight: 600;
}

.dy-upload:hover,
.dy-upload.on {
  background: var(--fill);
  text-decoration: none;
}

.dy-upload :deep(.app-icon) {
  color: #fe2c55;
}

.dy-notify-wrap {
  position: relative;
  display: block;
  flex: none;
}

.dy-notify-drop {
  position: absolute;
  top: calc(100% + 8px);
  right: 0;
  z-index: 40;
}

.dy-aside {
  grid-column: 1;
  grid-row: 2;
  min-height: 0;
  padding: 12px 12px 16px;
  display: flex;
  flex-direction: column;
  gap: 16px;
  background: var(--chrome);
  border-right: 1px solid var(--border);
  overflow: visible;
}

.dy-aside-scroll {
  flex: 1;
  min-height: 0;
  overflow: auto;
  display: grid;
  align-content: start;
  gap: 16px;
}

.dy-nav {
  display: grid;
  gap: 2px;
}

.dy-nav + .dy-nav {
  padding-top: 12px;
  border-top: 1px solid var(--border);
}

.dy-nav-link {
  height: 44px;
  padding: 0 14px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  gap: 12px;
  color: var(--text);
  text-decoration: none;
  font-size: 15px;
}

.dy-nav-link:hover {
  background: var(--fill);
  text-decoration: none;
}

.dy-nav-link.on {
  background: var(--nav-active);
  font-weight: 600;
}

.dy-aside-foot {
  margin-top: auto;
  display: grid;
  gap: 12px;
}

.dy-login {
  height: 40px;
  min-height: 40px;
  border: 0;
  border-radius: 10px;
  background: #fe2c55;
  color: #fff;
  font-size: 14px;
  font-weight: 700;
}

.dy-login:hover {
  background: #ef2950;
}

.dy-aside-tools {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.dy-foot-link,
.dy-foot-btn {
  height: 36px;
  padding: 0 8px;
  border: 0;
  border-radius: 10px;
  background: transparent;
  color: var(--muted);
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  text-decoration: none;
  cursor: pointer;
}

.dy-foot-link:hover,
.dy-foot-btn:hover,
.dy-foot-link.on {
  color: var(--text);
  background: var(--fill);
  text-decoration: none;
}

.dy-aside-theme {
  position: relative;
}

.dy-theme-fab {
  position: fixed;
  left: calc(16px + var(--safe-left, 0px));
  bottom: calc(var(--bottom-nav-h, 56px) + 12px);
  z-index: 85;
}

.dy-theme-btn {
  width: 44px;
  height: 44px;
  padding: 0;
  border-radius: 14px;
  border: 1px solid var(--border);
  background: var(--surface);
  color: var(--text);
  box-shadow: var(--shadow);
  cursor: pointer;
  display: grid;
  place-items: center;
}

.dy-theme-btn:hover {
  background: var(--fill-hover);
}

.dy-theme-drop {
  position: absolute;
  left: 0;
  bottom: calc(100% + 8px);
  z-index: 40;
  min-width: 168px;
  padding: 6px;
  border-radius: 14px;
  border: 1px solid var(--border);
  background: var(--surface);
  box-shadow: var(--shadow);
}

.dy-theme-drop.aside {
  left: auto;
  right: 0;
}

.dy-badge {
  position: absolute;
  top: -7px;
  right: -10px;
  min-width: 16px;
  height: 16px;
  padding: 0 4px;
  border-radius: 999px;
  background: #fe2c55;
  color: #fff;
  font-size: 10px;
  font-weight: 800;
  line-height: 16px;
  text-align: center;
}

.dy-mobile-notify {
  position: relative;
  text-decoration: none;
}

.dy-mobile-notify .dy-badge {
  top: 4px;
  right: 4px;
}

.dy-head-avatar {
  margin-left: 8px;
  display: grid;
  place-items: center;
  text-decoration: none;
}

.dy-icon-btn {
  width: 40px;
  height: 40px;
  border-radius: 12px;
  border: 0;
  background: transparent;
  color: var(--text);
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0;
  flex-shrink: 0;
}

.dy-mobile-search {
  display: flex;
  align-items: center;
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  height: var(--topbar-h, 52px);
  z-index: 95;
  gap: 8px;
  padding: 0 12px;
  border-bottom: 1px solid var(--border);
  background: var(--chrome);
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
  box-sizing: border-box;
}

.dy-mobile-search .dy-search-input {
  flex: 1;
  min-width: 0;
  height: 36px;
  border: 0;
  border-radius: 999px;
  background: var(--fill);
  color: var(--text);
  padding: 0 14px;
  font-size: 14px;
  outline: none;
}

.dy-search-cancel {
  appearance: none;
  border: 0;
  background: transparent;
  color: var(--muted);
  padding: 0 8px;
  cursor: pointer;
  font-size: 14px;
}

.dy-search-cancel:hover {
  color: var(--text);
}

.dy-main {
  grid-column: 2;
  grid-row: 2;
  min-width: 0;
  min-height: 0;
  height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--bg);
  overflow: hidden;
}

.dy-content {
  flex: 1 1 0%;
  min-height: 0;
  min-width: 0;
  display: flex;
  flex-direction: column;
  height: 100%;
}

.dy-content.padded {
  overflow: auto;
  -webkit-overflow-scrolling: touch;
}

.dy-content.full {
  height: 100%;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.dy-bottom-nav {
  position: fixed;
  left: 0;
  right: 0;
  bottom: 0;
  z-index: 80;
  height: var(--bottom-nav-h, 56px);
  padding-bottom: var(--safe-bottom, 0px);
  box-sizing: border-box;
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  align-items: stretch;
  border-top: 1px solid var(--border);
  background: var(--nav);
}

.dy-tab {
  appearance: none;
  border: 0;
  background: transparent;
  color: var(--muted);
  display: grid;
  place-items: center;
  gap: 2px;
  padding: 6px 2px 8px;
  font-size: 11px;
  text-decoration: none;
  cursor: pointer;
  min-width: 0;
}

.dy-tab.on,
.dy-tab.router-link-active {
  color: var(--text);
}

.dy-tab-publish {
  width: 34px;
  height: 34px;
  border-radius: 10px;
  display: grid;
  place-items: center;
  font-size: 22px;
  font-weight: 700;
  line-height: 1;
  color: #fff;
  background: linear-gradient(135deg, #25f4ee, #fe2c55);
}

.dy-tab.publish {
  color: var(--text);
}

.mobile-only {
  display: none !important;
}

.desktop-only {
  display: initial;
}

.dy-aside.desktop-only {
  display: flex;
}

.dy-search.desktop-only {
  display: grid;
}

.dy-head-act.desktop-only {
  display: grid;
}

.dy-upload.desktop-only {
  display: inline-flex;
}

.dy-notify-wrap.desktop-only {
  display: block;
}

@media (max-width: 900px) {
  .dy-shell {
    grid-template-columns: 1fr;
    grid-template-rows: var(--topbar-h, 52px) minmax(0, 1fr);
  }

  .desktop-only {
    display: none !important;
  }



  .dy-topbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    height: var(--topbar-h, 52px);
    padding: 0 12px;
    box-sizing: border-box;
  }

  .dy-brand {
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
    gap: 8px;
  }

  .dy-brand-text {
    font-size: 16px;
  }

  .dy-top-actions {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 4px;
    height: 100%;
    flex-shrink: 0;
  }

  .dy-icon-btn,
  .dy-icon-btn.mobile-only,
  button.dy-icon-btn,
  a.dy-icon-btn {
    width: 36px;
    height: 36px;
    display: inline-flex !important;
    align-items: center !important;
    justify-content: center !important;
    border-radius: 50%;
    padding: 0;
    flex-shrink: 0;
    position: relative;
    box-sizing: border-box;
    vertical-align: middle;
  }

  .dy-icon-btn :deep(.app-icon),
  .dy-icon-btn .app-icon {
    display: block;
    margin: 0 auto;
  }

  .dy-head-avatar {
    margin-left: 4px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
  }

  .dy-mobile-notify .dy-badge {
    top: 2px;
    right: 2px;
  }

  .dy-bottom-nav.mobile-only {
    display: grid !important;
  }

  .dy-mobile-search.mobile-only {
    display: flex !important;
  }

  .dy-main {
    grid-column: 1;
    padding-bottom: var(--bottom-nav-h, 56px);
    height: 100%;
    min-height: 0;
    overflow: hidden;
  }
}
</style>
