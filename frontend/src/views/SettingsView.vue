<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'

import { track } from '../analytics/track'
import AppShell from '../components/AppShell.vue'
import ThemePicker from '../components/ThemePicker.vue'
import UserAvatar from '../components/UserAvatar.vue'
import { ApiError } from '../api/client'
import * as accountApi from '../api/account'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'
import type { PasskeyItem } from '../api/types'
import * as webauthn from '../webauthn'

const router = useRouter()
const auth = useAuthStore()
const toast = useToastStore()

const busy = ref(false)
const passkeyReady = ref(false)
const passkeys = reactive({
  loading: false,
  name: '',
  items: [] as PasskeyItem[],
})

const me = computed(() => ({
  id: auth.claims?.account_id ?? 0,
  username: auth.claims?.username ?? '',
}))

const rename = reactive({
  open: false,
  newUsername: '',
})

async function openRename() {
  if (!auth.isLoggedIn) return
  rename.open = true
  rename.newUsername = me.value.username
  await nextTick()
}

async function submitRename() {
  if (!auth.isLoggedIn) return
  if (busy.value) return
  const newUsername = rename.newUsername.trim()
  if (!newUsername) {
    toast.error('请输入新用户名')
    return
  }

  busy.value = true
  try {
    const res = await accountApi.rename(newUsername)
    auth.setToken(res.token)
    rename.open = false
    toast.success('改名成功（已刷新 token）')
  } catch (e) {
    const msg = e instanceof ApiError ? e.message : String(e)
    toast.error(msg)
  } finally {
    busy.value = false
  }
}

async function goLogin() {
  await router.push('/account')
}

async function goChangePassword() {
  await router.push('/account/change-password')
}

async function loadPasskeys() {
  if (!auth.isLoggedIn) {
    passkeys.items = []
    return
  }
  passkeys.loading = true
  try {
    passkeys.items = (await accountApi.listPasskeys()).items ?? []
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : String(e))
    passkeys.items = []
  } finally {
    passkeys.loading = false
  }
}

function formatPasskeyTime(value?: string | null) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleString()
}

async function addPasskey() {
  if (!auth.isLoggedIn || busy.value) return
  if (!passkeyReady.value) {
    toast.error(webauthn.passkeyUnavailableTip)
    return
  }
  busy.value = true
  try {
    const began = await accountApi.beginPasskeyRegister(passkeys.name.trim())
    const cred = await webauthn.createPasskey(began.options)
    await accountApi.finishPasskeyRegister(began.session_id, webauthn.encodeCredential(cred))
    passkeys.name = ''
    toast.success('已添加通行密钥')
    await loadPasskeys()
  } catch (e) {
    if (webauthn.isPasskeyCanceled(e)) {
      toast.info('已取消')
      return
    }
    toast.error(e instanceof ApiError ? e.message : String(e))
  } finally {
    busy.value = false
  }
}

async function removePasskey(item: PasskeyItem) {
  if (!auth.isLoggedIn || busy.value) return
  if (!window.confirm(`删除通行密钥「${item.name}」？`)) return
  busy.value = true
  try {
    await accountApi.deletePasskey(item.id)
    toast.success('已删除通行密钥')
    await loadPasskeys()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : String(e))
  } finally {
    busy.value = false
  }
}

onMounted(() => {
  passkeyReady.value = webauthn.passkeySupported()
})

watch(
  () => auth.isLoggedIn,
  (ok) => {
    if (ok) void loadPasskeys()
    else passkeys.items = []
  },
  { immediate: true },
)

async function onLogout() {
  if (!auth.isLoggedIn) return
  if (busy.value) return
  if (!window.confirm('确认退出登录？')) return

  busy.value = true
  try {
    await accountApi.logout()
  } catch (e) {
    const msg = e instanceof ApiError ? e.message : String(e)
    toast.error(`登出失败：${msg}`)
  } finally {
    auth.clearToken()
    track('logout')
    rename.open = false
    toast.info('已退出登录')
    busy.value = false
    await router.push('/')
  }
}
</script>

<template>
  <AppShell>
    <div class="card">
      <p class="title">外观</p>
      <p class="subtle">浅色、深色，或跟随系统。此选择只存在这台设备上，不用登录。</p>
      <div style="margin-top: 12px">
        <ThemePicker />
      </div>
    </div>

    <div v-if="!auth.isLoggedIn" class="card" style="margin-top: 14px">
      <p class="title">账号</p>
      <p class="subtle">登录后才能改名、管理通行密钥和退出。</p>
      <div class="tools">
        <button class="primary compact" type="button" @click="goLogin">去登录</button>
      </div>
    </div>

    <div v-else class="card" style="margin-top: 14px">
      <div class="profile-head">
        <UserAvatar :username="me.username" :id="me.id" :size="56" />
        <div class="profile-id">
          <div class="title" style="margin: 0">{{ me.username }}</div>
        </div>
        <button class="ghost compact" type="button" :disabled="busy" @click="openRename">改名</button>
      </div>

      <div v-if="rename.open" class="grid" style="margin-top: 14px">
        <div>
          <label>新用户名</label>
          <input v-model.trim="rename.newUsername" @keydown.enter="submitRename" />
        </div>
        <div class="tools">
          <button class="ghost compact" type="button" :disabled="busy" @click="rename.open = false">取消</button>
          <button class="primary compact" type="button" :disabled="busy" @click="submitRename">保存</button>
        </div>
      </div>

      <div class="section">
        <p class="title">通行密钥</p>
        <p class="subtle">登记后可在支持的设备上直接登录，不必再输入验证码。邮箱验证码仍可用来找回账号。</p>
        <div class="grid" style="margin-top: 12px">
          <div>
            <label>名称（可选）</label>
            <input v-model.trim="passkeys.name" maxlength="32" placeholder="例如：这台电脑" @keydown.enter="addPasskey" />
          </div>
          <div class="tools">
            <button class="ghost compact" type="button" :disabled="busy || passkeys.loading" @click="addPasskey">
              添加通行密钥
            </button>
          </div>
        </div>
        <div v-if="passkeys.loading" class="subtle" style="margin-top: 12px">正在加载…</div>
        <div v-else-if="passkeys.items.length === 0" class="subtle" style="margin-top: 12px">还没有通行密钥</div>
        <div v-else class="passkey-list">
          <div v-for="item in passkeys.items" :key="item.id" class="passkey-row">
            <div>
              <div class="passkey-name">{{ item.name }}</div>
              <div class="subtle">
                添加于 {{ formatPasskeyTime(item.created_at) }}
                <template v-if="item.last_used_at"> · 最近使用 {{ formatPasskeyTime(item.last_used_at) }}</template>
              </div>
            </div>
            <button class="ghost compact" type="button" :disabled="busy" @click="removePasskey(item)">删除</button>
          </div>
        </div>
      </div>

      <div class="section">
        <p class="title">账号安全</p>
        <div class="tools">
          <button class="ghost compact" type="button" :disabled="busy" @click="goChangePassword">修改密码</button>
          <button class="danger compact" type="button" :disabled="busy" @click="onLogout">退出登录</button>
        </div>
      </div>
    </div>
  </AppShell>
</template>

<style scoped>
.ghost {
  border: 1px solid var(--border);
  background: var(--fill);
  color: var(--text);
  border-radius: 12px;
  padding: 10px 12px;
  cursor: pointer;
}

.ghost:hover {
  background: var(--fill-hover);
}

.ghost.compact,
.primary.compact,
.danger.compact {
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
}

.tools {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 12px;
}

.section {
  margin-top: 18px;
  padding-top: 16px;
  border-top: 1px solid var(--border);
}

.passkey-list {
  display: grid;
  gap: 10px;
  margin-top: 12px;
}

.passkey-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: 12px;
}

.passkey-name {
  font-weight: 800;
}
</style>
